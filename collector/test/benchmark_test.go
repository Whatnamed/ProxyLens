package test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func BenchmarkCollectorStateEngine(b *testing.B) {
	fixturePath := filepath.Join("..", "..", "tmp", "phase1-language-spike", "fixtures", "benchmark-frames.ndjson")
	file, err := os.Open(fixturePath)
	if err != nil {
		b.Skipf("Benchmark fixture not found: %v", err)
	}
	defer file.Close()

	var frames []*types.ConnectionSnapshotFrame
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var f types.ConnectionSnapshotFrame
		if err := json.Unmarshal(line, &f); err == nil {
			frames = append(frames, &f)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		memSink := sink.NewMemorySink()
		engine := state.NewStateEngine(state.EngineOptions{Sink: memSink})

		for _, frame := range frames {
			_ = engine.ProcessFrame(frame)
		}
	}
}

func TestActiveMapMemoryReclaimSoak(t *testing.T) {
	memSink := sink.NewMemorySink()
	engine := state.NewStateEngine(state.EngineOptions{Sink: memSink})

	// Frame 0: Bootstrap 100 conns
	conns := make([]types.ConnectionSnapshot, 100)
	for i := 0; i < 100; i++ {
		conns[i] = types.ConnectionSnapshot{
			ID:     string(rune('A'+i/26)) + string(rune('a'+i%26)),
			Upload: 100, Download: 200,
			Metadata: types.RawMetadata{Process: "chrome.exe"}, Rule: "Match", Chains: []string{"DIRECT"},
		}
	}
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:00.000Z",
		Frame:      types.ConnectionSnapshotPayload{UploadTotal: 1000, DownloadTotal: 2000, Connections: conns},
	})
	if engine.GetActiveConnectionsCount() != 100 {
		t.Fatalf("Expected 100 active connections")
	}

	// Frame 1: All 100 connections disappear
	_ = engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: "2026-08-21T00:00:01.000Z",
		Frame:      types.ConnectionSnapshotPayload{UploadTotal: 1000, DownloadTotal: 2000, Connections: []types.ConnectionSnapshot{}},
	})

	// 断言 activeMap 必须完全释放回收，cardinality = 0 (无内存泄漏)
	if engine.GetActiveConnectionsCount() != 0 {
		t.Errorf("Active connections must be reclaimed to 0, got %d", engine.GetActiveConnectionsCount())
	}
}
