package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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
		"eventCount":     eventCount,
		"minObservedAt":  minObs.String,
		"maxObservedAt":  maxObs.String,
		"reconciledTime": now.Format(time.RFC3339Nano),
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

	// 4. 派生 accounted_traffic (E2: 元数据取源于当时发生事件的 event_journal)
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

	// 6. 生成 Hourly Materialized Aggregations (E5: 纯整型纳秒分摊，无 float64)
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

type connKey struct {
	sessionID    string
	epochID      int
	connectionID string
}

type connInfo struct {
	key                        connKey
	firstObs, lastObs          time.Time
	route                      types.RouteType
	attributionClass           types.AttributionClass
	process, host, destIP      string
	chains                     []string
	monitoredUp, monitoredDown int64
}

// reconcileRelayRelations 实现收紧的 Conservative Relay Reconciliation
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

	for _, conns := range grouped {
		var candidates []connInfo
		var logicals []connInfo

		for _, c := range conns {
			hasProcess := strings.TrimSpace(c.process) != ""
			// Candidate: 必须具备 Phase 1 / Phase 0 确立的 candidate 特征
			isCandidate := c.attributionClass == types.ClassRelayCandidate ||
				c.attributionClass == types.ClassConfirmedRelayDuplicate ||
				(!hasProcess && c.route == types.RouteProxy && len(c.chains) > 0)

			// Logical: 必须是明确已知应用连接
			isLogical := c.attributionClass == types.ClassKnownApplication || hasProcess

			if isCandidate {
				candidates = append(candidates, c)
			} else if isLogical {
				logicals = append(logicals, c)
				resultClasses[c.key] = ClassUnique
			} else {
				// 未配对的未知非中继连接
				resultClasses[c.key] = ClassMissingAttribution
			}
		}

		candidateMatches := make(map[connKey][]connInfo)
		logicalMatches := make(map[connKey][]connInfo)
		matchEvidenceMap := make(map[string]map[string]any)

		for _, cand := range candidates {
			for _, log := range logicals {
				// 1. Structural Relation: 物理出站节点必须一致 (chains[0])
				if len(cand.chains) == 0 || len(log.chains) == 0 {
					continue
				}
				if cand.chains[0] != log.chains[0] {
					continue
				}

				// 2. 生命周期实质时间重叠
				if cand.lastObs.Before(log.firstObs) || cand.firstObs.After(log.lastObs) {
					continue
				}
				overlapStart := cand.firstObs
				if log.firstObs.After(overlapStart) {
					overlapStart = log.firstObs
				}
				overlapEnd := cand.lastObs
				if log.lastObs.Before(overlapEnd) {
					overlapEnd = log.lastObs
				}
				overlapMs := overlapEnd.Sub(overlapStart).Milliseconds()
				if overlapMs < 0 {
					continue
				}

				// 3. Minimum traffic 门槛与 Conservative tolerance
				hasMinTraffic := (cand.monitoredUp >= 64 || cand.monitoredDown >= 64 || log.monitoredUp >= 64 || log.monitoredDown >= 64)
				if !hasMinTraffic {
					continue
				}

				upDiff := cand.monitoredUp - log.monitoredUp
				if upDiff < 0 {
					upDiff = -upDiff
				}
				downDiff := cand.monitoredDown - log.monitoredDown
				if downDiff < 0 {
					downDiff = -downDiff
				}

				maxUp := log.monitoredUp
				if cand.monitoredUp > maxUp {
					maxUp = cand.monitoredUp
				}
				maxDown := log.monitoredDown
				if cand.monitoredDown > maxDown {
					maxDown = cand.monitoredDown
				}

				upRatio := 0.0
				if maxUp > 0 {
					upRatio = float64(upDiff) / float64(maxUp)
				}
				downRatio := 0.0
				if maxDown > 0 {
					downRatio = float64(downDiff) / float64(maxDown)
				}

				// 严格容差：<=1024 字节或 <= 10% 相对误差
				upMatch := upDiff <= 1024 || (maxUp > 0 && upRatio <= 0.10)
				downMatch := downDiff <= 1024 || (maxDown > 0 && downRatio <= 0.10)

				if upMatch && downMatch {
					candidateMatches[cand.key] = append(candidateMatches[cand.key], log)
					logicalMatches[log.key] = append(logicalMatches[log.key], cand)

					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, log.key.connectionID)
					matchEvidenceMap[pairKey] = map[string]any{
						"overlapMs":         overlapMs,
						"candidateChains":   cand.chains,
						"logicalChains":     log.chains,
						"candidateTotals":   map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
						"logicalTotals":     map[string]int64{"upload": log.monitoredUp, "download": log.monitoredDown},
						"uploadDiffRatio":   upRatio,
						"downloadDiffRatio": downRatio,
					}
				}
			}
		}

		// 判定 1-to-1 confirmed 与 ambiguous / unpaired 并持久化 relation
		for _, cand := range candidates {
			matchedLogicals := candidateMatches[cand.key]

			if len(matchedLogicals) == 0 {
				resultClasses[cand.key] = ClassMissingAttribution
				evidence := map[string]any{
					"decisionReason":  "unpaired_no_matching_logical_connection",
					"candidateChains": cand.chains,
					"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
				}
				if err := insertRelayRelation(tx, runID, cand.key, nil, RelayUnpaired, evidence); err != nil {
					return nil, err
				}
			} else if len(matchedLogicals) == 1 {
				singleLogical := matchedLogicals[0]
				if len(logicalMatches[singleLogical.key]) == 1 {
					// 严格 1-to-1 confirmed
					resultClasses[cand.key] = ClassConfirmedRelayDuplicate
					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, singleLogical.key.connectionID)
					evidence := matchEvidenceMap[pairKey]
					if evidence == nil {
						evidence = make(map[string]any)
					}
					evidence["decisionReason"] = "strict_1to1_structural_overlap_match"
					if err := insertRelayRelation(tx, runID, cand.key, &singleLogical.key, RelayConfirmed, evidence); err != nil {
						return nil, err
					}
				} else {
					// N-to-1 歧义 -> 不扣减
					resultClasses[cand.key] = ClassAmbiguousRelay
					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, singleLogical.key.connectionID)
					evidence := matchEvidenceMap[pairKey]
					if evidence == nil {
						evidence = make(map[string]any)
					}
					evidence["decisionReason"] = "n_to_1_logical_ambiguity"
					if err := insertRelayRelation(tx, runID, cand.key, &singleLogical.key, RelayAmbiguous, evidence); err != nil {
						return nil, err
					}
				}
			} else {
				// 1-to-N 歧义 -> 不扣减
				resultClasses[cand.key] = ClassAmbiguousRelay
				evidence := map[string]any{
					"decisionReason":  "1_to_n_candidate_ambiguity",
					"candidateChains": cand.chains,
					"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
					"matchedCount":    len(matchedLogicals),
				}
				if err := insertRelayRelation(tx, runID, cand.key, nil, RelayAmbiguous, evidence); err != nil {
					return nil, err
				}
			}
		}
	}

	return resultClasses, nil
}

func insertRelayRelation(tx *sql.Tx, runID string, candKey connKey, logKey *connKey, status RelayRelationStatus, evidence map[string]any) error {
	if logKey != nil {
		evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", logKey.sessionID, logKey.epochID, logKey.connectionID)
	}
	evidence["candidateKey"] = fmt.Sprintf("%s:%d:%s", candKey.sessionID, candKey.epochID, candKey.connectionID)
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

// deriveAccountedTraffic 遍历 raw connection_traffic，元数据严格取源于当时发生的 event_journal event
func deriveAccountedTraffic(ctx context.Context, tx *sql.Tx, runID string, relayMap map[connKey]AccountingClass) error {
	// 1. 准备 accounted_traffic 插入语句
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

	// 2. 按权威序列严格流式遍历所有 journal 事件，动态维护时间轴因果状态
	jRows, err := tx.QueryContext(ctx, `
		SELECT event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type, observed_at, event_json
		FROM event_journal
		ORDER BY frame_sequence ASC, event_sequence ASC;
	`)
	if err != nil {
		return err
	}
	defer jRows.Close()

	type histMeta struct {
		process, processPath, host, sniffHost, destIP, network, rule, rulePayload string
		chains                                                                    []string
		route                                                                     types.RouteType
	}
	currentMeta := make(map[connKey]histMeta)

	for jRows.Next() {
		var eventID, sessID, eventType, obsAtStr, ej string
		var epochID int
		var frameSeq, eventSeq int64

		if err := jRows.Scan(&eventID, &sessID, &epochID, &frameSeq, &eventSeq, &eventType, &obsAtStr, &ej); err != nil {
			return err
		}

		var ev types.CollectorEvent
		dec := json.NewDecoder(strings.NewReader(ej))
		dec.UseNumber()
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("failed to decode event %s: %w", eventID, err)
		}

		k := connKey{sessionID: sessID, epochID: epochID, connectionID: ev.ConnectionID}

		// 如果是元数据注入/更新事件，推进因果状态机
		if eventType == string(types.EventConnectionBootstrap) ||
			eventType == string(types.EventConnectionNew) ||
			eventType == string(types.EventConnectionMetadataUpdated) {
			m := currentMeta[k]
			if ev.Metadata.Process != "" {
				m.process = ev.Metadata.Process
			}
			if ev.Metadata.ProcessPath != "" {
				m.processPath = ev.Metadata.ProcessPath
			}
			if ev.Metadata.Host != "" {
				m.host = ev.Metadata.Host
			}
			if ev.Metadata.SniffHost != "" {
				m.sniffHost = ev.Metadata.SniffHost
			}
			if ev.Metadata.DestinationIP != "" {
				m.destIP = ev.Metadata.DestinationIP
			}
			if ev.Metadata.Network != "" {
				m.network = ev.Metadata.Network
			}
			if ev.Rule != "" {
				m.rule = ev.Rule
			}
			if ev.RulePayload != "" {
				m.rulePayload = ev.RulePayload
			}
			if len(ev.Chains) > 0 {
				m.chains = ev.Chains
			}
			if ev.Route != "" {
				m.route = ev.Route
			}
			currentMeta[k] = m
		}

		// 仅对产生流量的事件 (ConnectionNew / ConnectionDelta) 派生 accounted_traffic
		if eventType == string(types.EventConnectionNew) || eventType == string(types.EventConnectionDelta) {
			accClass := relayMap[k]
			if accClass == "" {
				accClass = ClassUnique
			}

			hist := currentMeta[k]

			proc := ev.Metadata.Process
			if proc == "" {
				proc = hist.process
			}
			procPath := ev.Metadata.ProcessPath
			if procPath == "" {
				procPath = hist.processPath
			}
			host := ev.Metadata.Host
			if host == "" {
				host = hist.host
			}
			sniffHost := ev.Metadata.SniffHost
			if sniffHost == "" {
				sniffHost = hist.sniffHost
			}
			destIP := ev.Metadata.DestinationIP
			if destIP == "" {
				destIP = hist.destIP
			}
			network := ev.Metadata.Network
			if network == "" {
				network = hist.network
			}
			rule := ev.Rule
			if rule == "" {
				rule = hist.rule
			}
			rulePayload := ev.RulePayload
			if rulePayload == "" {
				rulePayload = hist.rulePayload
			}
			chains := ev.Chains
			if len(chains) == 0 {
				chains = hist.chains
			}
			route := ev.Route
			if route == "" {
				route = hist.route
			}
			if route == "" {
				route = types.RouteUnknown
			}

			var accUp, accDown int64
			if accClass == ClassConfirmedRelayDuplicate {
				accUp = 0
				accDown = 0
			} else {
				accUp = ev.DeltaUpload
				accDown = ev.DeltaDownload
			}

			var finalProxy, topGroup sql.NullString
			if route == types.RouteDirect {
				finalProxy = sql.NullString{String: "DIRECT", Valid: true}
			} else if len(chains) > 0 {
				finalProxy = sql.NullString{String: chains[0], Valid: true}
				topGroup = sql.NullString{String: chains[len(chains)-1], Valid: true}
			}

			var intStart, intEnd sql.NullString
			if len(ev.AttributionInterval) >= 2 {
				intStart = sql.NullString{String: ev.AttributionInterval[0], Valid: true}
				intEnd = sql.NullString{String: ev.AttributionInterval[1], Valid: true}
			}
			prec := ev.Precision
			if prec == "" {
				prec = "exact_snapshot"
			}

			if _, err := stmt.ExecContext(ctx,
				runID, eventID, sessID, epochID, ev.ConnectionID, obsAtStr,
				intStart, intEnd, prec, string(route),
				ev.DeltaUpload, ev.DeltaDownload, accUp, accDown,
				string(accClass), proc, procPath, host, sniffHost, destIP, network,
				rule, rulePayload, finalProxy, topGroup, DimensionDerivationVersion,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// rebuildHourlyAggregates 实现纯整型纳秒分摊，杜绝 float64 精度损失 (E5.3, E8.5)
func rebuildHourlyAggregates(ctx context.Context, tx *sql.Tx, runID string) error {
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
		upload, download   int64
		exactUp, exactDown int64
		estUp, estDown     int64
		distinctConns      map[string]bool
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

		type timeAllocation struct {
			bucketStart time.Time
			upBytes     int64
			downBytes   int64
			isExact     bool
		}
		var allocations []timeAllocation

		if prec == "exact_snapshot" || !intStart.Valid || !intEnd.Valid {
			bucket := obsTime.Truncate(time.Hour)
			allocations = append(allocations, timeAllocation{
				bucketStart: bucket,
				upBytes:     accUp,
				downBytes:   accDown,
				isExact:     true,
			})
		} else {
			// 纯整型纳秒分摊算法 (去除所有 float64)
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
				totalNs := endTime.Sub(startTime).Nanoseconds()
				curr := startTime
				var allocatedUp, allocatedDown int64

				for curr.Before(endTime) {
					bucket := curr.Truncate(time.Hour)
					nextBucket := bucket.Add(time.Hour)
					bucketEnd := nextBucket
					if bucketEnd.After(endTime) {
						bucketEnd = endTime
					}
					overlapNs := bucketEnd.Sub(curr).Nanoseconds()

					var pieceUp, pieceDown int64
					if totalNs > 0 {
						pieceUp = (accUp * overlapNs) / totalNs
						pieceDown = (accDown * overlapNs) / totalNs
					}

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

				// 余数整型补偿到首个 bucket，保证绝对守恒
				remUp := accUp - allocatedUp
				remDown := accDown - allocatedDown
				if len(allocations) > 0 {
					allocations[0].upBytes += remUp
					allocations[0].downBytes += remDown
				}
			}
		}

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
