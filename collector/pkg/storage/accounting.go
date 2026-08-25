package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

const (
	AccountingAlgorithmVersion = "reconciled-accounting-v1"
	DimensionDerivationVersion = "chain-semantics-v1"
	DefaultBatchSize           = 1000
)

var runSeqCounter int64

// RebuildAccounting 从不可变原始证据执行非阻塞、绑定序列边界的完整核算 (F2 Non-Blocking Bounded Rebuild)
func RebuildAccounting(ctx context.Context, db *sql.DB, notes string) (*AccountingRunRecord, error) {
	now := time.Now().UTC()
	seq := atomic.AddInt64(&runSeqCounter, 1)
	runID := fmt.Sprintf("run-%d-%d", now.UnixNano(), seq)

	// -------------------------------------------------------------
	// 阶段 A: Capture Source Boundary (极短写事务，带锁重试)
	// -------------------------------------------------------------
	var boundarySeq int64
	var eventCount int64
	var minObs, maxObs sql.NullString

	err := execWithTxRetry(ctx, db, 15, func(tx *sql.Tx) error {
		// 检查是否有未完成的 running run
		var runningRunID, runningStartStr string
		err := tx.QueryRowContext(ctx, `
			SELECT run_id, started_at FROM accounting_runs
			WHERE status = 'running'
			ORDER BY started_at DESC LIMIT 1;
		`).Scan(&runningRunID, &runningStartStr)
		if err == nil {
			runningStart, _ := time.Parse(time.RFC3339Nano, runningStartStr)
			// 如果超过 10 分钟，判定为崩溃遗留，标记 failed
			if time.Since(runningStart) > 10*time.Minute {
				_, _ = tx.ExecContext(ctx, `
					UPDATE accounting_runs SET status = 'failed', failed_reason = 'abandoned_crashed_run'
					WHERE run_id = ?;
				`, runningRunID)
			} else {
				return ErrAccountingAlreadyRunning
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to check running accounting runs: %w", err)
		}

		// 读取当前最大 journal_sequence 与总数
		err = tx.QueryRowContext(ctx, `
			SELECT
				COALESCE(MAX(journal_sequence), 0),
				COUNT(*),
				MIN(observed_at),
				MAX(observed_at)
			FROM event_journal;
		`).Scan(&boundarySeq, &eventCount, &minObs, &maxObs)
		if err != nil {
			return fmt.Errorf("failed to query journal boundary: %w", err)
		}

		boundaryData := map[string]any{
			"eventCount":               eventCount,
			"sourceJournalSequenceMax": boundarySeq,
			"minObservedAt":            minObs.String,
			"maxObservedAt":            maxObs.String,
			"reconciledTime":           now.Format(time.RFC3339Nano),
		}
		boundaryJSON, _ := json.Marshal(boundaryData)

		insertRunSQL := `
		INSERT INTO accounting_runs (
			run_id, algorithm_version, started_at, status,
			source_journal_event_count, source_journal_sequence_max,
			source_boundary_json, notes
		) VALUES (?, ?, ?, 'running', ?, ?, ?, ?);
		`
		if _, err := tx.ExecContext(ctx, insertRunSQL,
			runID, AccountingAlgorithmVersion, now.Format(time.RFC3339Nano),
			eventCount, boundarySeq, string(boundaryJSON), notes,
		); err != nil {
			return fmt.Errorf("failed to insert running accounting_run: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// 错误/取消捕获闭包：确保失败时将 run 标记为 failed
	markFailed := func(failErr error) {
		ctxFail, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = execWithTxRetry(ctxFail, db, 5, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctxFail, `
				UPDATE accounting_runs SET status = 'failed', failed_reason = ?
				WHERE run_id = ?;
			`, failErr.Error(), runID)
			return err
		})
	}

	// -------------------------------------------------------------
	// 阶段 B: Construct Bounded Connection Summaries (只读，<= boundarySeq)
	// -------------------------------------------------------------
	relayMap, relayRelations, err := reconcileBoundedRelayRelations(ctx, db, runID, boundarySeq)
	if err != nil {
		markFailed(err)
		return nil, fmt.Errorf("relay reconciliation failed: %w", err)
	}

	// -------------------------------------------------------------
	// 阶段 C: Batch Write relay_relations (分批短事务，每批让出写锁)
	// -------------------------------------------------------------
	if err := batchWriteRelayRelations(ctx, db, relayRelations); err != nil {
		markFailed(err)
		return nil, fmt.Errorf("batch write relay_relations failed: %w", err)
	}

	// -------------------------------------------------------------
	// 阶段 D: Read Bounded Journal -> Derive & Batch Write accounted_traffic (读写彻底解耦)
	// -------------------------------------------------------------
	if err := streamAndBatchWriteAccountedTraffic(ctx, db, runID, boundarySeq, relayMap); err != nil {
		markFailed(err)
		return nil, fmt.Errorf("accounted traffic derivation failed: %w", err)
	}

	// -------------------------------------------------------------
	// 阶段 E: Invariants Validation
	// -------------------------------------------------------------
	var rawUpSum, rawDownSum, accUpSum, accDownSum int64
	var negCount int64
	err = db.QueryRowContext(ctx, `
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
		markFailed(err)
		return nil, fmt.Errorf("failed to validate accounting invariants: %w", err)
	}

	if negCount > 0 {
		invErr := fmt.Errorf("%w: negative accounted bytes detected (%d rows)", ErrAccountingInvariantBroken, negCount)
		markFailed(invErr)
		return nil, invErr
	}
	if accUpSum > rawUpSum || accDownSum > rawDownSum {
		invErr := fmt.Errorf("%w: accounted totals exceed raw totals (up: %d > %d, down: %d > %d)", ErrAccountingInvariantBroken, accUpSum, rawUpSum, accDownSum, rawDownSum)
		markFailed(invErr)
		return nil, invErr
	}

	// -------------------------------------------------------------
	// 阶段 F: Build & Batch Write Hourly Materialized Aggregations
	// -------------------------------------------------------------
	if err := rebuildHourlyAggregatesBatched(ctx, db, runID); err != nil {
		markFailed(err)
		return nil, fmt.Errorf("hourly aggregation rebuild failed: %w", err)
	}

	// -------------------------------------------------------------
	// 阶段 G: Publish Completed (短事务原子发布)
	// -------------------------------------------------------------
	completedAt := time.Now().UTC()
	completedAtStr := completedAt.Format(time.RFC3339Nano)
	if err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE accounting_runs SET
				status = 'completed',
				completed_at = ?
			WHERE run_id = ?;
		`, completedAtStr, runID)
		return err
	}); err != nil {
		markFailed(err)
		return nil, fmt.Errorf("failed to publish completed accounting run: %w", err)
	}

	return &AccountingRunRecord{
		RunID:                    runID,
		AlgorithmVersion:         AccountingAlgorithmVersion,
		StartedAt:                now,
		CompletedAt:              &completedAt,
		Status:                   AccountingRunCompleted,
		SourceJournalEventCount:  eventCount,
		SourceJournalSequenceMax: &boundarySeq,
		Notes:                    notes,
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
	rule, rulePayload          string
	chains                     []string
	monitoredUp, monitoredDown int64
}

// reconcileBoundedRelayRelations 从 <= boundarySeq 的不可变 Journal 事件中构建连接事实并进行 Relay 对账
func reconcileBoundedRelayRelations(ctx context.Context, db *sql.DB, runID string, boundarySeq int64) (map[connKey]AccountingClass, []RelayRelationRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, epoch_id, connection_id, event_type, observed_at, event_json
		FROM event_journal
		WHERE journal_sequence <= ? AND connection_id IS NOT NULL AND connection_id != ''
		ORDER BY frame_sequence ASC, event_sequence ASC;
	`, boundarySeq)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	connMap := make(map[connKey]*connInfo)

	for rows.Next() {
		var sessID, connID, eventType, obsAtStr, ej string
		var epochID int
		if err := rows.Scan(&sessID, &epochID, &connID, &eventType, &obsAtStr, &ej); err != nil {
			return nil, nil, err
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, obsAtStr)
		obsTime = obsTime.UTC()

		k := connKey{sessionID: sessID, epochID: epochID, connectionID: connID}
		c, ok := connMap[k]
		if !ok {
			c = &connInfo{key: k, firstObs: obsTime, lastObs: obsTime}
			connMap[k] = c
		}
		if obsTime.Before(c.firstObs) {
			c.firstObs = obsTime
		}
		if obsTime.After(c.lastObs) {
			c.lastObs = obsTime
		}

		var ev types.CollectorEvent
		dec := json.NewDecoder(strings.NewReader(ej))
		dec.UseNumber()
		if err := dec.Decode(&ev); err == nil {
			if ev.Route != "" {
				c.route = ev.Route
			}
			if ev.AttributionClass != "" {
				c.attributionClass = ev.AttributionClass
			}
			if ev.Metadata.Process != "" {
				c.process = ev.Metadata.Process
			}
			if ev.Metadata.Host != "" {
				c.host = ev.Metadata.Host
			}
			if ev.Metadata.DestinationIP != "" {
				c.destIP = ev.Metadata.DestinationIP
			}
			if ev.Rule != "" {
				c.rule = ev.Rule
			}
			if ev.RulePayload != "" {
				c.rulePayload = ev.RulePayload
			}
			if len(ev.Chains) > 0 {
				c.chains = ev.Chains
			}
			if ev.DeltaUpload > 0 || ev.DeltaDownload > 0 {
				c.monitoredUp += ev.DeltaUpload
				c.monitoredDown += ev.DeltaDownload
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading journal rows: %w", err)
	}

	// 按 session + epoch 分组
	grouped := make(map[string][]*connInfo)
	for _, c := range connMap {
		groupKey := fmt.Sprintf("%s:%d", c.key.sessionID, c.key.epochID)
		grouped[groupKey] = append(grouped[groupKey], c)
	}

	resultClasses := make(map[connKey]AccountingClass)
	var relationsToInsert []RelayRelationRecord

	for _, conns := range grouped {
		var candidates []*connInfo
		var logicals []*connInfo

		for _, c := range conns {
			hasProcess := strings.TrimSpace(c.process) != ""
			hasRule := strings.TrimSpace(c.rule) != ""
			hasProcessAndRule := hasProcess && hasRule

			// 核心原则：如果 metadata 明确具备 process+rule (或显式为 KnownApplication)，绝对不是 Candidate！
			isLogical := hasProcessAndRule || c.attributionClass == types.ClassKnownApplication

			isCandidate := false
			if !hasProcessAndRule {
				if c.attributionClass == types.ClassRelayCandidate ||
					c.attributionClass == types.ClassConfirmedRelayDuplicate ||
					(!hasProcess && !hasRule && len(c.chains) > 0 && c.route == types.RouteProxy) {
					isCandidate = true
				}
			}

			if isLogical {
				logicals = append(logicals, c)
				resultClasses[c.key] = ClassUnique
			} else if isCandidate {
				candidates = append(candidates, c)
			} else {
				resultClasses[c.key] = ClassMissingAttribution
			}
		}

		candidateMatches := make(map[connKey][]*connInfo)
		logicalMatches := make(map[connKey][]*connInfo)
		matchEvidenceMap := make(map[string]map[string]any)

		for _, cand := range candidates {
			for _, log := range logicals {
				candChains := cand.chains
				logChains := log.chains

				if len(candChains) == 0 || len(logChains) <= 1 {
					continue
				}
				if candChains[0] != logChains[0] {
					continue
				}

				var sharedHops []string
				logHopMap := make(map[string]bool, len(logChains))
				for _, hop := range logChains {
					logHopMap[hop] = true
				}
				for _, hop := range candChains {
					if logHopMap[hop] {
						sharedHops = append(sharedHops, hop)
					}
				}
				if len(sharedHops) == 0 {
					continue
				}

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

				hasTraffic := cand.monitoredUp > 500 || cand.monitoredDown > 500 || log.monitoredUp > 500 || log.monitoredDown > 500
				if !hasTraffic {
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

				upRatio := 1.0
				if maxUp > 0 {
					upRatio = float64(upDiff) / float64(maxUp)
				}
				downRatio := 1.0
				if maxDown > 0 {
					downRatio = float64(downDiff) / float64(maxDown)
				}

				upMatch := (maxUp > 0 && upRatio <= 0.05) || (upDiff < 2000 && upRatio < 0.20)
				downMatch := (maxDown > 0 && downRatio <= 0.05) || (downDiff < 2000 && downRatio < 0.20)

				var trafficMatch bool
				if log.monitoredUp > 500 && log.monitoredDown > 500 {
					trafficMatch = upMatch && downMatch
				} else if log.monitoredDown > 2000 && log.monitoredUp <= 500 {
					trafficMatch = downMatch && (upDiff < 2000 && upRatio < 0.20)
				} else if log.monitoredUp > 2000 && log.monitoredDown <= 500 {
					trafficMatch = upMatch && (downDiff < 2000 && downRatio < 0.20)
				} else {
					trafficMatch = upMatch && downMatch
				}

				if trafficMatch {
					candidateMatches[cand.key] = append(candidateMatches[cand.key], log)
					logicalMatches[log.key] = append(logicalMatches[log.key], cand)

					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, log.key.connectionID)
					matchEvidenceMap[pairKey] = map[string]any{
						"overlapMs":         overlapMs,
						"sharedHops":        sharedHops,
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

		for _, cand := range candidates {
			matchedLogicals := candidateMatches[cand.key]

			if len(matchedLogicals) == 0 {
				resultClasses[cand.key] = ClassMissingAttribution
				evidence := map[string]any{
					"decisionReason":  "unpaired_no_matching_logical_connection",
					"candidateChains": cand.chains,
					"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
					"candidateKey":    fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID),
				}
				evJSON, _ := json.Marshal(evidence)
				relationsToInsert = append(relationsToInsert, RelayRelationRecord{
					RunID:                 runID,
					CandidateSessionID:    cand.key.sessionID,
					CandidateEpochID:      cand.key.epochID,
					CandidateConnectionID: cand.key.connectionID,
					Status:                RelayUnpaired,
					EvidenceJSON:          string(evJSON),
					DerivationVersion:     AccountingAlgorithmVersion,
				})
			} else if len(matchedLogicals) == 1 {
				singleLogical := matchedLogicals[0]
				if len(logicalMatches[singleLogical.key]) == 1 {
					resultClasses[cand.key] = ClassConfirmedRelayDuplicate
					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, singleLogical.key.connectionID)
					evidence := matchEvidenceMap[pairKey]
					if evidence == nil {
						evidence = make(map[string]any)
					}
					evidence["decisionReason"] = "strict_1to1_structural_overlap_match"
					evidence["candidateKey"] = fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID)
					evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", singleLogical.key.sessionID, singleLogical.key.epochID, singleLogical.key.connectionID)
					evJSON, _ := json.Marshal(evidence)

					relationsToInsert = append(relationsToInsert, RelayRelationRecord{
						RunID:                 runID,
						CandidateSessionID:    cand.key.sessionID,
						CandidateEpochID:      cand.key.epochID,
						CandidateConnectionID: cand.key.connectionID,
						LogicalSessionID:      singleLogical.key.sessionID,
						LogicalEpochID:        singleLogical.key.epochID,
						LogicalConnectionID:   singleLogical.key.connectionID,
						Status:                RelayConfirmed,
						EvidenceJSON:          string(evJSON),
						DerivationVersion:     AccountingAlgorithmVersion,
					})
				} else {
					resultClasses[cand.key] = ClassAmbiguousRelay
					pairKey := fmt.Sprintf("%s:%s", cand.key.connectionID, singleLogical.key.connectionID)
					evidence := matchEvidenceMap[pairKey]
					if evidence == nil {
						evidence = make(map[string]any)
					}
					evidence["decisionReason"] = "n_to_1_logical_ambiguity"
					evidence["candidateKey"] = fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID)
					evidence["logicalKey"] = fmt.Sprintf("%s:%d:%s", singleLogical.key.sessionID, singleLogical.key.epochID, singleLogical.key.connectionID)
					evJSON, _ := json.Marshal(evidence)

					relationsToInsert = append(relationsToInsert, RelayRelationRecord{
						RunID:                 runID,
						CandidateSessionID:    cand.key.sessionID,
						CandidateEpochID:      cand.key.epochID,
						CandidateConnectionID: cand.key.connectionID,
						LogicalSessionID:      singleLogical.key.sessionID,
						LogicalEpochID:        singleLogical.key.epochID,
						LogicalConnectionID:   singleLogical.key.connectionID,
						Status:                RelayAmbiguous,
						EvidenceJSON:          string(evJSON),
						DerivationVersion:     AccountingAlgorithmVersion,
					})
				}
			} else {
				resultClasses[cand.key] = ClassAmbiguousRelay
				evidence := map[string]any{
					"decisionReason":  "1_to_n_candidate_ambiguity",
					"candidateChains": cand.chains,
					"candidateTotals": map[string]int64{"upload": cand.monitoredUp, "download": cand.monitoredDown},
					"matchedCount":    len(matchedLogicals),
					"candidateKey":    fmt.Sprintf("%s:%d:%s", cand.key.sessionID, cand.key.epochID, cand.key.connectionID),
				}
				evJSON, _ := json.Marshal(evidence)

				relationsToInsert = append(relationsToInsert, RelayRelationRecord{
					RunID:                 runID,
					CandidateSessionID:    cand.key.sessionID,
					CandidateEpochID:      cand.key.epochID,
					CandidateConnectionID: cand.key.connectionID,
					Status:                RelayAmbiguous,
					EvidenceJSON:          string(evJSON),
					DerivationVersion:     AccountingAlgorithmVersion,
				})
			}
		}
	}

	return resultClasses, relationsToInsert, nil
}

func batchWriteRelayRelations(ctx context.Context, db *sql.DB, relations []RelayRelationRecord) error {
	if len(relations) == 0 {
		return nil
	}

	for i := 0; i < len(relations); i += DefaultBatchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + DefaultBatchSize
		if end > len(relations) {
			end = len(relations)
		}
		batch := relations[i:end]

		err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
			stmt, err := tx.PrepareContext(ctx, `
				INSERT INTO relay_relations (
					run_id, candidate_session_id, candidate_epoch_id, candidate_connection_id,
					logical_session_id, logical_epoch_id, logical_connection_id,
					status, evidence_json, derivation_version
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for _, r := range batch {
				var logSess, logConn sql.NullString
				var logEpoch sql.NullInt64
				if r.LogicalConnectionID != "" {
					logSess = sql.NullString{String: r.LogicalSessionID, Valid: true}
					logEpoch = sql.NullInt64{Int64: int64(r.LogicalEpochID), Valid: true}
					logConn = sql.NullString{String: r.LogicalConnectionID, Valid: true}
				}

				if _, err := stmt.ExecContext(ctx,
					r.RunID, r.CandidateSessionID, r.CandidateEpochID, r.CandidateConnectionID,
					logSess, logEpoch, logConn, string(r.Status), r.EvidenceJSON, r.DerivationVersion,
				); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func streamAndBatchWriteAccountedTraffic(ctx context.Context, db *sql.DB, runID string, boundarySeq int64, relayMap map[connKey]AccountingClass) error {
	// 1. 先完全读取到内存切片并释放读锁 (避免与写事务产生内部死锁)
	rows, err := db.QueryContext(ctx, `
		SELECT event_id, session_id, epoch_id, frame_sequence, event_sequence, event_type, observed_at, event_json
		FROM event_journal
		WHERE journal_sequence <= ? AND event_type IN ('ConnectionNew', 'ConnectionDelta')
		ORDER BY frame_sequence ASC, event_sequence ASC;
	`, boundarySeq)
	if err != nil {
		return err
	}

	type rawJournalItem struct {
		eventID, sessID, eventType, obsAtStr, ej string
		epochID                                 int
		frameSeq, eventSeq                      int64
	}
	var rawItems []rawJournalItem

	for rows.Next() {
		var it rawJournalItem
		if err := rows.Scan(&it.eventID, &it.sessID, &it.epochID, &it.frameSeq, &it.eventSeq, &it.eventType, &it.obsAtStr, &it.ej); err != nil {
			rows.Close()
			return err
		}
		rawItems = append(rawItems, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("error reading event_journal: %w", err)
	}
	rows.Close() // 明确关闭读游标以释放共享锁！

	var allRecords []AccountedTrafficRecord

	for _, it := range rawItems {
		var ev types.CollectorEvent
		dec := json.NewDecoder(strings.NewReader(it.ej))
		dec.UseNumber()
		if err := dec.Decode(&ev); err != nil {
			return fmt.Errorf("failed to decode event %s: %w", it.eventID, err)
		}

		k := connKey{sessionID: it.sessID, epochID: it.epochID, connectionID: ev.ConnectionID}
		accClass := relayMap[k]
		if accClass == "" {
			accClass = ClassUnique
		}

		route := ev.Route
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

		var finalProxy, topGroup string
		if route == types.RouteDirect {
			finalProxy = "DIRECT"
		} else if len(ev.Chains) > 0 {
			finalProxy = ev.Chains[0]
			topGroup = ev.Chains[len(ev.Chains)-1]
		}

		obsTime, _ := time.Parse(time.RFC3339Nano, it.obsAtStr)
		obsTime = obsTime.UTC()

		var intS, intE *time.Time
		if len(ev.AttributionInterval) >= 2 {
			t1, err1 := time.Parse(time.RFC3339Nano, ev.AttributionInterval[0])
			t2, err2 := time.Parse(time.RFC3339Nano, ev.AttributionInterval[1])
			if err1 == nil && err2 == nil {
				t1 = t1.UTC()
				t2 = t2.UTC()
				intS = &t1
				intE = &t2
			}
		}

		prec := ev.Precision
		if prec == "" {
			prec = "exact_snapshot"
		}

		allRecords = append(allRecords, AccountedTrafficRecord{
			RunID:                      runID,
			SourceEventID:              it.eventID,
			SessionID:                  it.sessID,
			EpochID:                    it.epochID,
			ConnectionID:               ev.ConnectionID,
			ObservedAt:                 obsTime,
			IntervalStart:              intS,
			IntervalEnd:                intE,
			Precision:                  prec,
			Route:                      route,
			RawUpload:                  ev.DeltaUpload,
			RawDownload:                ev.DeltaDownload,
			AccountedUpload:            accUp,
			AccountedDownload:          accDown,
			AccountingClass:            accClass,
			Process:                    ev.Metadata.Process,
			ProcessPath:                ev.Metadata.ProcessPath,
			Host:                       ev.Metadata.Host,
			SniffHost:                  ev.Metadata.SniffHost,
			DestinationIP:              ev.Metadata.DestinationIP,
			Network:                    ev.Metadata.Network,
			Rule:                       ev.Rule,
			RulePayload:                ev.RulePayload,
			FinalProxy:                 finalProxy,
			TopPolicyGroup:             topGroup,
			DimensionDerivationVersion: DimensionDerivationVersion,
		})
	}

	// 2. 分批写入 accounted_traffic (短事务)
	for i := 0; i < len(allRecords); i += DefaultBatchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + DefaultBatchSize
		if end > len(allRecords) {
			end = len(allRecords)
		}
		batch := allRecords[i:end]

		err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
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

			for _, t := range batch {
				var intStart, intEnd, finalProxy, topGroup sql.NullString
				if t.IntervalStart != nil {
					intStart = sql.NullString{String: t.IntervalStart.UTC().Format(time.RFC3339Nano), Valid: true}
				}
				if t.IntervalEnd != nil {
					intEnd = sql.NullString{String: t.IntervalEnd.UTC().Format(time.RFC3339Nano), Valid: true}
				}
				if t.FinalProxy != "" {
					finalProxy = sql.NullString{String: t.FinalProxy, Valid: true}
				}
				if t.TopPolicyGroup != "" {
					topGroup = sql.NullString{String: t.TopPolicyGroup, Valid: true}
				}

				if _, err := stmt.ExecContext(ctx,
					t.RunID, t.SourceEventID, t.SessionID, t.EpochID, t.ConnectionID, t.ObservedAt.UTC().Format(time.RFC3339Nano),
					intStart, intEnd, t.Precision, string(t.Route),
					t.RawUpload, t.RawDownload, t.AccountedUpload, t.AccountedDownload,
					string(t.AccountingClass), t.Process, t.ProcessPath, t.Host, t.SniffHost, t.DestinationIP, t.Network,
					t.Rule, t.RulePayload, finalProxy, topGroup, t.DimensionDerivationVersion,
				); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func rebuildHourlyAggregatesBatched(ctx context.Context, db *sql.DB, runID string) error {
	// 1. 完全读取并计算聚合，然后立即释放读游标
	rows, err := db.QueryContext(ctx, `
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
			rows.Close()
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
				allocUp := NewIntervalAllocator(startTime, endTime, accUp)
				allocDown := NewIntervalAllocator(startTime, endTime, accDown)

				curr := startTime
				for curr.Before(endTime) {
					bucket := curr.Truncate(time.Hour)
					nextBucket := bucket.Add(time.Hour)
					bucketEnd := nextBucket
					if bucketEnd.After(endTime) {
						bucketEnd = endTime
					}

					pUp := allocUp.Allocate(curr, bucketEnd)
					pDown := allocDown.Allocate(curr, bucketEnd)

					allocations = append(allocations, timeAllocation{
						bucketStart: bucket,
						upBytes:     pUp,
						downBytes:   pDown,
						isExact:     false,
					})
					curr = bucketEnd
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("error reading accounted_traffic for aggregates: %w", err)
	}
	rows.Close() // 明确关闭读游标！

	var aggList []UsageHourlyDimensionRecord
	for k, v := range aggregates {
		aggList = append(aggList, UsageHourlyDimensionRecord{
			RunID:                  runID,
			BucketStart:            k.bucketStart,
			DimensionType:          k.dimType,
			DimensionKey:           k.dimKey,
			Route:                  k.route,
			UploadBytes:            v.upload,
			DownloadBytes:          v.download,
			ConnectionCount:        int64(len(v.distinctConns)),
			ExactUploadBytes:       v.exactUp,
			ExactDownloadBytes:     v.exactDown,
			EstimatedUploadBytes:   v.estUp,
			EstimatedDownloadBytes: v.estDown,
		})
	}

	// 2. 分批短事务写入 usage_hourly_dimensions
	for i := 0; i < len(aggList); i += DefaultBatchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + DefaultBatchSize
		if end > len(aggList) {
			end = len(aggList)
		}
		batch := aggList[i:end]

		err := execWithTxRetry(ctx, db, 10, func(tx *sql.Tx) error {
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

			for _, a := range batch {
				bucketStr := a.BucketStart.UTC().Format(time.RFC3339Nano)
				if _, err := stmt.ExecContext(ctx,
					a.RunID, bucketStr, a.DimensionType, a.DimensionKey, string(a.Route),
					a.UploadBytes, a.DownloadBytes, a.ConnectionCount,
					a.ExactUploadBytes, a.ExactDownloadBytes, a.EstimatedUploadBytes, a.EstimatedDownloadBytes,
				); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}
