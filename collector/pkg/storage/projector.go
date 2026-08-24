package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

// ApplyEventProjection 将单个 CollectorEvent 投影到对应的维度与时序表中
func ApplyEventProjection(ctx context.Context, tx *sql.Tx, ev *types.CollectorEvent) error {
	obsAtStr := ev.Timestamp.UTC().Format(time.RFC3339Nano)
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)

	switch ev.Type {
	case types.EventConnectionBootstrap:
		qualityJSON, _ := json.Marshal(ev.QualityFlags)
		specialRulesJSON, _ := json.Marshal(ev.Metadata.SpecialRules)
		chainsJSON, _ := json.Marshal(ev.Chains)
		providerChainsJSON, _ := json.Marshal(ev.ProviderChains)
		relayEvJSON, _ := json.Marshal(ev.Details)

		var startClass sql.NullString
		if ev.Details != nil {
			if sc, ok := ev.Details["startClassification"].(string); ok {
				startClass = sql.NullString{String: sc, Valid: true}
			}
		}

		insertSQL := `
		INSERT INTO connections (
			session_id, epoch_id, connection_id, mihomo_start, first_observed_at, last_observed_at,
			state, preexisting_at_start, possible_unobserved_tail, start_classification,
			process, process_path, host, sniff_host, network, type, source_ip, source_port,
			destination_ip, remote_destination, destination_port, dns_mode, special_proxy,
			special_rules_json, inbound_user, inbound_name, inbound_port, rule, rule_payload,
			chains_json, provider_chains_json, route, latest_attribution_class, quality_flags_json,
			relay_evidence_json, baseline_upload_counter, baseline_download_counter,
			last_observed_upload_counter, last_observed_download_counter,
			monitored_upload_total, monitored_download_total, created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			'active', ?, 0, ?,
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?,
			?, ?,
			0, 0, ?, ?
		)
		ON CONFLICT(session_id, epoch_id, connection_id) DO UPDATE SET
			last_observed_at = excluded.last_observed_at,
			updated_at = excluded.updated_at;
		`
		if _, err := tx.ExecContext(ctx, insertSQL,
			ev.SessionID, ev.EpochID, ev.ConnectionID, ev.MihomoStart, obsAtStr, obsAtStr,
			ev.PreexistingAtStart, startClass,
			ev.Metadata.Process, ev.Metadata.ProcessPath, ev.Metadata.Host, ev.Metadata.SniffHost,
			ev.Metadata.Network, ev.Metadata.Type, ev.Metadata.SourceIP, ev.Metadata.SourcePort,
			ev.Metadata.DestinationIP, ev.Metadata.RemoteDestination, ev.Metadata.DestinationPort,
			ev.Metadata.DnsMode, ev.Metadata.SpecialProxy, string(specialRulesJSON),
			ev.Metadata.InboundUser, ev.Metadata.InboundName, ev.Metadata.InboundPort,
			ev.Rule, ev.RulePayload, string(chainsJSON), string(providerChainsJSON),
			string(ev.Route), string(ev.AttributionClass), string(qualityJSON), string(relayEvJSON),
			ev.BaselineUploadCounter, ev.BaselineDownloadCounter,
			ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			nowStr, nowStr,
		); err != nil {
			return fmt.Errorf("failed to project ConnectionBootstrap: %w", err)
		}

	case types.EventConnectionNew:
		qualityJSON, _ := json.Marshal(ev.QualityFlags)
		specialRulesJSON, _ := json.Marshal(ev.Metadata.SpecialRules)
		chainsJSON, _ := json.Marshal(ev.Chains)
		providerChainsJSON, _ := json.Marshal(ev.ProviderChains)
		relayEvJSON, _ := json.Marshal(ev.Details)

		insertSQL := `
		INSERT INTO connections (
			session_id, epoch_id, connection_id, mihomo_start, first_observed_at, last_observed_at,
			state, preexisting_at_start, possible_unobserved_tail,
			process, process_path, host, sniff_host, network, type, source_ip, source_port,
			destination_ip, remote_destination, destination_port, dns_mode, special_proxy,
			special_rules_json, inbound_user, inbound_name, inbound_port, rule, rule_payload,
			chains_json, provider_chains_json, route, latest_attribution_class, quality_flags_json,
			relay_evidence_json, baseline_upload_counter, baseline_download_counter,
			last_observed_upload_counter, last_observed_download_counter,
			monitored_upload_total, monitored_download_total, created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?,
			'active', 0, 0,
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, 0, 0,
			?, ?,
			?, ?, ?, ?
		);
		`
		if _, err := tx.ExecContext(ctx, insertSQL,
			ev.SessionID, ev.EpochID, ev.ConnectionID, ev.MihomoStart, obsAtStr, obsAtStr,
			ev.Metadata.Process, ev.Metadata.ProcessPath, ev.Metadata.Host, ev.Metadata.SniffHost,
			ev.Metadata.Network, ev.Metadata.Type, ev.Metadata.SourceIP, ev.Metadata.SourcePort,
			ev.Metadata.DestinationIP, ev.Metadata.RemoteDestination, ev.Metadata.DestinationPort,
			ev.Metadata.DnsMode, ev.Metadata.SpecialProxy, string(specialRulesJSON),
			ev.Metadata.InboundUser, ev.Metadata.InboundName, ev.Metadata.InboundPort,
			ev.Rule, ev.RulePayload, string(chainsJSON), string(providerChainsJSON),
			string(ev.Route), string(ev.AttributionClass), string(qualityJSON), string(relayEvJSON),
			ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload,
			nowStr, nowStr,
		); err != nil {
			return fmt.Errorf("failed to project ConnectionNew: %w", err)
		}

		// 插入 connection_traffic
		precision := ev.Precision
		if precision == "" {
			precision = "exact_snapshot"
		}
		trafficSQL := `
		INSERT INTO connection_traffic (
			event_id, session_id, epoch_id, connection_id, observed_at, precision,
			delta_upload, delta_download, observed_upload_counter, observed_download_counter,
			monitored_upload_total, monitored_download_total, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`
		if _, err := tx.ExecContext(ctx, trafficSQL,
			ev.EventID, ev.SessionID, ev.EpochID, ev.ConnectionID, obsAtStr, precision,
			ev.DeltaUpload, ev.DeltaDownload, ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload, nowStr,
		); err != nil {
			return fmt.Errorf("failed to project ConnectionNew traffic: %w", err)
		}

	case types.EventConnectionDelta:
		precision := ev.Precision
		if precision == "" {
			precision = "exact_snapshot"
		}
		var intervalStart, intervalEnd sql.NullString
		if len(ev.AttributionInterval) >= 2 {
			intervalStart = sql.NullString{String: ev.AttributionInterval[0], Valid: true}
			intervalEnd = sql.NullString{String: ev.AttributionInterval[1], Valid: true}
		}

		// 插入 connection_traffic
		trafficSQL := `
		INSERT INTO connection_traffic (
			event_id, session_id, epoch_id, connection_id, observed_at, interval_start, interval_end, precision,
			delta_upload, delta_download, observed_upload_counter, observed_download_counter,
			monitored_upload_total, monitored_download_total, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`
		if _, err := tx.ExecContext(ctx, trafficSQL,
			ev.EventID, ev.SessionID, ev.EpochID, ev.ConnectionID, obsAtStr, intervalStart, intervalEnd, precision,
			ev.DeltaUpload, ev.DeltaDownload, ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload, nowStr,
		); err != nil {
			return fmt.Errorf("failed to project ConnectionDelta traffic: %w", err)
		}

		// 更新 connections 维度表
		updateSQL := `
		UPDATE connections SET
			last_observed_at = ?,
			last_observed_upload_counter = ?,
			last_observed_download_counter = ?,
			monitored_upload_total = ?,
			monitored_download_total = ?,
			updated_at = ?
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSQL,
			obsAtStr, ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload, nowStr,
			ev.SessionID, ev.EpochID, ev.ConnectionID,
		); err != nil {
			return fmt.Errorf("failed to update connection from Delta: %w", err)
		}

	case types.EventConnectionMetadataUpdated:
		qualityJSON, _ := json.Marshal(ev.QualityFlags)
		specialRulesJSON, _ := json.Marshal(ev.Metadata.SpecialRules)
		chainsJSON, _ := json.Marshal(ev.Chains)
		providerChainsJSON, _ := json.Marshal(ev.ProviderChains)

		updateSQL := `
		UPDATE connections SET
			process = ?, process_path = ?, host = ?, sniff_host = ?,
			network = ?, type = ?, source_ip = ?, source_port = ?,
			destination_ip = ?, remote_destination = ?, destination_port = ?,
			dns_mode = ?, special_proxy = ?, special_rules_json = ?,
			inbound_user = ?, inbound_name = ?, inbound_port = ?,
			rule = ?, rule_payload = ?, chains_json = ?, provider_chains_json = ?,
			route = ?, quality_flags_json = ?, updated_at = ?
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSQL,
			ev.Metadata.Process, ev.Metadata.ProcessPath, ev.Metadata.Host, ev.Metadata.SniffHost,
			ev.Metadata.Network, ev.Metadata.Type, ev.Metadata.SourceIP, ev.Metadata.SourcePort,
			ev.Metadata.DestinationIP, ev.Metadata.RemoteDestination, ev.Metadata.DestinationPort,
			ev.Metadata.DnsMode, ev.Metadata.SpecialProxy, string(specialRulesJSON),
			ev.Metadata.InboundUser, ev.Metadata.InboundName, ev.Metadata.InboundPort,
			ev.Rule, ev.RulePayload, string(chainsJSON), string(providerChainsJSON),
			string(ev.Route), string(qualityJSON), nowStr,
			ev.SessionID, ev.EpochID, ev.ConnectionID,
		); err != nil {
			return fmt.Errorf("failed to project MetadataUpdated: %w", err)
		}

	case types.EventConnectionDisappeared:
		updateSQL := `
		UPDATE connections SET
			state = 'disappeared_from_snapshot',
			disappeared_observed_at = ?,
			possible_unobserved_tail = 1,
			updated_at = ?
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSQL, obsAtStr, nowStr, ev.SessionID, ev.EpochID, ev.ConnectionID); err != nil {
			return fmt.Errorf("failed to project Disappeared: %w", err)
		}

	case types.EventRelayClassificationChanged:
		relayEvJSON, _ := json.Marshal(ev.Details)
		updateSQL := `
		UPDATE connections SET
			latest_attribution_class = ?,
			relay_evidence_json = ?,
			updated_at = ?
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSQL, string(ev.AttributionClass), string(relayEvJSON), nowStr, ev.SessionID, ev.EpochID, ev.ConnectionID); err != nil {
			return fmt.Errorf("failed to project RelayClassificationChanged: %w", err)
		}

	case types.EventMonitoringGapOpened:
		gapID := fmt.Sprintf("gap-%s-%s", ev.SessionID, ev.EventID)
		startStr := obsAtStr
		if len(ev.AttributionInterval) > 0 && ev.AttributionInterval[0] != "" {
			startStr = ev.AttributionInterval[0]
		}
		reason := "controller_disconnected_or_watchdog_stalled"
		if ev.Details != nil {
			if r, ok := ev.Details["reason"].(string); ok {
				reason = r
			}
		}

		insertSQL := `
		INSERT INTO monitoring_gaps (
			gap_id, source, session_id, open_event_id, started_at, reason, precision, created_at
		) VALUES (?, 'controller_stream', ?, ?, ?, ?, 'interval', ?);
		`
		if _, err := tx.ExecContext(ctx, insertSQL, gapID, ev.SessionID, ev.EventID, startStr, reason, nowStr); err != nil {
			return fmt.Errorf("failed to project MonitoringGapOpened: %w", err)
		}

	case types.EventMonitoringGapClosed:
		// 寻找当前未关闭的 controller_stream gap
		var gapID string
		var startedAtStr string
		err := tx.QueryRowContext(ctx, `
			SELECT gap_id, started_at FROM monitoring_gaps
			WHERE session_id = ? AND source = 'controller_stream' AND ended_at IS NULL
			ORDER BY started_at DESC LIMIT 1
		`, ev.SessionID).Scan(&gapID, &startedAtStr)

		if err != nil {
			if err == sql.ErrNoRows {
				// 若未找到 open gap 则生成一条闭合 gap
				gapID = fmt.Sprintf("gap-closed-%s-%s", ev.SessionID, ev.EventID)
				startedAtStr = obsAtStr
				if len(ev.AttributionInterval) > 0 && ev.AttributionInterval[0] != "" {
					startedAtStr = ev.AttributionInterval[0]
				}
				insertSQL := `
				INSERT INTO monitoring_gaps (
					gap_id, source, session_id, close_event_id, started_at, ended_at, duration_ms,
					reason, global_gap_upload_delta, global_gap_download_delta, physical_delta_unavailable, precision, created_at
				) VALUES (?, 'controller_stream', ?, ?, ?, ?, ?, 'reconnected', ?, ?, ?, 'interval', ?);
				`
				var durationMs int64
				var upDelta, downDelta *int64
				var physUnavail bool
				if ev.Details != nil {
					if d, ok := ev.Details["actualGapMs"].(int64); ok {
						durationMs = d
					} else if d, ok := ev.Details["actualGapMs"].(float64); ok {
						durationMs = int64(d)
					}
					if u, ok := ev.Details["globalGapUploadDelta"].(int64); ok {
						upDelta = &u
					}
					if d, ok := ev.Details["globalGapDownloadDelta"].(int64); ok {
						downDelta = &d
					}
					if p, ok := ev.Details["gapPhysicalDeltaUnavailable"].(bool); ok {
						physUnavail = p
					}
				}
				_, _ = tx.ExecContext(ctx, insertSQL, gapID, ev.SessionID, ev.EventID, startedAtStr, obsAtStr, durationMs, upDelta, downDelta, physUnavail, nowStr)
				return nil
			}
			return fmt.Errorf("failed to find open monitoring gap: %w", err)
		}

		var durationMs int64
		var upDelta, downDelta *int64
		var physUnavail bool
		if ev.Details != nil {
			if d, ok := ev.Details["actualGapMs"].(int64); ok {
				durationMs = d
			} else if d, ok := ev.Details["actualGapMs"].(float64); ok {
				durationMs = int64(d)
			}
			if u, ok := ev.Details["globalGapUploadDelta"].(int64); ok {
				upDelta = &u
			}
			if d, ok := ev.Details["globalGapDownloadDelta"].(int64); ok {
				downDelta = &d
			}
			if p, ok := ev.Details["gapPhysicalDeltaUnavailable"].(bool); ok {
				physUnavail = p
			}
		}

		updateSQL := `
		UPDATE monitoring_gaps SET
			close_event_id = ?,
			ended_at = ?,
			duration_ms = ?,
			global_gap_upload_delta = ?,
			global_gap_download_delta = ?,
			physical_delta_unavailable = ?
		WHERE gap_id = ?;
		`
		if _, err := tx.ExecContext(ctx, updateSQL, ev.EventID, obsAtStr, durationMs, upDelta, downDelta, physUnavail, gapID); err != nil {
			return fmt.Errorf("failed to project MonitoringGapClosed: %w", err)
		}

	case types.EventSamplingResidual:
		var upDelta, downDelta, uniqueUp, uniqueDown, resUp, resDown int64
		if ev.Details != nil {
			if v, ok := ev.Details["globalUploadDelta"].(int64); ok { upDelta = v }
			if v, ok := ev.Details["globalDownloadDelta"].(int64); ok { downDelta = v }
			if v, ok := ev.Details["uniqueObservedUpload"].(int64); ok { uniqueUp = v }
			if v, ok := ev.Details["uniqueObservedDownload"].(int64); ok { uniqueDown = v }
			if v, ok := ev.Details["residualUpload"].(int64); ok { resUp = v }
			if v, ok := ev.Details["residualDownload"].(int64); ok { resDown = v }
		}

		insertSQL := `
		INSERT INTO residual_intervals (
			event_id, session_id, epoch_id, observed_at,
			global_upload_delta, global_download_delta,
			unique_observed_upload, unique_observed_download,
			residual_upload, residual_download,
			derivation_version, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'phase1_diagnostic_v1', ?);
		`
		if _, err := tx.ExecContext(ctx, insertSQL,
			ev.EventID, ev.SessionID, ev.EpochID, obsAtStr,
			upDelta, downDelta, uniqueUp, uniqueDown, resUp, resDown, nowStr,
		); err != nil {
			return fmt.Errorf("failed to project SamplingResidual: %w", err)
		}

	case types.EventCollectorHealth:
		issue := "unknown"
		if ev.Details != nil {
			if is, ok := ev.Details["issue"].(string); ok {
				issue = is
			}
		}
		detailsJSON, _ := json.Marshal(ev.Details)

		insertSQL := `
		INSERT INTO collector_health (
			event_id, session_id, epoch_id, observed_at, connection_id, issue, details_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
		`
		connID := sql.NullString{String: ev.ConnectionID, Valid: ev.ConnectionID != ""}
		if _, err := tx.ExecContext(ctx, insertSQL,
			ev.EventID, ev.SessionID, ev.EpochID, obsAtStr, connID, issue, string(detailsJSON), nowStr,
		); err != nil {
			return fmt.Errorf("failed to project CollectorHealth: %w", err)
		}
	}

	return nil
}
