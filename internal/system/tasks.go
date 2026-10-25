package system

import (
	"context"
	"sync"
	"time"
)

// TaskKind classifies a background task in the worker inventory.
type TaskKind string

const (
	// TaskScheduled is a periodic worker (e.g. the indexer health sweep).
	TaskScheduled TaskKind = "scheduled"
	// TaskOnDemand runs only when triggered (e.g. an on-demand probe).
	TaskOnDemand TaskKind = "on-demand"
	// TaskPlanned is in the PLAN.md §3 inventory but not yet implemented —
	// it is listed so the Tasks page shows the full worker roster.
	TaskPlanned TaskKind = "planned"
)

// TaskInfo is the inspection/wire shape of one task.
type TaskInfo struct {
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Kind         TaskKind   `json:"kind"`
	Interval     string     `json:"interval,omitempty"`
	LastRun      *time.Time `json:"last_run"`
	LastDuration string     `json:"last_duration,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	Running      bool       `json:"running"`
	Enabled      bool       `json:"enabled"`
}

// TaskSpec describes a task to register. For scheduled tasks Interval must be
// positive and Run non-nil; planned tasks carry metadata only.
type TaskSpec struct {
	Name          string
	Description   string
	Kind          TaskKind
	Interval      time.Duration
	IntervalLabel string
	Enabled       bool
	// Run is the work performed by one tick. Nil for planned tasks.
	Run func(ctx context.Context) error
}

// SystemTasks is the full task inventory report.
type SystemTasks struct {
	Tasks     []TaskInfo `json:"tasks"`
	Running   int        `json:"running"`
	Scheduled int        `json:"scheduled"`
}

type trackedTask struct {
	spec    TaskSpec
	mu      sync.Mutex
	running bool
	lastRun *time.Time
	lastDur string
	lastErr string
	cancel  context.CancelFunc
}

// TaskRegistry tracks background tasks and exposes their live state so the
// Tasks page can show running + scheduled workers (PLAN.md §3). It does not
// reinvent the workers: main.go registers the real workers' sweep functions
// and the planned roster; the registry only records state and (for scheduled
// tasks) drives the tick loop.
type TaskRegistry struct {
	mu    sync.RWMutex
	byKey map[string]*trackedTask
	order []string
}

// NewTaskRegistry builds an empty registry.
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{byKey: map[string]*trackedTask{}}
}

// Register adds (or replaces) a task. It keeps registration order stable so
// the Tasks page renders a consistent roster.
func (r *TaskRegistry) Register(spec TaskSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byKey[spec.Name]; !ok {
		r.order = append(r.order, spec.Name)
	}
	r.byKey[spec.Name] = &trackedTask{spec: spec}
}

// RunOnce runs the named task a single time and records the outcome
// (last-run time, duration, error). It is the hook the API uses for
// on-demand triggers and the unit the periodic loop calls.
func (r *TaskRegistry) RunOnce(ctx context.Context, name string) error {
	t, ok := r.get(name)
	if !ok {
		return errf("task %q is not registered", name)
	}
	if t.spec.Run == nil {
		return errf("task %q has no runnable work", name)
	}
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return errf("task %q is already running", name)
	}
	t.running = true
	t.mu.Unlock()

	start := time.Now()
	err := t.spec.Run(ctx)
	dur := time.Since(start)

	t.mu.Lock()
	now := time.Now()
	t.lastRun = &now
	t.lastDur = dur.Round(time.Millisecond).String()
	if err != nil {
		t.lastErr = err.Error()
	} else {
		t.lastErr = ""
	}
	t.running = false
	t.mu.Unlock()
	return err
}

// Start begins a scheduled task's tick loop: it runs immediately, then every
// spec.Interval, until Stop or the parent context is cancelled. Starting an
// already-started (or non-scheduled) task is a no-op.
func (r *TaskRegistry) Start(parent context.Context, name string) {
	t, ok := r.get(name)
	if !ok || t.spec.Run == nil || t.spec.Interval <= 0 {
		return
	}
	t.mu.Lock()
	if t.cancel != nil {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	t.cancel = cancel
	t.mu.Unlock()

	go func() {
		defer cancel()
		for {
			_ = r.RunOnce(ctx, name)
			select {
			case <-ctx.Done():
				return
			case <-time.After(t.spec.Interval):
			}
		}
	}()
}

// Stop cancels a running scheduled task's loop.
func (r *TaskRegistry) Stop(name string) {
	t, ok := r.get(name)
	if !ok {
		return
	}
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.mu.Unlock()
}

func (r *TaskRegistry) get(name string) (*trackedTask, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byKey[name]
	return t, ok
}

// Snapshot returns a copy of the inventory in registration order, with the
// derived running/scheduled counts.
func (r *TaskRegistry) Snapshot() SystemTasks {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := SystemTasks{Tasks: []TaskInfo{}}
	for _, key := range r.order {
		t := r.byKey[key]
		info := TaskInfo{
			Name:        t.spec.Name,
			Description: t.spec.Description,
			Kind:        t.spec.Kind,
			Enabled:     t.spec.Enabled,
		}
		if t.spec.Interval > 0 {
			info.Interval = t.spec.IntervalLabel
			if info.Interval == "" {
				info.Interval = t.spec.Interval.String()
			}
		}
		t.mu.Lock()
		info.Running = t.running
		info.LastRun = t.lastRun
		info.LastDuration = t.lastDur
		info.LastError = t.lastErr
		t.mu.Unlock()
		out.Tasks = append(out.Tasks, info)
		if info.Running {
			out.Running++
		}
		if info.Kind == TaskScheduled && info.Enabled {
			out.Scheduled++
		}
	}
	return out
}
