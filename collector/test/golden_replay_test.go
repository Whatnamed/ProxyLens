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

func computeCanonicalFullEventStreamHash(events []*types.CollectorEvent) string {
	hasher := sha256.New()
	for _, ev := range events {
		detailsBytes, _ := json.Marshal(ev.Details)
		line := fmt.Sprintf(
			"%s|%s|%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%t|%t|%+v|%s\n",
			ev.EventID, ev.SessionID, ev.EpochID, ev.FrameSequence, ev.EventSequence,
			ev.Type, ev.ConnectionID, ev.Route, ev.AttributionClass,
			ev.ObservedUploadCounter, ev.ObservedDownloadCounter,
			ev.BaselineUploadCounter, ev.BaselineDownloadCounter,
			ev.DeltaUpload, ev.DeltaDownload,
			ev.MonitoredCumulativeUpload > 0, ev.PreexistingAtStart,
			ev.QualityFlags,
			string(detailsBytes),
		)
		hasher.Write([]byte(line))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func TestCanonicalGoldenReplay50Iterations(t *testing.T) {
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
	var firstEventIDs []string

	for iteration := 0; iteration < 50; iteration++ {
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
		streamHash := computeCanonicalFullEventStreamHash(events)

		// 检查 EventID 唯一性
		seenIDs := make(map[string]bool, len(events))
		currentEventIDs := make([]string, len(events))
		for idx, ev := range events {
			if seenIDs[ev.EventID] {
				t.Fatalf("Duplicate EventID detected: %s", ev.EventID)
			}
			seenIDs[ev.EventID] = true
			currentEventIDs[idx] = ev.EventID
		}

		if iteration == 0 {
			firstHash = streamHash
			firstEventIDs = currentEventIDs
		} else {
			if streamHash != firstHash {
				t.Fatalf("Iteration %d produced divergent event stream hash!\nExpected: %s\nGot:      %s", iteration, firstHash, streamHash)
			}
			for idx, id := range currentEventIDs {
				if id != firstEventIDs[idx] {
					t.Fatalf("Iteration %d produced mismatched EventID at index %d: expected %s, got %s", iteration, idx, firstEventIDs[idx], id)
				}
			}
		}
	}
}
