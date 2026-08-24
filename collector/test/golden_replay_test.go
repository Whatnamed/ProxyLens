package test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Whatnamed/ProxyLens/collector/pkg/sink"
	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func computeCanonicalEventStreamHash(events []*types.CollectorEvent) string {
	hasher := sha256.New()
	for _, ev := range events {
		line := fmt.Sprintf(
			"%s|%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%v|%v\n",
			ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence,
			ev.Type, ev.ConnectionID, ev.Route, ev.AttributionClass,
			ev.DeltaUpload, ev.DeltaDownload,
			ev.MonitoredCumulativeUpload, ev.MonitoredCumulativeDownload,
			ev.QualityFlags, ev.PreexistingAtStart,
		)
		hasher.Write([]byte(line))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func TestCanonicalGoldenReplay20Iterations(t *testing.T) {
	fixturePath := filepath.Join("..", "testdata", "golden", "canonical-fixtures.ndjson")
	file, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("Failed to open golden fixture: %v", err)
	}
	defer file.Close()

	var rawLines [][]byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		cpy := make([]byte, len(b))
		copy(cpy, b)
		rawLines = append(rawLines, cpy)
	}

	if len(rawLines) == 0 {
		t.Fatalf("Golden fixture is empty")
	}

	var firstHash string

	for iteration := 0; iteration < 20; iteration++ {
		memSink := sink.NewMemorySink()
		engine := state.NewStateEngine(state.EngineOptions{
			Sink:      memSink,
			SessionID: "golden-deterministic-sess",
		})

		for _, line := range rawLines {
			var frame types.ConnectionSnapshotFrame
			if err := json.Unmarshal(line, &frame); err != nil {
				t.Fatalf("JSON parse error: %v", err)
			}
			if err := engine.ProcessFrame(&frame); err != nil {
				t.Fatalf("Engine error: %v", err)
			}
		}

		events := memSink.GetEvents()
		streamHash := computeCanonicalEventStreamHash(events)

		if iteration == 0 {
			firstHash = streamHash
		} else if streamHash != firstHash {
			t.Fatalf("Iteration %d produced divergent event stream hash!\nExpected: %s\nGot:      %s", iteration, firstHash, streamHash)
		}
	}
}
