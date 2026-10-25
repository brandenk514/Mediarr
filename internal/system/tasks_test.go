package system

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskRegistrySnapshotOrderAndCounts(t *testing.T) {
	r := NewTaskRegistry()
	r.Register(TaskSpec{Name: "indexer_health", Kind: TaskScheduled, Interval: time.Minute, IntervalLabel: "every 1m", Enabled: true, Run: func(context.Context) error { return nil }})
	r.Register(TaskSpec{Name: "import_feed", Kind: TaskPlanned, Enabled: false})
	r.Register(TaskSpec{Name: "manual_probe", Kind: TaskOnDemand, Enabled: true, Run: func(context.Context) error { return nil }})

	snap := r.Snapshot()
	if len(snap.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(snap.Tasks))
	}
	// Registration order stable.
	if snap.Tasks[0].Name != "indexer_health" || snap.Tasks[1].Name != "import_feed" {
		t.Fatalf("order wrong: %v, %v", snap.Tasks[0].Name, snap.Tasks[1].Name)
	}
	// Only the one scheduled+enabled task counts as scheduled.
	if snap.Scheduled != 1 {
		t.Fatalf("Scheduled=%d want 1", snap.Scheduled)
	}
	if snap.Running != 0 {
		t.Fatalf("Running=%d want 0", snap.Running)
	}
	// Interval label propagated.
	if snap.Tasks[0].Interval != "every 1m" {
		t.Fatalf("interval=%q want every 1m", snap.Tasks[0].Interval)
	}
	// Planned task not enabled.
	if snap.Tasks[1].Enabled {
		t.Fatalf("planned task should be disabled")
	}
}

func TestTaskRegistryRunOnceRecordsState(t *testing.T) {
	r := NewTaskRegistry()
	r.Register(TaskSpec{Name: "ok", Kind: TaskOnDemand, Enabled: true, Run: func(context.Context) error { return nil }})
	r.Register(TaskSpec{Name: "boom", Kind: TaskOnDemand, Enabled: true, Run: func(context.Context) error { return errors.New("kaboom") }})

	if err := r.RunOnce(context.Background(), "ok"); err != nil {
		t.Fatalf("ok run: %v", err)
	}
	if err := r.RunOnce(context.Background(), "boom"); !errors.Is(err, errors.New("kaboom")) && err == nil {
		t.Fatalf("boom should error")
	}

	snap := r.Snapshot()
	var ok, boom *TaskInfo
	for i := range snap.Tasks {
		switch snap.Tasks[i].Name {
		case "ok":
			ok = &snap.Tasks[i]
		case "boom":
			boom = &snap.Tasks[i]
		}
	}
	if ok.LastRun == nil {
		t.Fatalf("ok should have a last run")
	}
	if ok.LastError != "" {
		t.Fatalf("ok last error=%q want empty", ok.LastError)
	}
	if boom.LastError != "kaboom" {
		t.Fatalf("boom last error=%q want kaboom", boom.LastError)
	}
	if ok.Running || boom.Running {
		t.Fatalf("tasks should not be running after completion")
	}
}

func TestTaskRegistryRunOnceUnknownAndNoRun(t *testing.T) {
	r := NewTaskRegistry()
	r.Register(TaskSpec{Name: "planned", Kind: TaskPlanned}) // no Run
	if err := r.RunOnce(context.Background(), "nope"); err == nil {
		t.Fatalf("expected error for unknown task")
	}
	if err := r.RunOnce(context.Background(), "planned"); err == nil {
		t.Fatalf("expected error for task without runnable work")
	}
}

func TestTaskRegistryConcurrentRunningGuard(t *testing.T) {
	r := NewTaskRegistry()
	release := make(chan struct{})
	r.Register(TaskSpec{Name: "slow", Kind: TaskOnDemand, Enabled: true, Run: func(context.Context) error {
		<-release
		return nil
	}})

	done := make(chan error, 1)
	go func() { done <- r.RunOnce(context.Background(), "slow") }()
	// Give the goroutine a moment to start and set running.
	time.Sleep(20 * time.Millisecond)
	if err := r.RunOnce(context.Background(), "slow"); err == nil {
		t.Fatalf("expected 'already running' guard error")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
}

func TestTaskRegistryStartStopScheduled(t *testing.T) {
	r := NewTaskRegistry()
	ticks := make(chan struct{}, 10)
	r.Register(TaskSpec{
		Name:     "tick",
		Kind:     TaskScheduled,
		Interval: 10 * time.Millisecond,
		Enabled:  true,
		Run: func(context.Context) error {
			ticks <- struct{}{}
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx, "tick")
	// Expect at least one tick within the window.
	select {
	case <-ticks:
	case <-time.After(time.Second):
		t.Fatalf("no tick observed")
	}
	r.Stop("tick")
	cancel()
	// Snapshot still valid after stop.
	snap := r.Snapshot()
	if len(snap.Tasks) != 1 {
		t.Fatalf("expected 1 task")
	}
}

func TestTaskRegistryStartNoopForNonScheduled(t *testing.T) {
	r := NewTaskRegistry()
	r.Register(TaskSpec{Name: "planned", Kind: TaskPlanned})
	// Start on a non-scheduled task must be a no-op and must not panic.
	r.Start(context.Background(), "planned")
	r.Start(context.Background(), "missing")
	if snap := r.Snapshot(); snap.Scheduled != 0 {
		t.Fatalf("Scheduled=%d want 0", snap.Scheduled)
	}
}
