package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

const (
	AccountingAlgorithmVersion = "reconciled-accounting-v1"
	DimensionDerivationVersion = "chain-semantics-v1"
)

var runSeqCounter int64

// RebuildAccounting 从不可变原始证据完整重算并原子发布一轮核算结果
func RebuildAccounting(ctx context.Context, db *sql.DB, notes string) (*AccountingRunRecord, error) {
	now := time.Now().UTC()
	seq := atomic.AddInt64(&runSeqCounter, 1)
	runID := fmt.Sprintf("run-%d-%d", now.UnixNano(), seq)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin accounting tx: %w", err)
	}
	defer tx.Rollback()

	// 1. 统计源 Journal 边界信息
	var eventCount int64
	var minObs, maxObs sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(observed_at), MAX(observed_at) FROM event_journal;
	`).Scan(&eventCount, &minObs, &maxObs)
	if err != nil {
		return nil, fmt.Errorf("failed to query journal boundary: %w", err)
	}

	boundaryData := map[string]any{
		"eventCount":       eventCount,
		"minObservedAt":    minObs.String,
		"maxObservedAt":    maxObs.String,
		"reconciledTime":   now.Format(time.RFC3339Nano),
	}
	boundaryJSON, _ := json.Marshal(boundaryData)

	// 2. 插入 accounting_runs (初始状态 running)
	insertRunSQL := `
	INSERT INTO accounting_runs (
		run_id, algorithm_version, started_at, status, source_journal_event_count, source_boundary_json, notes
	) VALUES (?, ?, ?, 'running', ?, ?, ?);
	`
	if _, err := tx.ExecContext(ctx, insertRunSQL, runID, AccountingAlgorithmVersion, now.Format(time.RFC3339Nano), eventCount, string(boundaryJSON), notes); err != nil {
		return nil, fmt.Errorf("failed to insert accounting_run: %w", err)
	}

	// 3. 读取所有 connections 执行 Conservative Relay Reconciliation (E3)
	relayMap, err := reconcileRelayRelations(ctx, tx, runID)
	if err != nil {
		return nil, fmt.Errorf("relay reconciliation failed: %w", err)
	}

	// 4. 派生 accounted_traffic (E2)
	if err := deriveAccountedTraffic(ctx, tx, runID, relayMap); err != nil {
		return nil, fmt.Errorf("accounted traffic derivation failed: %w", err)
	}

	// 5. 校验 Invariants (E3.6)
	var rawUpSum, rawDownSum, accUpSum, accDownSum int64
	var negCount int64
	err = tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(raw_upload), 0),
			COALESCE(SUM(raw_download), 0),
			COALESCE(SUM(accounted_upload), 0),
			COALESCE(SUM(accounted_download), 0),
			COUNT(CASE WHEN accounted_upload < 0 OR accounted_download < 0 THEN 1 END)
		FROM accounted_traffic
		WHERE run_id = ?;
	`, runID).Scan(&rawUpSum, &rawDownSum, &accUpSum, &accDownSum, &negCount)
	if err != nil {
		return nil, fmt.Errorf("failed to validate accounting invariants: %w", err)
	}

	if negCount > 0 {
		return nil, fmt.Errorf("%w: negative accounted bytes detected (%d rows)", ErrAccountingInvariantBroken, negCount)
	}
	if accUpSum > rawUpSum || accDownSum > rawDownSum {
		return nil, fmt.Errorf("%w: accounted totals exceed raw totals (up: %d > %d, down: %d > %d)", ErrAccountingInvariantBroken, accUpSum, rawUpSum, accDownSum, rawDownSum)
	}

	// 6. 生成 Hourly Materialized Aggregations (E5)
	if err := rebuildHourlyAggregates(ctx, tx, runID); err != nil {
		return nil, fmt.Errorf("hourly aggregation rebuild failed: %w", err)
	}

	// 7. 更新 run 状态为 completed
	completedAt := time.Now().UTC()
	completedAtStr := completedAt.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounting_runs SET
			status = 'completed',
			completed_at = ?
		WHERE run_id = ?;
	`, completedAtStr, runID); err != nil {
		return nil, fmt.Errorf("failed to complete accounting run: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit accounting run: %w", err)
	}

	return &AccountingRunRecord{
		RunID:                   runID,
		AlgorithmVersion:        AccountingAlgorithmVersion,
		StartedAt:               now,
		CompletedAt:             &completedAt,
		Status:                  AccountingRunCompleted,
		SourceJournalEventCount: eventCount,
		SourceBoundaryJSON:      string(boundaryJSON),
		Notes:                   notes,
	}, nil
}

// connKey 唯一标识一条连接快照生命周期
type connKey struct {
	sessionID    string
	epochID      int
	connectionID string
}

type connInfo struct {
	key                     connKey
	firstObs, lastObs       time.Time
	route                   types.RouteType
	attributionClass        types.AttributionClass
	process, host, destIP   string
	chains                  []string
	monitoredUp, monitoredDown int64
}

// reconcileRelayRelations 实现 E3 的 Conservative Relay Reconciliation
func reconcileRelayRelations(ctx context.Context, tx *sql.Tx, runID string) (map[connKey]AccountingClass, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			session_id, epoch_id, connection_id, first_observed_at, last_observed_at,
			route, latest_attribution_class, process, host, destination_ip, chains_json,
			monitored_upload_total, monitored_download_total
		FROM connections;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allConns []connInfo
	for rows.Next() {
		var c connInfo
		var firstStr, lastStr, chainsJSON string
		if err := rows.Scan(
			&c.key.sessionID, &c.key.epochID, &c.key.connectionID, &firstStr, &lastStr,
			&c.route, &c.attributionClass, &c.process, &c.host, &c.destIP, &chainsJSON,
			&c.monitoredUp, &c.monitoredDown,
		); err != nil {
			return nil, err
		}
		c.firstObs, _ = time.Parse(time.RFC3339Nano, firstStr)
		c.lastObs, _ = time.Parse(time.RFC3339Nano, lastStr)
		if chainsJSON != "" {
			_ = json.Unmarshal([]byte(chainsJSON), &c.chains)
		}
		allConns = append(allConns, c)
	}

	// 按 session + epoch 分组
	grouped := make(map[string][]connInfo)
	for _, c := range allConns {
		groupKey := fmt.Sprintf("%s:%d", c.key.sessionID, c.key.epochID)
		grouped[groupKey] = append(grouped[groupKey], c)
	}

	resultClasses := make(map[connKey]AccountingClass)

	// 针对每个组执行保守配对
	for _, conns := range grouped {
		var candidates []connInfo
		var logicals []connInfo

		for _, c := range conns {
			// Candidate 判定：最新归因为 candidate / duplicate，或缺 process 且为 PROXY
			isCandidate := c.attributionClass == types.ClassRelayCandidate ||
				c.attributionClass == types.ClassConfirmedRelayDuplicate ||
				(c.process == "" && c.route == types.RouteProxy && len(c.chains) > 0)

			if isCandidate {
				candidates = append(candidates, c)
			} else {
				logicals = append(logicals, c)
				resultClasses[c.key] = ClassUnique
			}
		}

		// 为 candidate 寻找 logical matches
		candidateMatches := make(map[connKey][]connInfo)
		logicalMatches := make(map[connKey][]connInfo)

		for _, cand := range candidates {
			for _, log := range logicals {
				// 1. 结构关联检查：物理出站节点必须一致，或 logical 链路包含 candidate 节点
				structMatch := false
				if len(cand.chains) > 0 && len(log.chains) > 0 {
					if cand.chains[0] == log.chains[0] {
						structMatch = true
					}
				}
				if !structMatch && cand.destIP != "" && cand.destIP == log.destIP {
					structMatch = true
				}
				if !structMatch {
					continue
				}

				// 2. 时间重叠检查：实质生命周期重叠
				if cand.lastObs.Before(log.firstObs) || cand.firstObs.After(log.lastObs) {
					continue
				}

				// 3. 流量吻合度检查：相对误差在 15% 或绝对误差 2048 字节内
				upDiff := math.Abs(float64(cand.monitoredUp - log.monitoredUp))
				downDiff := math.Abs(float64(cand.monitoredDown - log.monitoredDown))
				maxUp := math.Max(float64(cand.monitoredUp), float64(log.monitoredUp))
				maxDown := math.Max(float64(cand.monitoredDown), float64(log.monitoredDown))

				upMatch := upDiff <= 2048 || (maxUp > 0 && upDiff/maxUp <= 0.15)
				downMatch := downDiff <= 2048 || (maxDown > 0 && downDiff/maxDown <= 0.15)

				if upMatch && downMatch {
					candidateMatches[cand.key] = append(candidateMatches[cand.key], log)
					logicalMatches[log.key] = append(logicalMatches[log.key], cand)
				}
			}
		}

		// 严格判定 1-to-1 confirmed 与 ambiguous / unpaired
		for _, cand := range candidates {
			matchedLogicals := candidateMatches[cand.key]

			if len(matchedLogicals) == 0 {
				// Unpaired missing attribution
				resultClasses[cand.key] = ClassMissingAttribution
				_ = insertRelayRelation(tx, runID, cand.key, nil, RelayUnpaired, "no_matching_logical_connection")
			} else if len(matchedLogicals) == 1 {
				singleLogical := matchedLogicals[0]
				// 检查反向 1-to-1 是否唯一
				if len(logicalMatches[singleLogical.key]) == 1 {
					// Confirmed 1-to-1 relay
					resultClasses[cand.key] = ClassConfirmedRelayDuplicate
					_ = insertRelayRelation(tx, runID, cand.key, &singleLogical.key, RelayConfirmed, "strict_1to1_structural_overlap_match")
				} else {
					// N-to-1 ambiguity
					resultClasses[cand.key] = ClassAmbiguousRelay
					_ = insertRelayRelation(tx, runID, cand.key, &singleLogical.key, RelayAmbiguous, "n_to_1_logical_ambiguity")
				}
			} else {
				// 1-to-N ambiguity
				resultClasses[cand.key] = ClassAmbiguousRelay
				_ = insertRelayRelation(tx, runID, cand.key, nil, RelayAmbiguous, "1_to_n_candidate_ambiguity")
			}
		}
	}

	return resultClasses, nil
}

func insertRelayRelation(tx *sql.Tx, runID string, candKey connKey, logKey *connKey, status RelayRelationStatus, reason string) error {
	evidence := map[string]any{
		"reason": reason,
	}
	if logKey != nil {
		evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", logKey.sessionID, logKey.epochID, logKey.connectionID)
	}
	evJSON, _ := json.Marshal(evidence)

	var logSess, logConn sql.NullString
	var logEpoch sql.NullInt64
	if logKey != nil {
		logSess = sql.NullString{String: logKey.sessionID, Valid: true}
		logEpoch = sql.NullInt64{Int64: int64(logKey.epochID), Valid: true}
		logConn = sql.NullString{String: logKey.connectionID, Valid: true}
	}

	insertSQL := `
	INSERT INTO relay_relations (
		run_id, candidate_session_id, candidate_epoch_id, candidate_connection_id,
		logical_session_id, logical_epoch_id, logical_connection_id,
		status, evidence_json, derivation_version
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := tx.Exec(insertSQL,
		runID, candKey.sessionID, candKey.epochID, candKey.connectionID,
		logSess, logEpoch, logConn, string(status), string(evJSON), AccountingAlgorithmVersion,
	)
	return err
}

// deriveAccountedTraffic 遍历 raw connection_traffic 派生 accounted_traffic
func deriveAccountedTraffic(ctx context.Context, tx *sql.Tx, runID string, relayMap map[connKey]AccountingClass) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			t.event_id, t.session_id, t.epoch_id, t.connection_id, t.observed_at,
			t.interval_start, t.interval_end, t.precision, t.delta_upload, t.delta_download,
			c.route, c.process, c.process_path, c.host, c.sniff_host, c.destination_ip, c.network,
			c.rule, c.rule_payload, c.chains_json
		FROM connection_traffic t
		JOIN connections c ON t.session_id = c.session_id AND t.epoch_id = c.epoch_id AND t.connection_id = c.connection_id
		ORDER BY t.frame_sequence ASC, t.event_sequence ASC, t.observed_at ASC;
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO accounted_traffic (
			run_id, source_event_id, session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route,
			raw_upload, raw_download, accounted_upload, accounted_download,
			accounting_class, process, process_path, host, sniff_host, destination_ip, network,
			rule, rule_payload, final_proxy, top_policy_group, dimension_derivation_version
		) VALUES (
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?
		);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var eventID, sessID, connID, obsAtStr, prec, route, chainsJSON string
		var epochID int
		var intStart, intEnd, proc, procPath, host, sniffHost, destIP, network, rule, rulePayload sql.NullString
		var deltaUp, deltaDown int64

		if err := rows.Scan(
			&eventID, &sessID, &epochID, &connID, &obsAtStr,
			&intStart, &intEnd, &prec, &deltaUp, &deltaDown,
			&route, &proc, &procPath, &host, &sniffHost, &destIP, &network,
			&rule, &rulePayload, &chainsJSON,
		); err != nil {
			return err
		}

		key := connKey{sessionID: sessID, epochID: epochID, connectionID: connID}
		accClass := relayMap[key]
		if accClass == "" {
			accClass = ClassUnique
		}

		// 根据 accounting class 确定真实入账流量
		var accUp, accDown int64
		if accClass == ClassConfirmedRelayDuplicate {
			accUp = 0
			accDown = 0
		} else {
			accUp = deltaUp
			accDown = deltaDown
		}

		// 派生 final_proxy 与 top_policy_group (E5.2)
		var chains []string
		if chainsJSON != "" {
			_ = json.Unmarshal([]byte(chainsJSON), &chains)
		}
		var finalProxy, topGroup sql.NullString
		if route == string(types.RouteDirect) {
			finalProxy = sql.NullString{String: "DIRECT", Valid: true}
		} else if len(chains) > 0 {
			finalProxy = sql.NullString{String: chains[0], Valid: true}
			topGroup = sql.NullString{String: chains[len(chains)-1], Valid: true}
		}

		if _, err := stmt.ExecContext(ctx,
			runID, eventID, sessID, epochID, connID, obsAtStr,
			intStart, intEnd, prec, route,
			deltaUp, deltaDown, accUp, accDown,
			string(accClass), proc, procPath, host, sniffHost, destIP, network,
			rule, rulePayload, finalProxy, topGroup, DimensionDerivationVersion,
		); err != nil {
			return err
		}
	}

	return nil
}

// rebuildHourlyAggregates 实现 E5: 从 accounted_traffic 聚合写入 usage_hourly_dimensions
func rebuildHourlyAggregates(ctx context.Context, tx *sql.Tx, runID string) error {
	// 读取本轮所有 accounted_traffic
	rows, err := tx.QueryContext(ctx, `
		SELECT
			source_event_id, session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision, route,
			accounted_upload, accounted_download, accounting_class,
			process, host, destination_ip, network, rule, rule_payload, final_proxy, top_policy_group
		FROM accounted_traffic
		WHERE run_id = ?;
	`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type aggKey struct {
		bucketStart time.Time
		dimType     string
		dimKey      string
		route       types.RouteType
	}
	type aggVal struct {
		upload, download                     int64
		exactUp, exactDown                   int64
		estUp, estDown                       int64
		distinctConns                        map[string]bool
	}

	aggregates := make(map[aggKey]*aggVal)

	getOrCreate := func(k aggKey) *aggVal {
		v, ok := aggregates[k]
		if !ok {
			v = &aggVal{distinctConns: make(map[string]bool)}
			aggregates[k] = v
		}
		return v
	}

	for rows.Next() {
		var eventID, sessID, connID, obsAtStr, prec, route, accClass string
		var epochID int
		var intStart, intEnd, proc, host, destIP, network, rule, rulePayload, finalProxy, topGroup sql.NullString
		var accUp, accDown int64

		if err := rows.Scan(
			&eventID, &sessID, &epochID, &connID, &obsAtStr,
			&intStart, &intEnd, &prec, &route,
			&accUp, &accDown, &accClass,
			&proc, &host, &destIP, &network, &rule, &rulePayload, &finalProxy, &topGroup,
		); err != nil {
			return err
		}

		connFullKey := fmt.Sprintf("%s:%d:%s", sessID, epochID, connID)
		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		// 确定时间分配 (E5.3: exact snapshot vs interval跨小时分配)
		type timeAllocation struct {
			bucketStart time.Time
			upBytes     int64
			downBytes   int64
			isExact     bool
		}
		var allocations []timeAllocation

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			// 整小时 Bucket
			bucket := obsTime.Truncate(time.Hour)
			allocations = append(allocations, timeAllocation{
				bucketStart: bucket,
				upBytes:     accUp,
				downBytes:   accDown,
				isExact:     true,
			})
		} else {
			// 跨区间分摊分配（严格整数纳秒按比例分摊，余数补齐，保证字节守恒）
			startTime, _ := time.Parse(time.RFC3339Nano, intStart.String)
			endTime, _ := time.Parse(time.RFC3339Nano, intEnd.String)
			startTime = startTime.UTC()
			endTime = endTime.UTC()

			if !endTime.After(startTime) {
				bucket := obsTime.Truncate(time.Hour)
				allocations = append(allocations, timeAllocation{
					bucketStart: bucket, upBytes: accUp, downBytes: accDown, isExact: false,
				})
			} else {
				totalDuration := endTime.Sub(startTime)
				curr := startTime
				var allocatedUp, allocatedDown int64

				for curr.Before(endTime) {
					bucket := curr.Truncate(time.Hour)
					nextBucket := bucket.Add(time.Hour)
					bucketEnd := nextBucket
					if bucketEnd.After(endTime) {
						bucketEnd = endTime
					}
					overlap := bucketEnd.Sub(curr)
					ratio := float64(overlap) / float64(totalDuration)

					pieceUp := int64(math.Floor(float64(accUp) * ratio))
					pieceDown := int64(math.Floor(float64(accDown) * ratio))

					allocations = append(allocations, timeAllocation{
						bucketStart: bucket,
						upBytes:     pieceUp,
						downBytes:   pieceDown,
						isExact:     false,
					})
					allocatedUp += pieceUp
					allocatedDown += pieceDown
					curr = bucketEnd
				}

				// 余数确定性补入第一个 bucket，保证绝对守恒
				remUp := accUp - allocatedUp
				remDown := accDown - allocatedDown
				if len(allocations) > 0 {
					allocations[0].upBytes += remUp
					allocations[0].downBytes += remDown
				}
			}
		}

		// 多维度映射
		type dimPair struct {
			dimType string
			dimKey  string
		}
		dims := []dimPair{
			{dimType: "total", dimKey: "total"},
		}
		if proc.Valid && proc.String != "" {
			dims = append(dims, dimPair{dimType: "process", dimKey: proc.String})
		}
		if host.Valid && host.String != "" {
			dims = append(dims, dimPair{dimType: "host", dimKey: host.String})
		}
		if destIP.Valid && destIP.String != "" {
			dims = append(dims, dimPair{dimType: "destination_ip", dimKey: destIP.String})
		}
		if rule.Valid && rule.String != "" {
			dims = append(dims, dimPair{dimType: "rule", dimKey: rule.String})
		}
		if rulePayload.Valid && rulePayload.String != "" {
			dims = append(dims, dimPair{dimType: "rule_payload", dimKey: rulePayload.String})
		}
		if finalProxy.Valid && finalProxy.String != "" {
			dims = append(dims, dimPair{dimType: "final_proxy", dimKey: finalProxy.String})
		}
		if topGroup.Valid && topGroup.String != "" {
			dims = append(dims, dimPair{dimType: "top_policy_group", dimKey: topGroup.String})
		}
		if network.Valid && network.String != "" {
			dims = append(dims, dimPair{dimType: "network", dimKey: network.String})
		}

		// 累加到内存聚合表
		for _, alloc := range allocations {
			for _, d := range dims {
				k := aggKey{
					bucketStart: alloc.bucketStart,
					dimType:     d.dimType,
					dimKey:      d.dimKey,
					route:       types.RouteType(route),
				}
				v := getOrCreate(k)
				v.upload += alloc.upBytes
				v.download += alloc.downBytes
				if alloc.isExact {
					v.exactUp += alloc.upBytes
					v.exactDown += alloc.downBytes
				} else {
					v.estUp += alloc.upBytes
					v.estDown += alloc.downBytes
				}
				if accClass != string(ClassConfirmedRelayDuplicate) {
					v.distinctConns[connFullKey] = true
				}
			}
		}
	}

	// 批量写入 usage_hourly_dimensions
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO usage_hourly_dimensions (
			run_id, bucket_start, dimension_type, dimension_key, route,
			upload_bytes, download_bytes, connection_count,
			exact_upload_bytes, exact_download_bytes,
			estimated_upload_bytes, estimated_download_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range aggregates {
		bucketStr := k.bucketStart.UTC().Format(time.RFC3339Nano)
		connCount := int64(len(v.distinctConns))
		if _, err := stmt.ExecContext(ctx,
			runID, bucketStr, k.dimType, k.dimKey, string(k.route),
			v.upload, v.download, connCount,
			v.exactUp, v.exactDown, v.estUp, v.estDown,
		); err != nil {
			return err
		}
	}

	return nil
}
