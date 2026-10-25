package blocklist

import (
	"context"
	"errors"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/blocklist"
)

type fakeRepo struct {
	entries []dom.Entry
	next    int64
	err     error
}

func (f *fakeRepo) List(_ context.Context, flt dom.Filter) ([]dom.Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return flt.Apply(f.entries), nil
}
func (f *fakeRepo) Add(_ context.Context, e dom.Entry) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.next++
	e.ID = f.next
	f.entries = append(f.entries, e)
	return e.ID, nil
}
func (f *fakeRepo) Unblock(_ context.Context, id int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	for i, e := range f.entries {
		if e.ID == id {
			f.entries = append(f.entries[:i], f.entries[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func TestAddValidatesAndPersists(t *testing.T) {
	s := New(&fakeRepo{next: 10})
	if _, err := s.Add(context.Background(), dom.Entry{Kind: "nope"}); !errors.Is(err, dom.ErrInvalidKind) {
		t.Fatalf("bad kind: got %v, want ErrInvalidKind", err)
	}
	if _, err := s.Add(context.Background(), dom.Entry{Kind: dom.KindTV, ReleaseTitle: "  "}); !errors.Is(err, dom.ErrEmptyTitle) {
		t.Fatalf("empty title: got %v, want ErrEmptyTitle", err)
	}
	id, err := s.Add(context.Background(), dom.Entry{
		Kind: dom.KindTV, ParentID: 7, ReleaseTitle: "  Severance.S01E01  ", Indexer: " nyaa ", Reason: " wrong region ",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id != 11 {
		t.Fatalf("id = %d, want 11", id)
	}
}

func TestAddTrimsFields(t *testing.T) {
	repo := &fakeRepo{next: 1}
	s := New(repo)
	_, _ = s.Add(context.Background(), dom.Entry{
		Kind: dom.KindMovie, ParentID: 2, ReleaseTitle: "  Dune.2160p  ", Indexer: " nyaa ", Reason: "  reason ",
	})
	if len(repo.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(repo.entries))
	}
	e := repo.entries[0]
	if e.ReleaseTitle != "Dune.2160p" || e.Indexer != "nyaa" || e.Reason != "reason" {
		t.Fatalf("not trimmed: %+v", e)
	}
}

func TestListAppliesFilter(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{entries: []dom.Entry{
		{ID: 1, Kind: dom.KindTV, ParentID: 1, ReleaseTitle: "A", CreatedAt: now},
		{ID: 2, Kind: dom.KindMovie, ParentID: 2, ReleaseTitle: "B", CreatedAt: now},
	}}
	s := New(repo)
	got, err := s.List(context.Background(), dom.Filter{Kind: dom.KindTV})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("filter: got %v", got)
	}
}

func TestUnblock(t *testing.T) {
	repo := &fakeRepo{next: 5, entries: []dom.Entry{{ID: 5, Kind: dom.KindTV}}}
	s := New(repo)
	if ok, err := s.Unblock(context.Background(), 5); err != nil || !ok {
		t.Fatalf("unblock 5: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Unblock(context.Background(), 5); err != nil || ok {
		t.Fatalf("unblock again: ok=%v err=%v (want false, nil)", ok, err)
	}
	if _, err := s.Unblock(context.Background(), 0); !errors.Is(err, dom.ErrInvalidID) {
		t.Fatalf("unblock 0: got %v, want ErrInvalidID", err)
	}
}
