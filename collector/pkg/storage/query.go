package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// QueryService 提供面向 UI 与只读审计客户端的轻量查询层
type QueryService struct {
	db *sql.DB
}

// NewQueryService 创建 QueryService 实例
func NewQueryService(db *sql.DB) *QueryService {
	return &QueryService{db: db}
}

// GetConnection 查询指定会话、纪元与 ID 的连接明细
func (q *QueryService) GetConnection(ctx context.Context, sessionID string, epochID int, connectionID string) (*ConnectionRecord, error) {
	row := q.db.QueryRowContext(ctx, `
		SELECT
			session_id, epoch_id, connection_id, mihomo_start, first_observed_at, last_observed_at,
			disappeared_observed_at, state, preexisting_at_start, possible_unobserved_tail, start_classification,
			process, process_path, host, sniff_host, network, type, source_ip, source_port,
			destination_ip, remote_destination, destination_port, dns_mode, special_proxy,
			special_rules_json, inbound_user, inbound_name, inbound_port, rule, rule_payload,
			chains_json, provider_chains_json, route, latest_attribution_class, quality_flags_json,
			relay_evidence_json, baseline_upload_counter, baseline_download_counter,
			last_observed_upload_counter, last_observed_download_counter,
			monitored_upload_total, monitored_download_total
		FROM connections
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?;
	`, sessionID, epochID, connectionID)

	var rec ConnectionRecord
	var mihomoStart, startClass, specialRulesJSON, chainsJSON, providerChainsJSON, qualityJSON, relayEvJSON sql.NullString
	var firstObsStr, lastObsStr string
	var disObsStr sql.NullString

	err := row.Scan(
		&rec.SessionID, &rec.EpochID, &rec.ConnectionID, &mihomoStart, &firstObsStr, &lastObsStr,
		&disObsStr, &rec.State, &rec.PreexistingAtStart, &rec.PossibleUnobservedTail, &startClass,
		&rec.Metadata.Process, &rec.Metadata.ProcessPath, &rec.Metadata.Host, &rec.Metadata.SniffHost,
		&rec.Metadata.Network, &rec.Metadata.Type, &rec.Metadata.SourceIP, &rec.Metadata.SourcePort,
		&rec.Metadata.DestinationIP, &rec.Metadata.RemoteDestination, &rec.Metadata.DestinationPort,
		&rec.Metadata.DnsMode, &rec.Metadata.SpecialProxy, &specialRulesJSON,
		&rec.Metadata.InboundUser, &rec.Metadata.InboundName, &rec.Metadata.InboundPort,
		&rec.Rule, &rec.RulePayload, &chainsJSON, &providerChainsJSON,
		&rec.Route, &rec.LatestAttributionClass, &qualityJSON, &relayEvJSON,
		&rec.BaselineUploadCounter, &rec.BaselineDownloadCounter,
		&rec.LastObservedUploadCounter, &rec.LastObservedDownloadCounter,
		&rec.MonitoredUploadTotal, &rec.MonitoredDownloadTotal,
	)
	if err != nil {
		return nil, err
	}

	rec.MihomoStart = mihomoStart.String
	rec.StartClassification = startClass.String
	rec.FirstObservedAt, _ = time.Parse(time.RFC3339Nano, firstObsStr)
	rec.LastObservedAt, _ = time.Parse(time.RFC3339Nano, lastObsStr)
	if disObsStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, disObsStr.String)
		rec.DisappearedObservedAt = &t
	}

	rec.Metadata.SpecialRules = specialRulesJSON.String
	if chainsJSON.Valid && chainsJSON.String != "" {
		_ = json.Unmarshal([]byte(chainsJSON.String), &rec.Chains)
	}
	if providerChainsJSON.Valid && providerChainsJSON.String != "" {
		_ = json.Unmarshal([]byte(providerChainsJSON.String), &rec.ProviderChains)
	}
	if qualityJSON.Valid && qualityJSON.String != "" {
		_ = json.Unmarshal([]byte(qualityJSON.String), &rec.QualityFlags)
	}
	if relayEvJSON.Valid && relayEvJSON.String != "" {
		_ = json.Unmarshal([]byte(relayEvJSON.String), &rec.RelayEvidence)
	}

	return &rec, nil
}

// ListConnections 支持多条件过滤查询连接列表（支持时间重叠窗口匹配）
func (q *QueryService) ListConnections(ctx context.Context, filter ConnectionFilter) ([]*ConnectionRecord, error) {
	var whereClauses []string
	var args []any

	if filter.StartTime != nil {
		whereClauses = append(whereClauses, "last_observed_at >= ?")
		args = append(args, filter.StartTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.EndTime != nil {
		whereClauses = append(whereClauses, "first_observed_at <= ?")
		args = append(args, filter.EndTime.UTC().Format(time.RFC3339Nano))
	}
	if filter.Route != "" {
		whereClauses = append(whereClauses, "route = ?")
		args = append(args, string(filter.Route))
	}
	if filter.Process != "" {
		whereClauses = append(whereClauses, "process LIKE ?")
		args = append(args, "%"+filter.Process+"%")
	}
	if filter.Host != "" {
		whereClauses = append(whereClauses, "(host LIKE ? OR sniff_host LIKE ?)")
		args = append(args, "%"+filter.Host+"%", "%"+filter.Host+"%")
	}
	if filter.DestinationIP != "" {
		whereClauses = append(whereClauses, "destination_ip = ?")
		args = append(args, filter.DestinationIP)
	}
	if filter.Network != "" {
		whereClauses = append(whereClauses, "network = ?")
		args = append(args, filter.Network)
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	limitSQL := ""
	if filter.Limit > 0 {
		limitSQL = fmt.Sprintf("LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			limitSQL += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	querySQL := fmt.Sprintf(`
		SELECT
			session_id, epoch_id, connection_id, mihomo_start, first_observed_at, last_observed_at,
			disappeared_observed_at, state, preexisting_at_start, possible_unobserved_tail, start_classification,
			process, process_path, host, sniff_host, network, type, source_ip, source_port,
			destination_ip, remote_destination, destination_port, dns_mode, special_proxy,
			special_rules_json, inbound_user, inbound_name, inbound_port, rule, rule_payload,
			chains_json, provider_chains_json, route, latest_attribution_class, quality_flags_json,
			relay_evidence_json, baseline_upload_counter, baseline_download_counter,
			last_observed_upload_counter, last_observed_download_counter,
			monitored_upload_total, monitored_download_total
		FROM connections
		%s
		ORDER BY first_observed_at DESC
		%s;
	`, whereSQL, limitSQL)

	rows, err := q.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query connections: %w", err)
	}
	defer rows.Close()

	var records []*ConnectionRecord
	for rows.Next() {
		var rec ConnectionRecord
		var mihomoStart, startClass, specialRulesJSON, chainsJSON, providerChainsJSON, qualityJSON, relayEvJSON sql.NullString
		var firstObsStr, lastObsStr string
		var disObsStr sql.NullString

		if err := rows.Scan(
			&rec.SessionID, &rec.EpochID, &rec.ConnectionID, &mihomoStart, &firstObsStr, &lastObsStr,
			&disObsStr, &rec.State, &rec.PreexistingAtStart, &rec.PossibleUnobservedTail, &startClass,
			&rec.Metadata.Process, &rec.Metadata.ProcessPath, &rec.Metadata.Host, &rec.Metadata.SniffHost,
			&rec.Metadata.Network, &rec.Metadata.Type, &rec.Metadata.SourceIP, &rec.Metadata.SourcePort,
			&rec.Metadata.DestinationIP, &rec.Metadata.RemoteDestination, &rec.Metadata.DestinationPort,
			&rec.Metadata.DnsMode, &rec.Metadata.SpecialProxy, &specialRulesJSON,
			&rec.Metadata.InboundUser, &rec.Metadata.InboundName, &rec.Metadata.InboundPort,
			&rec.Rule, &rec.RulePayload, &chainsJSON, &providerChainsJSON,
			&rec.Route, &rec.LatestAttributionClass, &qualityJSON, &relayEvJSON,
			&rec.BaselineUploadCounter, &rec.BaselineDownloadCounter,
			&rec.LastObservedUploadCounter, &rec.LastObservedDownloadCounter,
			&rec.MonitoredUploadTotal, &rec.MonitoredDownloadTotal,
		); err != nil {
			return nil, err
		}

		rec.MihomoStart = mihomoStart.String
		rec.StartClassification = startClass.String
		rec.FirstObservedAt, _ = time.Parse(time.RFC3339Nano, firstObsStr)
		rec.LastObservedAt, _ = time.Parse(time.RFC3339Nano, lastObsStr)
		if disObsStr.Valid {
			t, _ := time.Parse(time.RFC3339Nano, disObsStr.String)
			rec.DisappearedObservedAt = &t
		}

		rec.Metadata.SpecialRules = specialRulesJSON.String
		if chainsJSON.Valid && chainsJSON.String != "" {
			_ = json.Unmarshal([]byte(chainsJSON.String), &rec.Chains)
		}
		if providerChainsJSON.Valid && providerChainsJSON.String != "" {
			_ = json.Unmarshal([]byte(providerChainsJSON.String), &rec.ProviderChains)
		}
		if qualityJSON.Valid && qualityJSON.String != "" {
			_ = json.Unmarshal([]byte(qualityJSON.String), &rec.QualityFlags)
		}
		if relayEvJSON.Valid && relayEvJSON.String != "" {
			_ = json.Unmarshal([]byte(relayEvJSON.String), &rec.RelayEvidence)
		}

		records = append(records, &rec)
	}

	return records, nil
}

// ListConnectionTraffic 按权威时间序查询单个连接的流量增量时间轴
func (q *QueryService) ListConnectionTraffic(ctx context.Context, sessionID string, epochID int, connectionID string) ([]*ConnectionTrafficRecord, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT
			event_id, session_id, epoch_id, connection_id, observed_at,
			interval_start, interval_end, precision,
			delta_upload, delta_download, observed_upload_counter, observed_download_counter,
			monitored_upload_total, monitored_download_total
		FROM connection_traffic
		WHERE session_id = ? AND epoch_id = ? AND connection_id = ?
		ORDER BY observed_at ASC;
	`, sessionID, epochID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query connection traffic: %w", err)
	}
	defer rows.Close()

	var list []*ConnectionTrafficRecord
	for rows.Next() {
		var tr ConnectionTrafficRecord
		var obsAtStr string
		var intStart, intEnd sql.NullString

		if err := rows.Scan(
			&tr.EventID, &tr.SessionID, &tr.EpochID, &tr.ConnectionID, &obsAtStr,
			&intStart, &intEnd, &tr.Precision,
			&tr.DeltaUpload, &tr.DeltaDownload, &tr.ObservedUploadCounter, &tr.ObservedDownloadCounter,
			&tr.MonitoredUploadTotal, &tr.MonitoredDownloadTotal,
		); err != nil {
			return nil, err
		}

		tr.ObservedAt, _ = time.Parse(time.RFC3339Nano, obsAtStr)
		if intStart.Valid {
			t, _ := time.Parse(time.RFC3339Nano, intStart.String)
			tr.IntervalStart = &t
		}
		if intEnd.Valid {
			t, _ := time.Parse(time.RFC3339Nano, intEnd.String)
			tr.IntervalEnd = &t
		}
		list = append(list, &tr)
	}

	return list, nil
}

// ListMonitoringGaps 查询指定时间范围内的监控缺口（包含 controller_stream 与 collector_session_boundary）
func (q *QueryService) ListMonitoringGaps(ctx context.Context, startTime, endTime *time.Time) ([]*MonitoringGapRecord, error) {
	var whereClauses []string
	var args []any

	if startTime != nil {
		whereClauses = append(whereClauses, "(ended_at >= ? OR ended_at IS NULL)")
		args = append(args, startTime.UTC().Format(time.RFC3339Nano))
	}
	if endTime != nil {
		whereClauses = append(whereClauses, "started_at <= ?")
		args = append(args, endTime.UTC().Format(time.RFC3339Nano))
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	querySQL := fmt.Sprintf(`
		SELECT
			gap_id, source, session_id, open_event_id, close_event_id,
			started_at, ended_at, duration_ms, reason,
			global_gap_upload_delta, global_gap_download_delta, physical_delta_unavailable,
			precision, created_at
		FROM monitoring_gaps
		%s
		ORDER BY started_at DESC;
	`, whereSQL)

	rows, err := q.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query monitoring gaps: %w", err)
	}
	defer rows.Close()

	var gaps []*MonitoringGapRecord
	for rows.Next() {
		var g MonitoringGapRecord
		var sessID, openEvID, closeEvID, endedAtStr sql.NullString
		var startedAtStr, createdAtStr string
		var durationMs, upDelta, downDelta sql.NullInt64

		if err := rows.Scan(
			&g.GapID, &g.Source, &sessID, &openEvID, &closeEvID,
			&startedAtStr, &endedAtStr, &durationMs, &g.Reason,
			&upDelta, &downDelta, &g.PhysicalDeltaUnavailable,
			&g.Precision, &createdAtStr,
		); err != nil {
			return nil, err
		}

		g.SessionID = sessID.String
		g.OpenEventID = openEvID.String
		g.CloseEventID = closeEvID.String
		g.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAtStr)
		g.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)

		if endedAtStr.Valid {
			t, _ := time.Parse(time.RFC3339Nano, endedAtStr.String)
			g.EndedAt = &t
		}
		if durationMs.Valid {
			g.DurationMs = &durationMs.Int64
		}
		if upDelta.Valid {
			g.GlobalGapUploadDelta = &upDelta.Int64
		}
		if downDelta.Valid {
			g.GlobalGapDownloadDelta = &downDelta.Int64
		}

		gaps = append(gaps, &g)
	}

	return gaps, nil
}

// ListDiagnosticResiduals 查询初阶残差视图
func (q *QueryService) ListDiagnosticResiduals(ctx context.Context, startTime, endTime *time.Time) ([]*ResidualRecord, error) {
	var whereClauses []string
	var args []any

	if startTime != nil {
		whereClauses = append(whereClauses, "observed_at >= ?")
		args = append(args, startTime.UTC().Format(time.RFC3339Nano))
	}
	if endTime != nil {
		whereClauses = append(whereClauses, "observed_at <= ?")
		args = append(args, endTime.UTC().Format(time.RFC3339Nano))
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	querySQL := fmt.Sprintf(`
		SELECT
			event_id, session_id, epoch_id, observed_at,
			global_upload_delta, global_download_delta,
			unique_observed_upload, unique_observed_download,
			residual_upload, residual_download,
			derivation_version, created_at
		FROM residual_intervals
		%s
		ORDER BY observed_at DESC;
	`, whereSQL)

	rows, err := q.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query residuals: %w", err)
	}
	defer rows.Close()

	var residuals []*ResidualRecord
	for rows.Next() {
		var r ResidualRecord
		var obsAtStr, createdAtStr string

		if err := rows.Scan(
			&r.EventID, &r.SessionID, &r.EpochID, &obsAtStr,
			&r.GlobalUploadDelta, &r.GlobalDownloadDelta,
			&r.UniqueObservedUpload, &r.UniqueObservedDownload,
			&r.ResidualUpload, &r.ResidualDownload,
			&r.DerivationVersion, &createdAtStr,
		); err != nil {
			return nil, err
		}

		r.ObservedAt, _ = time.Parse(time.RFC3339Nano, obsAtStr)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		residuals = append(residuals, &r)
	}

	return residuals, nil
}
