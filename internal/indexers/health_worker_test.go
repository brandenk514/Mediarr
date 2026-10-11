package indexers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeIndexerRepo is an in-memory IndexerRepo for worker tests. It records
// SetLastTest calls so tests can assert the worker persisted probe times.
type fakeIndexerRepo struct {
	mu       sync.Mutex
	defs     []Definition
	lastTest map[int64]*time.Time
}

func newFakeIndexerRepo() *fakeIndexerRepo {
	return &fakeIndexerRepo{lastTest: map[int64]*time.Time{}}
}

func (r *fakeIndexerRepo) Create(ctx context.Context, def Definition) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	def.ID = int64(len(r.defs) + 1)
	r.defs = append(r.defs, def)
	return def.ID, nil
}
func (r *fakeIndexerRepo) Get(ctx context.Context, id int64) (*Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.defs {
		if d.ID == id {
			cp := d
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}
func (r *fakeIndexerRepo) GetByName(ctx context.Context, name string) (*Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.defs {
		if d.Name == name {
			cp := d
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}
func (r *fakeIndexerRepo) List(ctx context.Context) ([]Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Definition(nil), r.defs...), nil
}
func (r *fakeIndexerRepo) ListEnabled(ctx context.Context) ([]Definition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Definition
	for _, d := range r.defs {
		if d.Enabled {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *fakeIndexerRepo) Update(ctx context.Context, def Definition) error { return nil }
func (r *fakeIndexerRepo) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == id {
			r.defs = append(r.defs[:i], r.defs[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}
func (r *fakeIndexerRepo) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	return nil
}
func (r *fakeIndexerRepo) SetLastTest(ctx context.Context, id int64, at *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTest[id] = at
	return nil
}

// TestHealthWorker_TestAllRecordsAndPersists is the #34 core test: a sweep
// probes every enabled indexer, records each outcome to the shared tracker,
// and persists last_test. A disabled indexer is excluded from the sweep.
func TestHealthWorker_TestAllRecordsAndPersists(t *testing.T) {
	repo := newFakeIndexerRepo()
	_, _ = repo.Create(context.Background(), Definition{Name: "ok", Kind: "fake", Enabled: true})
	_, _ = repo.Create(context.Background(), Definition{Name: "off", Kind: "fake", Enabled: false})

	tracker := NewHealthTracker()
	reg := NewDefaultRegistry()
	// The shared deps carry the tracker so the probe records into it.
	deps := AdapterDeps{Health: tracker}
	worker := NewHealthWorker(repo, reg, deps, 0, nil)

	if err := worker.TestAll(context.Background()); err != nil {
		t.Fatalf("TestAll: %v", err)
	}

	// The enabled "ok" indexer was probed and recorded as a success.
	s := tracker.Stats("ok")
	if s.Successes != 1 || s.Calls != 1 {
		t.Fatalf("expected ok recorded once as success, got %+v", s)
	}
	// The disabled "off" indexer was not swept.
	if g := tracker.Stats("off"); g.Calls != 0 {
		t.Errorf("disabled indexer should not be swept, got %+v", g)
	}
	// last_test persisted for the probed indexer.
	repo.mu.Lock()
	if repo.lastTest[1] == nil {
		t.Error("expected last_test persisted for indexer id 1")
	}
	repo.mu.Unlock()
}

// TestHealthWorker_TestOneOnDisabledDiagnoses confirms on-demand probing works
// for a disabled indexer (#35 POST .../test must let you diagnose a turned-off
// indexer).
func TestHealthWorker_TestOneOnDisabledDiagnoses(t *testing.T) {
	repo := newFakeIndexerRepo()
	id, _ := repo.Create(context.Background(), Definition{Name: "off", Kind: "fake", Enabled: false})
	tracker := NewHealthTracker()
	deps := AdapterDeps{Health: tracker}
	worker := NewHealthWorker(repo, NewDefaultRegistry(), deps, 0, nil)

	res, err := worker.TestOne(context.Background(), id)
	if err != nil {
		t.Fatalf("TestOne: %v", err)
	}
	if !res.OK {
		t.Errorf("expected ok probe, got %+v", res)
	}
	if s := tracker.Stats("off"); s.Successes != 1 {
		t.Errorf("expected on-demand probe recorded, got %+v", s)
	}
}

// TestHealthWorker_ProbeFailureIsCapturedNotPropagated drives a real failing
// probe (a Torznab against a 401 server) and confirms the worker captures the
// failure in the result + tracker rather than returning an error.
func TestHealthWorker_ProbeFailureIsCapturedNotPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	repo := newFakeIndexerRepo()
	id, _ := repo.Create(context.Background(), Definition{
		Name: "broken", Kind: "torznab", BaseURL: srv.URL, Enabled: true,
	})
	tracker := NewHealthTracker()
	deps := AdapterDeps{Health: tracker}
	worker := NewHealthWorker(repo, NewDefaultRegistry(), deps, 0, nil)

	res, err := worker.TestOne(context.Background(), id)
	if err != nil {
		t.Fatalf("a failing probe must not error, got: %v", err)
	}
	if res.OK {
		t.Error("expected ok=false for a 401 indexer")
	}
	if res.Error == "" {
		t.Error("expected a non-empty credential-free error")
	}
	if s := tracker.Stats("broken"); s.Failures != 1 {
		t.Errorf("expected failure recorded, got %+v", s)
	}
}

// TestHealthWorker_RunZeroIntervalDoesSingleSweep confirms a non-positive
// interval performs the initial sweep and returns (no blocking loop).
func TestHealthWorker_RunZeroIntervalDoesSingleSweep(t *testing.T) {
	repo := newFakeIndexerRepo()
	_, _ = repo.Create(context.Background(), Definition{Name: "a", Kind: "fake", Enabled: true})
	tracker := NewHealthTracker()
	deps := AdapterDeps{Health: tracker}
	worker := NewHealthWorker(repo, NewDefaultRegistry(), deps, 0, nil)

	done := make(chan struct{})
	go func() {
		_ = worker.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run with zero interval should return after one sweep")
	}
	if s := tracker.Stats("a"); s.Calls != 1 {
		t.Errorf("expected exactly one sweep, got %+v", s)
	}
}

// TestHealthWorker_RunTicksUntilCancelled confirms the periodic loop fires
// multiple times and stops on context cancellation.
func TestHealthWorker_RunTicksUntilCancelled(t *testing.T) {
	repo := newFakeIndexerRepo()
	_, _ = repo.Create(context.Background(), Definition{Name: "a", Kind: "fake", Enabled: true})
	tracker := NewHealthTracker()
	deps := AdapterDeps{Health: tracker}
	worker := NewHealthWorker(repo, NewDefaultRegistry(), deps, 10*time.Millisecond, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s := tracker.Stats("a"); s.Calls < 2 {
		t.Errorf("expected multiple sweeps, got %d", s.Calls)
	}
}
