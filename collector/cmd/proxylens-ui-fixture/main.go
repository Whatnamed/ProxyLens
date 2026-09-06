package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func main() {
	profile := flag.String("profile", "healthy", "Synthetic profile to generate: healthy | gaps | stale | empty | scaled | review")
	outPath := flag.String("out", "", "Output SQLite database path (e.g. ./fixtures/fixture_healthy.db)")
	anchorStr := flag.String("anchor", "", "Anchor time in RFC3339 (optional, defaults to current UTC time)")
	scaleCount := flag.Int("scale", 100000, "Event count for scaled profile (default: 100000)")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Join(".", fmt.Sprintf("fixture_%s.db", *profile))
	}

	var anchorTime time.Time
	if *anchorStr != "" {
		t, err := time.Parse(time.RFC3339Nano, *anchorStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, *anchorStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[FATAL] Invalid --anchor timestamp '%s': %v\n", *anchorStr, err)
				os.Exit(1)
			}
		}
		anchorTime = t.UTC()
	} else {
		anchorTime = time.Now().UTC()
	}

	// 确保父目录存在
	if dir := filepath.Dir(*outPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "[FATAL] Failed to create directory '%s': %v\n", dir, err)
			os.Exit(1)
		}
	}

	// 清理旧文件
	_ = os.Remove(*outPath)
	_ = os.Remove(*outPath + "-wal")
	_ = os.Remove(*outPath + "-shm")

	ctx := context.Background()
	absPath, _ := filepath.Abs(*outPath)
	fmt.Printf("[UI Fixture Generator] Generating profile '%s' (Anchor: %s) at: %s\n", *profile, anchorTime.Format(time.RFC3339), absPath)

	switch *profile {
	case "empty":
		generateEmpty(ctx, absPath)
	case "healthy":
		generateHealthy(ctx, absPath, anchorTime)
	case "gaps":
		generateGaps(ctx, absPath, anchorTime)
	case "stale":
		generateStale(ctx, absPath, anchorTime)
	case "scaled":
		generateScaled(ctx, absPath, anchorTime, *scaleCount)
	case "review":
		generateReview(ctx, absPath, anchorTime)
	default:
		fmt.Fprintf(os.Stderr, "Unknown profile: %s. Supported: healthy, gaps, stale, empty, scaled, review\n", *profile)
		os.Exit(1)
	}

	fmt.Printf("[UI Fixture Generator] Successfully generated profile '%s'!\n", *profile)
}

func checkErr(op string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL FIXTURE ERROR] Operation '%s' failed: %v\n", op, err)
		os.Exit(1)
	}
}

func generateEmpty(ctx context.Context, dbPath string) {
	db, err := storage.OpenDB(ctx, dbPath)
	checkErr("OpenDB empty", err)
	defer db.Close()
	fmt.Println("  ✔ Empty database initialized with valid schema migrations.")
}

func generateHealthy(ctx context.Context, dbPath string, anchor time.Time) {
	sessionID := "sess-synthetic-healthy"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic")
	checkErr("OpenSQLiteSink healthy", err)

	// Keep the healthy sample inside the anchor's local "Today" window so the
	// default Overview/History surfaces show meaningful data during visual QA.
	baseTime := anchor.Add(-12 * time.Hour)
	seq := int64(1)

	// 1. Chrome 访问多个海外域名 (PROXY)
	emitConnSafe(sink, sessionID, 1, &seq, "c-chrome-1", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"github.com", "", "140.82.112.3", "443", "tcp", "DomainSuffix", "github.com", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 50000, 250000, baseTime.Add(1*time.Hour))

	emitConnSafe(sink, sessionID, 1, &seq, "c-chrome-2", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"google.com", "google.com", "142.250.190.46", "443", "tcp", "DomainKeyword", "google", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 15000, 120000, baseTime.Add(2*time.Hour))

	// 2. VS Code 访问 API (PROXY - Node-JP-02)
	emitConnSafe(sink, sessionID, 1, &seq, "c-code-1", "Code.exe", "C:\\Users\\Synthetic\\AppData\\Local\\Programs\\Microsoft VS Code\\Code.exe",
		"api.github.com", "", "140.82.112.4", "443", "tcp", "DomainSuffix", "github.com", types.RouteProxy,
		[]string{"Node-JP-02", "AutoSelect"}, 80000, 450000, baseTime.Add(4*time.Hour))

	// 3. Spotify 音频流 (PROXY - Node-US-03)
	emitConnSafe(sink, sessionID, 1, &seq, "c-spotify-1", "Spotify.exe", "C:\\Users\\Synthetic\\AppData\\Local\\Spotify\\Spotify.exe",
		"audio-ak.spotify.com", "audio-ak.spotify.com", "104.154.127.100", "443", "tcp", "DomainSuffix", "spotify.com", types.RouteProxy,
		[]string{"Node-US-03", "MediaGroup"}, 120000, 1850000, baseTime.Add(5*time.Hour))

	// 4. 国内直连流量 (DIRECT - 微信 / 百度)
	emitConnSafe(sink, sessionID, 1, &seq, "c-wechat-1", "WeChat.exe", "C:\\Program Files\\Tencent\\WeChat\\WeChat.exe",
		"szshort.weixin.qq.com", "", "183.6.84.10", "443", "tcp", "GeoIP", "CN", types.RouteDirect,
		[]string{"DIRECT"}, 45000, 890000, baseTime.Add(6*time.Hour))

	emitConnSafe(sink, sessionID, 1, &seq, "c-curl-1", "curl.exe", "C:\\Windows\\System32\\curl.exe",
		"baidu.com", "", "220.181.38.148", "80", "tcp", "DomainSuffix", "baidu.com", types.RouteDirect,
		[]string{"DIRECT"}, 1200, 3400, baseTime.Add(7*time.Hour))

	// 5. UDP 流量 (DNS / QUIC)
	emitConnSafe(sink, sessionID, 1, &seq, "c-dns-1", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"", "", "8.8.8.8", "53", "udp", "Match", "Final", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 512, 1024, baseTime.Add(8*time.Hour))

	// 6. 产生一次节点切换证据 (Node switch)
	emitConnSafe(sink, sessionID, 1, &seq, "c-chrome-switch", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"fastly.net", "", "151.101.1.57", "443", "tcp", "DomainSuffix", "fastly.net", types.RouteProxy,
		[]string{"Node-SG-01", "ProxyGroup"}, 30000, 150000, baseTime.Add(9*time.Hour))

	checkErr("EndSession healthy", sink.EndSession(ctx, sessionID, storage.SessionStatusClosedClean))
	checkErr("Close sink healthy", sink.Close())

	// 执行一次完整的 Accounting Rebuild
	db, err := storage.OpenDB(ctx, dbPath)
	checkErr("OpenDB rebuild", err)
	defer db.Close()

	_, err = storage.RebuildAccounting(ctx, db, "synthetic healthy profile build")
	checkErr("RebuildAccounting healthy", err)

	// The generator is intentionally time-anchored, while the normal sink uses
	// wall-clock session timestamps. Normalize the synthetic session so a
	// visual run performed minutes after generation still renders a healthy
	// Today window instead of manufacturing an offline trailing gap.
	startedAt := anchor.Add(-12 * time.Hour).Format(time.RFC3339Nano)
	lastEventAt := anchor.Add(-3 * time.Hour).Format(time.RFC3339Nano)
	qaEndAt := anchor.Add(1 * time.Hour).Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `
		UPDATE collector_sessions
		SET started_at = ?, ended_at = ?, last_event_at = ?, last_heartbeat_at = ?, updated_at = ?
		WHERE session_id = ?;
	`, startedAt, qaEndAt, lastEventAt, anchor.Format(time.RFC3339Nano), anchor.Format(time.RFC3339Nano), sessionID)
	checkErr("Normalize healthy visual session", err)
}

func generateGaps(ctx context.Context, dbPath string, anchor time.Time) {
	// 先生成基础数据
	generateHealthy(ctx, dbPath, anchor)

	db, err := storage.OpenDB(ctx, dbPath)
	checkErr("OpenDB gaps", err)
	defer db.Close()

	gap1Start := anchor.Add(-10 * time.Hour).Format(time.RFC3339Nano)
	gap1End := anchor.Add(-9 * time.Hour).Format(time.RFC3339Nano)

	gap2Start := anchor.Add(-5 * time.Hour).Format(time.RFC3339Nano)
	gap2End := anchor.Add(-4*time.Hour - 30*time.Minute).Format(time.RFC3339Nano)

	_, err = db.ExecContext(ctx, `
		INSERT INTO monitoring_gaps (
			gap_id, source, session_id, started_at, ended_at, duration_ms, reason,
			global_gap_upload_delta, global_gap_download_delta, physical_delta_unavailable, precision, created_at
		) VALUES
		('gap-controller-1', 'controller_stream', 'sess-synthetic-healthy', ?, ?, 3600000, 'Controller stream reconnect timeout', 204800, 1048576, 0, 'interval_derived', ?),
		('gap-collector-1', 'collector_session_boundary', 'sess-synthetic-healthy', ?, ?, 1800000, 'Collector daemon offline interval', 51200, 524288, 0, 'interval_derived', ?);
	`, gap1Start, gap1End, gap1End, gap2Start, gap2End, gap2End)
	checkErr("Insert monitoring_gaps", err)

	_, err = storage.RebuildAccounting(ctx, db, "synthetic gaps profile rebuild")
	checkErr("RebuildAccounting gaps", err)
}

func generateStale(ctx context.Context, dbPath string, anchor time.Time) {
	// 先生成 healthy 并完成 Rebuild
	generateHealthy(ctx, dbPath, anchor)

	sessionID := "sess-synthetic-stale-append"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic")
	checkErr("OpenSQLiteSink stale", err)

	seq := int64(100)
	emitConnSafe(sink, sessionID, 1, &seq, "c-stale-1", "curl.exe", "C:\\Windows\\System32\\curl.exe",
		"news.ycombinator.com", "", "178.62.207.240", "443", "tcp", "DomainKeyword", "ycombinator", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 2048, 8192, anchor.Add(-10*time.Minute))

	emitConnSafe(sink, sessionID, 1, &seq, "c-stale-2", "node.exe", "C:\\Program Files\\nodejs\\node.exe",
		"registry.npmjs.org", "", "104.16.16.35", "443", "tcp", "DomainSuffix", "npmjs.org", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 15000, 64000, anchor.Add(-5*time.Minute))

	checkErr("Close sink stale", sink.Close())
}

func generateScaled(ctx context.Context, dbPath string, anchor time.Time, totalEvents int) {
	sessionID := "sess-synthetic-scaled"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic-scaled")
	checkErr("OpenSQLiteSink scaled", err)

	processes := []struct {
		name, path, host, ip, rule, payload string
		route                               types.RouteType
		chains                              []string
	}{
		{"chrome.exe", "C:\\Program Files\\Google\\Chrome\\chrome.exe", "google.com", "142.250.190.46", "DomainKeyword", "google", types.RouteProxy, []string{"Node-HK-01", "ProxyGroup"}},
		{"chrome.exe", "C:\\Program Files\\Google\\Chrome\\chrome.exe", "github.com", "140.82.112.3", "DomainSuffix", "github.com", types.RouteProxy, []string{"Node-HK-01", "ProxyGroup"}},
		{"Code.exe", "C:\\VSCode\\Code.exe", "api.github.com", "140.82.112.4", "DomainSuffix", "github.com", types.RouteProxy, []string{"Node-JP-02", "AutoSelect"}},
		{"Spotify.exe", "C:\\Spotify\\Spotify.exe", "audio.spotify.com", "104.154.127.100", "DomainSuffix", "spotify.com", types.RouteProxy, []string{"Node-US-03", "MediaGroup"}},
		{"WeChat.exe", "C:\\Tencent\\WeChat.exe", "weixin.qq.com", "183.6.84.10", "GeoIP", "CN", types.RouteDirect, []string{"DIRECT"}},
		{"curl.exe", "C:\\Windows\\System32\\curl.exe", "baidu.com", "220.181.38.148", "DomainSuffix", "baidu.com", types.RouteDirect, []string{"DIRECT"}},
		{"slack.exe", "C:\\Slack\\slack.exe", "slack.com", "54.148.100.1", "DomainSuffix", "slack.com", types.RouteProxy, []string{"Node-SG-01", "ProxyGroup"}},
		{"node.exe", "C:\\Node\\node.exe", "registry.npmjs.org", "104.16.16.35", "DomainSuffix", "npmjs.org", types.RouteProxy, []string{"Node-HK-01", "ProxyGroup"}},
	}

	startWindow := anchor.Add(-30 * 24 * time.Hour)
	intervalStep := (30 * 24 * time.Hour) / time.Duration(totalEvents)

	fmt.Printf("  -> Ingesting %d events distributed over 30 days...\n", totalEvents)
	seq := int64(1)
	connSeen := make(map[string]bool)

	for i := 0; i < totalEvents; i++ {
		p := processes[i%len(processes)]
		t := startWindow.Add(time.Duration(i) * intervalStep)
		connID := fmt.Sprintf("c-scaled-%d", i%5000) // 5000 distinct connections

		evType := types.EventConnectionNew
		if connSeen[connID] {
			evType = types.EventConnectionDelta
		} else {
			connSeen[connID] = true
		}

		emitConnEvent(sink, sessionID, 1, &seq, connID, evType, p.name, p.path, p.host, "", p.ip, "443", "tcp", p.rule, p.payload, p.route, p.chains, int64(1024+(i%5000)), int64(4096+(i%20000)), t)
	}

	checkErr("EndSession scaled", sink.EndSession(ctx, sessionID, storage.SessionStatusClosedClean))
	checkErr("Close sink scaled", sink.Close())

	fmt.Println("  -> Ingestion completed. Running RebuildAccounting...")
	db, err := storage.OpenDB(ctx, dbPath)
	checkErr("OpenDB scaled", err)
	defer db.Close()

	t0 := time.Now()
	runRec, err := storage.RebuildAccounting(ctx, db, fmt.Sprintf("scaled profile build (%d events)", totalEvents))
	checkErr("RebuildAccounting scaled", err)
	fmt.Printf("  ✔ RebuildAccounting completed in %v (RunID: %s)\n", time.Since(t0), runRec.RunID)
}

func generateReview(ctx context.Context, dbPath string, anchor time.Time) {
	sessionID := "sess-synthetic-review"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic-review")
	checkErr("OpenSQLiteSink review", err)

	baseTime := anchor.Add(-10 * time.Hour)
	seq := int64(1)
	frame := emitConnEvent(sink, sessionID, 1, &seq, "c-review-match-1", types.EventConnectionNew,
		"fallback-app.exe", "C:\\Synthetic\\fallback-app.exe", "fallback.example", "", "198.51.100.10", "443", "tcp", "MATCH", "",
		types.RouteProxy, []string{"Node-Review-01", "ProxyGroup"}, 32768, 196608, baseTime)
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime)

	frame = emitConnEvent(sink, sessionID, 1, &seq, "c-review-match-2", types.EventConnectionNew,
		"browser.exe", "C:\\Synthetic\\browser.exe", "match.example", "", "198.51.100.11", "443", "tcp", "MATCH", "",
		types.RouteProxy, []string{"Node-Review-02", "ProxyGroup"}, 16384, 98304, baseTime.Add(20*time.Minute))
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime.Add(20*time.Minute))

	frame = emitConnEventWithInterval(sink, sessionID, 1, &seq, "c-review-udp", types.EventConnectionNew,
		"media.exe", "C:\\Synthetic\\media.exe", "", "", "198.51.100.22", "443", "udp", "NETWORK,udp", "",
		types.RouteProxy, []string{"Node-Review-03", "ProxyGroup"}, 4096, 8192, baseTime.Add(40*time.Minute),
		baseTime.Add(39*time.Minute), baseTime.Add(41*time.Minute))
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime.Add(40*time.Minute))

	frame = emitConnEvent(sink, sessionID, 1, &seq, "c-review-ip", types.EventConnectionNew,
		"sync.exe", "C:\\Synthetic\\sync.exe", "", "", "203.0.113.17", "443", "tcp", "DomainSuffix", "example",
		types.RouteProxy, []string{"Node-Review-04", "ProxyGroup"}, 65536, 524288, baseTime.Add(60*time.Minute))
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime.Add(60*time.Minute))

	frame = emitConnEvent(sink, sessionID, 1, &seq, "c-review-large", types.EventConnectionNew,
		"backup.exe", "C:\\Synthetic\\backup.exe", "archive.example", "", "192.0.2.44", "443", "tcp", "DomainSuffix", "archive.example",
		types.RouteProxy, []string{"Node-Review-05", "ProxyGroup"}, 4*1024*1024, 128*1024*1024, baseTime.Add(80*time.Minute))
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime.Add(80*time.Minute))

	frame = emitConnEvent(sink, sessionID, 1, &seq, "c-review-udp-direct", types.EventConnectionNew,
		"dns.exe", "C:\\Synthetic\\dns.exe", "resolver.example", "", "192.0.2.53", "53", "udp", "NETWORK,udp", "",
		types.RouteDirect, []string{"DIRECT"}, 2048, 4096, baseTime.Add(100*time.Minute))
	emitReviewResidual(sink, sessionID, 1, frame, &seq, baseTime.Add(100*time.Minute))

	checkErr("EndSession review", sink.EndSession(ctx, sessionID, storage.SessionStatusClosedClean))
	checkErr("Close sink review", sink.Close())

	db, err := storage.OpenDB(ctx, dbPath)
	checkErr("OpenDB review", err)
	defer db.Close()
	_, err = storage.RebuildAccounting(ctx, db, "synthetic review profile build")
	checkErr("RebuildAccounting review", err)

	startedAt := anchor.Add(-10 * time.Hour).Format(time.RFC3339Nano)
	lastEventAt := anchor.Add(-7 * time.Hour).Format(time.RFC3339Nano)
	qaEndAt := anchor.Add(1 * time.Hour).Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `
		UPDATE collector_sessions
		SET started_at = ?, ended_at = ?, last_event_at = ?, last_heartbeat_at = ?, updated_at = ?
		WHERE session_id = ?;
	`, startedAt, qaEndAt, lastEventAt, anchor.Format(time.RFC3339Nano), anchor.Format(time.RFC3339Nano), sessionID)
	checkErr("Normalize review visual session", err)
}

func emitReviewResidual(sink *storage.SQLiteEventSink, sessionID string, epoch int, frame int64, seq *int64, ts time.Time) {
	currentSeq := *seq
	*seq++
	ev := &types.CollectorEvent{
		EventID:       fmt.Sprintf("ev-seq-%d", currentSeq),
		SessionID:     sessionID,
		EpochID:       epoch,
		FrameSequence: frame,
		EventSequence: 2,
		Timestamp:     ts,
		Type:          types.EventSamplingResidual,
		Details:       map[string]any{"residualUpload": int64(0), "residualDownload": int64(0)},
	}
	if err := sink.Emit(ev); err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Emit review residual failed for event %s: %v\n", ev.EventID, err)
		os.Exit(1)
	}
}

func emitConnEvent(sink *storage.SQLiteEventSink, sessionID string, epoch int, seq *int64, connID string, evType types.EventType, proc, procPath, host, sniffHost, destIP string, destPort string, net, rule, rulePayload string, route types.RouteType, chains []string, up, down int64, t time.Time) int64 {
	return emitConnEventWithInterval(sink, sessionID, epoch, seq, connID, evType, proc, procPath, host, sniffHost, destIP, destPort, net, rule, rulePayload, route, chains, up, down, t, time.Time{}, time.Time{})
}

func emitConnEventWithInterval(sink *storage.SQLiteEventSink, sessionID string, epoch int, seq *int64, connID string, evType types.EventType, proc, procPath, host, sniffHost, destIP string, destPort string, net, rule, rulePayload string, route types.RouteType, chains []string, up, down int64, t, intervalStart, intervalEnd time.Time) int64 {
	currentSeq := *seq
	*seq++
	ev := &types.CollectorEvent{
		EventID:          fmt.Sprintf("ev-seq-%d", currentSeq),
		SessionID:        sessionID,
		EpochID:          epoch,
		FrameSequence:    currentSeq,
		EventSequence:    1,
		Type:             evType,
		Timestamp:        t,
		ConnectionID:     connID,
		Route:            route,
		AttributionClass: types.ClassKnownApplication,
		Metadata: types.RawMetadata{
			Process:         proc,
			ProcessPath:     procPath,
			Host:            host,
			SniffHost:       sniffHost,
			DestinationIP:   destIP,
			DestinationPort: destPort,
			Network:         net,
			SourceIP:        "127.0.0.1",
			SourcePort:      "54321",
			Type:            "HTTP",
		},
		Rule:                    rule,
		RulePayload:             rulePayload,
		Chains:                  chains,
		DeltaUpload:             up,
		DeltaDownload:           down,
		ObservedUploadCounter:   up,
		ObservedDownloadCounter: down,
	}
	if !intervalStart.IsZero() && !intervalEnd.IsZero() {
		ev.AttributionInterval = []string{intervalStart.UTC().Format(time.RFC3339Nano), intervalEnd.UTC().Format(time.RFC3339Nano)}
		ev.Precision = "interval_derived"
	}
	err := sink.Emit(ev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FATAL] Emit failed for event %s: %v\n", ev.EventID, err)
		os.Exit(1)
	}
	return currentSeq
}

func emitConnSafe(sink *storage.SQLiteEventSink, sessionID string, epoch int, seq *int64, connID, proc, procPath, host, sniffHost, destIP string, destPort string, net, rule, rulePayload string, route types.RouteType, chains []string, up, down int64, t time.Time) {
	emitConnEvent(sink, sessionID, epoch, seq, connID, types.EventConnectionNew, proc, procPath, host, sniffHost, destIP, destPort, net, rule, rulePayload, route, chains, up, down, t)
}
