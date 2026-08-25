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
	profile := flag.String("profile", "healthy", "Synthetic profile to generate: healthy | gaps | stale | empty")
	outPath := flag.String("out", "", "Output SQLite database path (e.g. ./fixtures/healthy.db)")
	flag.Parse()

	if *outPath == "" {
		*outPath = filepath.Join(".", fmt.Sprintf("fixture_%s.db", *profile))
	}

	// 确保父目录存在
	if dir := filepath.Dir(*outPath); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	// 如果文件已存在，先删除重建
	_ = os.Remove(*outPath)
	_ = os.Remove(*outPath + "-wal")
	_ = os.Remove(*outPath + "-shm")

	ctx := context.Background()
	absPath, _ := filepath.Abs(*outPath)
	fmt.Printf("[UI Fixture Generator] Generating profile '%s' at: %s\n", *profile, absPath)

	switch *profile {
	case "empty":
		generateEmpty(ctx, absPath)
	case "healthy":
		generateHealthy(ctx, absPath)
	case "gaps":
		generateGaps(ctx, absPath)
	case "stale":
		generateStale(ctx, absPath)
	default:
		fmt.Fprintf(os.Stderr, "Unknown profile: %s. Supported: healthy, gaps, stale, empty\n", *profile)
		os.Exit(1)
	}

	fmt.Printf("[UI Fixture Generator] Successfully generated profile '%s'!\n", *profile)
}

func generateEmpty(ctx context.Context, dbPath string) {
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize empty DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Println("  ✔ Empty database initialized with valid current migrations.")
}

func generateHealthy(ctx context.Context, dbPath string) {
	sessionID := "sess-synthetic-healthy"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic")
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenSQLiteSink failed: %v\n", err)
		os.Exit(1)
	}

	baseTime := time.Now().UTC().Add(-24 * time.Hour)
	seq := int64(1)

	// 1. Chrome 访问多个海外域名 (PROXY)
	emitConn(sink, sessionID, 1, &seq, "c-chrome-1", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"github.com", "", "140.82.112.3", 443, "tcp", "DomainSuffix", "github.com", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 50000, 250000, baseTime.Add(1*time.Hour))

	emitConn(sink, sessionID, 1, &seq, "c-chrome-2", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"google.com", "google.com", "142.250.190.46", 443, "tcp", "DomainKeyword", "google", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 15000, 120000, baseTime.Add(2*time.Hour))

	// 2. VS Code 访问 API (PROXY - 切换出口节点为 Node-JP-02)
	emitConn(sink, sessionID, 1, &seq, "c-code-1", "Code.exe", "C:\\Users\\Synthetic\\AppData\\Local\\Programs\\Microsoft VS Code\\Code.exe",
		"api.github.com", "", "140.82.112.4", 443, "tcp", "DomainSuffix", "github.com", types.RouteProxy,
		[]string{"Node-JP-02", "AutoSelect"}, 80000, 450000, baseTime.Add(4*time.Hour))

	// 3. Spotify 音频流 (PROXY - Node-US-03)
	emitConn(sink, sessionID, 1, &seq, "c-spotify-1", "Spotify.exe", "C:\\Users\\Synthetic\\AppData\\Local\\Spotify\\Spotify.exe",
		"audio-ak.spotify.com", "audio-ak.spotify.com", "104.154.127.100", 443, "tcp", "DomainSuffix", "spotify.com", types.RouteProxy,
		[]string{"Node-US-03", "MediaGroup"}, 120000, 1850000, baseTime.Add(5*time.Hour))

	// 4. 国内直连流量 (DIRECT - 微信 / 百度)
	emitConn(sink, sessionID, 1, &seq, "c-wechat-1", "WeChat.exe", "C:\\Program Files\\Tencent\\WeChat\\WeChat.exe",
		"szshort.weixin.qq.com", "", "183.6.84.10", 443, "tcp", "GeoIP", "CN", types.RouteDirect,
		[]string{"DIRECT"}, 45000, 890000, baseTime.Add(6*time.Hour))

	emitConn(sink, sessionID, 1, &seq, "c-curl-1", "curl.exe", "C:\\Windows\\System32\\curl.exe",
		"baidu.com", "", "220.181.38.148", 80, "tcp", "DomainSuffix", "baidu.com", types.RouteDirect,
		[]string{"DIRECT"}, 1200, 3400, baseTime.Add(7*time.Hour))

	// 5. UDP 流量 (DNS / QUIC)
	emitConn(sink, sessionID, 1, &seq, "c-dns-1", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"", "", "8.8.8.8", 53, "udp", "Match", "Final", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 512, 1024, baseTime.Add(8*time.Hour))

	// 6. 产生一次节点切换证据 (Node switch)
	emitConn(sink, sessionID, 1, &seq, "c-chrome-switch", "chrome.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"fastly.net", "", "151.101.1.57", 443, "tcp", "DomainSuffix", "fastly.net", types.RouteProxy,
		[]string{"Node-SG-01", "ProxyGroup"}, 30000, 150000, baseTime.Add(9*time.Hour))

	_ = sink.EndSession(ctx, sessionID, storage.SessionStatusClosedClean)
	_ = sink.Close()

	// 执行一次完整的 Accounting Rebuild
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenDB failed: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if _, err := storage.RebuildAccounting(ctx, db, "synthetic healthy profile build"); err != nil {
		fmt.Fprintf(os.Stderr, "RebuildAccounting failed: %v\n", err)
		os.Exit(1)
	}
}

func generateGaps(ctx context.Context, dbPath string) {
	// 先生成基础数据
	generateHealthy(ctx, dbPath)

	// 然后向 monitoring_gaps 插入一条 controller_stream 缺口和一条 collector_session_boundary 缺口
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenDB for gaps failed: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	tNow := time.Now().UTC()
	gap1Start := tNow.Add(-10 * time.Hour).Format(time.RFC3339Nano)
	gap1End := tNow.Add(-9 * time.Hour).Format(time.RFC3339Nano)

	gap2Start := tNow.Add(-5 * time.Hour).Format(time.RFC3339Nano)
	gap2End := tNow.Add(-4 * time.Hour - 30*time.Minute).Format(time.RFC3339Nano)

	_, _ = db.ExecContext(ctx, `
		INSERT INTO monitoring_gaps (
			gap_id, source, session_id, started_at, ended_at, duration_ms, reason,
			global_gap_upload_delta, global_gap_download_delta, physical_delta_unavailable, precision, created_at
		) VALUES
		('gap-controller-1', 'controller_stream', 'sess-synthetic-healthy', ?, ?, 3600000, 'Controller stream reconnect timeout', 204800, 1048576, 0, 'interval_derived', ?),
		('gap-collector-1', 'collector_session_boundary', 'sess-synthetic-healthy', ?, ?, 1800000, 'Collector daemon offline interval', 51200, 524288, 0, 'interval_derived', ?);
	`, gap1Start, gap1End, gap1End, gap2Start, gap2End, gap2End)

	// 重新聚合
	_, _ = storage.RebuildAccounting(ctx, db, "synthetic gaps profile rebuild")
}

func generateStale(ctx context.Context, dbPath string) {
	// 先生成 healthy 并完成 Rebuild
	generateHealthy(ctx, dbPath)

	// 然后以新 Session 追加数个未核算的 Journal 事件，制造 lagEvents > 0
	sessionID := "sess-synthetic-stale-append"
	sink, err := storage.OpenSQLiteSink(ctx, dbPath, sessionID, "v1.0.0-synthetic")
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenSQLiteSink failed: %v\n", err)
		os.Exit(1)
	}

	seq := int64(100)
	tNow := time.Now().UTC()
	emitConn(sink, sessionID, 1, &seq, "c-stale-1", "curl.exe", "C:\\Windows\\System32\\curl.exe",
		"news.ycombinator.com", "", "178.62.207.240", 443, "tcp", "DomainKeyword", "ycombinator", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 2048, 8192, tNow.Add(-10*time.Minute))

	emitConn(sink, sessionID, 1, &seq, "c-stale-2", "node.exe", "C:\\Program Files\\nodejs\\node.exe",
		"registry.npmjs.org", "", "104.16.16.35", 443, "tcp", "DomainSuffix", "npmjs.org", types.RouteProxy,
		[]string{"Node-HK-01", "ProxyGroup"}, 15000, 64000, tNow.Add(-5*time.Minute))

	_ = sink.Close()
	// 注意：这里故意不执行 RebuildAccounting，形成 stale 状态！
}

func emitConn(sink types.EventSink, sessionID string, epoch int, seq *int64, connID, proc, procPath, host, sniffHost, destIP string, destPort int, net, rule, rulePayload string, route types.RouteClassification, chains []string, up, down int64, t time.Time) {
	*seq++
	_ = sink.Emit(&types.CollectorEvent{
		EventID:          fmt.Sprintf("ev-%s-%d", connID, *seq),
		SessionID:        sessionID,
		EpochID:          epoch,
		FrameSequence:    *seq,
		EventSequence:    1,
		Type:             types.EventConnectionNew,
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
			SourcePort:      54321,
			Type:            "HTTP",
		},
		Rule:        rule,
		RulePayload: rulePayload,
		Chains:      chains,
		DeltaUpload: up, DeltaDownload: down,
		TotalUpload: up, TotalDownload: down,
	})
}
