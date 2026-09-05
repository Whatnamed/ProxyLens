package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Whatnamed/ProxyLens/collector/pkg/storage"
)

func freshness(current, lag int64) *storage.AccountingFreshness {
	return &storage.AccountingFreshness{
		CurrentJournalSequenceMax: current,
		LagEvents:                 lag,
		IsFresh:                   lag == 0,
	}
}

func testRun() *storage.AccountingRunRecord {
	return &storage.AccountingRunRecord{RunID: "run-test", SourceJournalSequenceMax: ptrInt64(1)}
}

func ptrInt64(v int64) *int64 { return &v }

func TestAccountingSchedulerSkipAndRebuildDecisions(t *testing.T) {
	var rebuilds int
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			return freshness(0, 0), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			rebuilds++
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	result := scheduler.Tick(context.Background())
	if result.Action != AccountingSkippedNoEvents || rebuilds != 0 {
		t.Fatalf("no-events tick mismatch: %+v rebuilds=%d", result, rebuilds)
	}

	var current atomic.Int64
	current.Store(3)
	scheduler, err = NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			return freshness(current.Load(), 3), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			rebuilds++
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	result = scheduler.Tick(context.Background())
	if result.Action != AccountingRebuilt || result.Run == nil || rebuilds != 1 {
		t.Fatalf("lag tick mismatch: %+v rebuilds=%d", result, rebuilds)
	}
}

func TestAccountingSchedulerFreshAndNewEventDecisions(t *testing.T) {
	var mu sync.Mutex
	lag := int64(2)
	var rebuilds int
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			mu.Lock()
			defer mu.Unlock()
			return freshness(2, lag), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			mu.Lock()
			rebuilds++
			lag = 0
			mu.Unlock()
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	if result := scheduler.Tick(context.Background()); result.Action != AccountingRebuilt {
		t.Fatalf("first stale tick mismatch: %+v", result)
	}
	if result := scheduler.Tick(context.Background()); result.Action != AccountingSkippedFresh {
		t.Fatalf("fresh tick mismatch: %+v", result)
	}

	mu.Lock()
	lag = 1
	mu.Unlock()
	if result := scheduler.Tick(context.Background()); result.Action != AccountingRebuilt {
		t.Fatalf("new event stale tick mismatch: %+v", result)
	}
	if rebuilds != 2 {
		t.Fatalf("expected two rebuilds after new event, got %d", rebuilds)
	}
}

func TestAccountingSchedulerFailureSurvivesAndRetries(t *testing.T) {
	var rebuilds int
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			return freshness(4, 1), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			rebuilds++
			if rebuilds == 1 {
				return nil, errors.New("injected rebuild failure")
			}
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	first := scheduler.Tick(context.Background())
	if first.Action != AccountingFailed || first.Err == nil {
		t.Fatalf("expected first rebuild failure, got %+v", first)
	}
	second := scheduler.Tick(context.Background())
	if second.Action != AccountingRebuilt || second.Err != nil || rebuilds != 2 {
		t.Fatalf("expected retry success, got %+v rebuilds=%d", second, rebuilds)
	}
}

func TestAccountingSchedulerRunContinuesAfterFailure(t *testing.T) {
	var rebuilds atomic.Int64
	results := make(chan AccountingTickAction, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Interval: 10 * time.Millisecond,
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			return freshness(2, 1), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			if rebuilds.Add(1) == 1 {
				return nil, errors.New("injected first-run failure")
			}
			return testRun(), nil
		},
		OnTick: func(result AccountingTickResult) {
			results <- result.Action
			if result.Action == AccountingRebuilt {
				cancel()
			}
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()

	seenFailure := false
	seenSuccess := false
	deadline := time.After(time.Second)
	for !seenSuccess {
		select {
		case action := <-results:
			seenFailure = seenFailure || action == AccountingFailed
			seenSuccess = seenSuccess || action == AccountingRebuilt
		case <-deadline:
			t.Fatalf("scheduler did not retry after failure: failure=%v success=%v", seenFailure, seenSuccess)
		}
	}
	if !seenFailure {
		t.Fatal("scheduler success was observed without the injected failure result")
	}
	if err := <-done; err != nil {
		t.Fatalf("scheduler returned an error after cancellation: %v", err)
	}
}

func TestAccountingSchedulerDoesNotOverlapRebuilds(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var active atomic.Int64
	var maxActive atomic.Int64
	var calls atomic.Int64

	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(context.Context) (*storage.AccountingFreshness, error) {
			return freshness(2, 1), nil
		},
		Rebuild: func(ctx context.Context, _ string) (*storage.AccountingRunRecord, error) {
			calls.Add(1)
			current := active.Add(1)
			for {
				previous := maxActive.Load()
				if current <= previous || maxActive.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}

	firstDone := make(chan AccountingTickResult, 1)
	secondDone := make(chan AccountingTickResult, 1)
	go func() { firstDone <- scheduler.Tick(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first rebuild did not start")
	}
	go func() { secondDone <- scheduler.Tick(context.Background()) }()
	time.Sleep(30 * time.Millisecond)
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("rebuilds overlapped, max active=%d", got)
	}
	close(release)
	<-firstDone
	<-secondDone
	if calls.Load() != 2 || maxActive.Load() != 1 {
		t.Fatalf("expected two serialized rebuild calls, calls=%d max=%d", calls.Load(), maxActive.Load())
	}
}

func TestAccountingSchedulerCancellationExitsCleanly(t *testing.T) {
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Interval: time.Hour,
		Freshness: func(ctx context.Context) (*storage.AccountingFreshness, error) {
			return freshness(0, 0), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			return testRun(), nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scheduler cancellation returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit after cancellation")
	}
}

func TestAccountingSchedulerRunInvokesCheckpointPerTick(t *testing.T) {
	var ticks atomic.Int64
	var checkpoints atomic.Int64
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Interval: 10 * time.Millisecond,
		Freshness: func(ctx context.Context) (*storage.AccountingFreshness, error) {
			ticks.Add(1)
			return freshness(0, 0), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			return testRun(), nil
		},
		Checkpoint: func(ctx context.Context) error {
			checkpoints.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scheduler Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("scheduler did not exit")
	}
	if ticks.Load() == 0 || checkpoints.Load() < ticks.Load() {
		t.Fatalf("checkpoint coverage mismatch: ticks=%d checkpoints=%d", ticks.Load(), checkpoints.Load())
	}
}

func TestAccountingSchedulerCheckpointFailureIsNonFatal(t *testing.T) {
	scheduler, err := NewAccountingSchedulerWithOptions(AccountingSchedulerOptions{
		Freshness: func(ctx context.Context) (*storage.AccountingFreshness, error) {
			return freshness(0, 0), nil
		},
		Rebuild: func(context.Context, string) (*storage.AccountingRunRecord, error) {
			return testRun(), nil
		},
		Checkpoint: func(ctx context.Context) error {
			return errors.New("checkpoint exploded")
		},
	})
	if err != nil {
		t.Fatalf("NewAccountingSchedulerWithOptions failed: %v", err)
	}
	result := scheduler.Tick(context.Background())
	if result.Action != AccountingSkippedNoEvents {
		t.Fatalf("checkpoint failure must not alter tick result, got %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scheduler Run returned error despite checkpoint failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit")
	}
}
