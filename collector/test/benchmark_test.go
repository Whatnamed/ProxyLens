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
