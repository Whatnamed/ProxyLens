// proxylens-scale-acceptance runs the Phase 3S S4 production-scale acceptance
// phases against E:-drive databases. It never touches the C: production DB.
//
// Phases:
//
//	density        zero-heavy raw-density workload, legacy-vs-new row reduction
//	seed-real      migration + v2 seed of the E: copy of the real production DB
//	soak           continuous collector + incremental accounting + query workload
//	constant-cost  identical appended batch on small vs large history
//	crash          cancel/rollback/boundary and checkpoint contention checks
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/state"
	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
	"github.com/Whatnamed/ProxyLens/collector/pkg/types"
)

func main() {
	phase := flag.String("phase", "", "density|seed-real|soak|constant-cost|crash|cardinality")
	dir := flag.String("dir", "", "working directory (must be on an allowed test volume)")
	realDB := flag.String("real-db", "", "path to the E: copy of the real production DB")
	minutes := flag.Float64("minutes", 30, "soak duration in minutes")
	chunk := flag.Int64("chunk-events", 50000, "journal events per accounting chunk")
	flag.Parse()

	if *phase == "" || *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: proxylens-scale-acceptance -phase <name> -dir <E:\\dir> [-real-db <path>] [-minutes 30]")
		os.Exit(2)
	}

	// Fail-closed volume guard: every path this harness writes to - including
	// the working directory and any database it opens read-write - must live
	// on an explicitly allowed non-system test volume. The C: system volume
	// and the production DB must never become a scale target.
	if err := enforceAllowedVolumes(*dir, *realDB); err != nil {
		fatal("volume guard: %v", err)
	}

	// SQLite spill strategy: temp files (sorter spills, temp b-trees) must
	// land on the allowed test volume too, never in the user profile temp on
	// the system drive. Set before any database is opened.
	tempDir := filepath.Join(*dir, ".tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		fatal("create temp dir: %v", err)
	}
	_ = os.Setenv("TMP", tempDir)
	_ = os.Setenv("TEMP", tempDir)
	_ = os.Setenv("SQLITE_TMPDIR", tempDir)

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fatal("create dir: %v", err)
	}

	ctx := context.Background()
	var err error
	switch *phase {
	case "density":
		err = runDensity(ctx, *dir)
	case "seed-real":
		err = runSeedReal(ctx, *realDB, *chunk)
	case "soak":
		err = runSoak(ctx, *dir, time.Duration(*minutes*float64(time.Minute)))
	case "constant-cost":
		err = runConstantCost(ctx, *dir, *realDB)
	case "crash":
		err = runCrash(ctx, *dir, *chunk)
	case "cardinality":
		err = runCardinality(ctx, *dir, *chunk)
	default:
		fatal("unknown phase %q", *phase)
	}
	if err != nil {
		fatal("phase %s failed: %v", *phase, err)
	}
	fmt.Printf("[scale] phase %s PASS\n", *phase)
}

// enforceAllowedVolumes fails closed unless every supplied path is on an
// explicitly allowed test volume. Default allowed volume: E:\. The environment
// variable PROXYLENS_SCALE_ALLOWED_VOLUMES may add volumes (semicolon
// separated, e.g. "X:\;Y:\") for isolated test environments; C: can never be
// added.
func enforceAllowedVolumes(paths ...string) error {
	allowed := map[string]bool{"E:": true}
	if extra := os.Getenv("PROXYLENS_SCALE_ALLOWED_VOLUMES"); extra != "" {
		for _, v := range strings.Split(extra, ";") {
			v = strings.ToUpper(strings.TrimSpace(v))
			v = strings.TrimSuffix(v, "\\")
			if v == "C:" || v == "" {
				continue
			}
			allowed[v] = true
		}
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", p, err)
		}
		vol := strings.ToUpper(filepath.VolumeName(abs))
		if !allowed[vol] {
			return fmt.Errorf("path %q is on volume %s which is not an allowed scale-test volume (allowed: %v); refusing to run - the production system drive and its databases must never be a scale target", p, vol, allowed)
		}
	}
	return nil
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "[scale] FAIL: "+f+"\n", a...)
	os.Exit(1)
}

func report(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Printf("[scale] report:\n%s\n", b)
}

func fileBytes(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
}

func dirBytes(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := f.WriteTo(h); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// -----------------------------------------------------------------------------
// density: zero-heavy raw-density workload
// -----------------------------------------------------------------------------

type densityReport struct {
	Frames                  int     `json:"frames"`
	Connections             int     `json:"connections"`
	LegacyDurableRows       int     `json:"legacyDurableRows"`
	DeltaRows               int     `json:"deltaRows"`
	PresenceRows            int     `json:"presenceRows"`
	TrafficTableRows        int     `json:"trafficTableRows"`
	ReductionPct            float64 `json:"reductionPct"`
	InputUploadSum          int64   `json:"inputUploadSum"`
	InputDownloadSum        int64   `json:"inputDownloadSum"`
	EmittedDeltaUploadSum   int64   `json:"emittedDeltaUploadSum"`
	EmittedDeltaDownloadSum int64   `json:"emittedDeltaDownloadSum"`
	GapEvidence             bool    `json:"gapEvidence"`
	DisappearanceCloses     bool    `json:"disappearanceCloses"`
	PeakHeapMB              float64 `json:"peakHeapMB"`
}

func runDensity(ctx context.Context, dir string) error {
	dbPath := filepath.Join(dir, "density.db")
	_ = os.Remove(dbPath)
	db, err := storage.OpenDB(ctx, dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	s, err := storage.OpenSQLiteSink(ctx, dbPath, "sess-density", "scale-tool")
	if err != nil {
		return err
	}

	engine := state.NewStateEngine(state.EngineOptions{Sink: s, SessionID: "sess-density"})

	const conns = 50
	const frames = 4800 // 20 minutes at 250ms
	base := time.Now().UTC().Add(-24 * time.Hour)

	// Deterministic zero-heavy profile: ~2.4% of frame-conn pairs carry bytes.
	type connGen struct {
		up, down int64
	}
	gens := make([]connGen, conns)
	inputUp, inputDown := int64(0), int64(0)

	emitFrame := func(i int) error {
		ts := base.Add(time.Duration(i) * 250 * time.Millisecond)
		connsSnap := make([]types.ConnectionSnapshot, 0, conns)
		for c := 0; c < conns; c++ {
			// Zero-heavy: every 42nd frame-conn pair gets traffic (~2.38%).
			// The bootstrap frame establishes baselines and emits no deltas,
			// so bursts are only injected from the first steady frame on.
			if i > 0 && (i*conns+c)%42 == 7 {
				gens[c].up += 500
				gens[c].down += 900
				inputUp += 500
				inputDown += 900
			}
			connsSnap = append(connsSnap, types.ConnectionSnapshot{
				ID:       fmt.Sprintf("conn-%03d", c),
				Upload:   gens[c].up,
				Download: gens[c].down,
				Metadata: types.RawMetadata{Process: fmt.Sprintf("proc-%02d.exe", c%7), Host: fmt.Sprintf("host-%02d.example", c%11), Network: "tcp"},
				Rule:     "MATCH",
				Chains:   []string{"Node-A", "Group-X"},
			})
		}
		var totalUp, totalDown int64
		for _, g := range gens {
			totalUp += g.up
			totalDown += g.down
		}
		frame := &types.ConnectionSnapshotFrame{
			ReceivedAt: ts.Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal:   100000 + totalUp,
				DownloadTotal: 200000 + totalDown,
				Connections:   connsSnap,
			},
		}
		return engine.ProcessFrame(frame)
	}

	// Frame 0 bootstrap, then steady frames.
	if err := emitFrame(0); err != nil {
		return err
	}
	var peakHeap float64
	for i := 1; i < frames-10; i++ {
		if err := emitFrame(i); err != nil {
			return err
		}
		if i%500 == 0 {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			peak := float64(ms.HeapAlloc) / (1 << 20)
			if peak > peakHeap {
				peakHeap = peak
			}
		}
	}

	// Gap + recovery + disappearance evidence.
	if err := engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemGapOpened,
		Timestamp: base.Add(time.Duration(frames-10) * 250 * time.Millisecond),
	}); err != nil {
		return err
	}
	recoveryTs := base.Add(time.Duration(frames-5) * 250 * time.Millisecond)
	connsSnap := make([]types.ConnectionSnapshot, 0, conns)
	var totalUp, totalDown int64
	for c := 0; c < conns; c++ {
		connsSnap = append(connsSnap, types.ConnectionSnapshot{
			ID: fmt.Sprintf("conn-%03d", c), Upload: gens[c].up, Download: gens[c].down,
			Metadata: types.RawMetadata{Process: fmt.Sprintf("proc-%02d.exe", c%7), Host: fmt.Sprintf("host-%02d.example", c%11), Network: "tcp"},
			Rule:     "MATCH", Chains: []string{"Node-A", "Group-X"},
		})
		totalUp += gens[c].up
		totalDown += gens[c].down
	}
	if err := engine.ProcessIngestItem(&types.IngestItem{
		Kind:      types.ItemFrame,
		Timestamp: recoveryTs,
		Frame: &types.ConnectionSnapshotFrame{
			ReceivedAt: recoveryTs.Format(time.RFC3339Nano),
			Frame: types.ConnectionSnapshotPayload{
				UploadTotal: 100000 + totalUp, DownloadTotal: 200000 + totalDown, Connections: connsSnap,
			},
		},
	}); err != nil {
		return err
	}
	// Disappear everything.
	if err := engine.ProcessFrame(&types.ConnectionSnapshotFrame{
		ReceivedAt: base.Add(time.Duration(frames) * 250 * time.Millisecond).Format(time.RFC3339Nano),
		Frame: types.ConnectionSnapshotPayload{
			UploadTotal: 100000 + totalUp, DownloadTotal: 200000 + totalDown,
		},
	}); err != nil {
		return err
	}

	if err := s.EndSession(ctx, "sess-density", storage.SessionStatusClosedClean); err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}

	var deltaRows, presenceRows, trafficRows int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type='ConnectionDelta';`).Scan(&deltaRows); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type='ConnectionPresenceCheckpoint';`).Scan(&presenceRows); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_traffic;`).Scan(&trafficRows); err != nil {
		return err
	}
	var gapEvents int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_journal WHERE event_type IN ('MonitoringGapOpened','MonitoringGapClosed');`).Scan(&gapEvents); err != nil {
		return err
	}
	var disappeared int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connections WHERE state='disappeared_from_snapshot' AND disappeared_observed_at IS NOT NULL;`).Scan(&disappeared); err != nil {
		return err
	}

	var emUp, emDown int64
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CAST(json_extract(event_json,'$.deltaUpload') AS INTEGER)),0),
		       COALESCE(SUM(CAST(json_extract(event_json,'$.deltaDownload') AS INTEGER)),0)
		FROM event_journal WHERE event_type='ConnectionDelta';`).Scan(&emUp, &emDown); err != nil {
		return err
	}

	legacyDurable := (frames - 10) * conns // pre-fix: one Delta per frame per live connection
	rep := densityReport{
		Frames: frames, Connections: conns,
		LegacyDurableRows: legacyDurable,
		DeltaRows:         int(deltaRows), PresenceRows: int(presenceRows), TrafficTableRows: int(trafficRows),
		ReductionPct:   100 * float64(legacyDurable-int(deltaRows)) / float64(legacyDurable),
		InputUploadSum: inputUp, InputDownloadSum: inputDown,
		EmittedDeltaUploadSum: emUp, EmittedDeltaDownloadSum: emDown,
		GapEvidence:         gapEvents >= 2,
		DisappearanceCloses: disappeared == conns,
		PeakHeapMB:          peakHeap,
	}
	report(rep)

	if rep.ReductionPct < 90 {
		return fmt.Errorf("durable delta row reduction %.1f%% below 90%% gate", rep.ReductionPct)
	}
	if emUp != inputUp || emDown != inputDown {
		return fmt.Errorf("byte sums diverge: input %d/%d emitted %d/%d", inputUp, inputDown, emUp, emDown)
	}
	if !rep.GapEvidence || !rep.DisappearanceCloses {
		return fmt.Errorf("lifecycle evidence incomplete: gap=%v disappeared=%d/%d", rep.GapEvidence, disappeared, conns)
	}
	return nil
}
