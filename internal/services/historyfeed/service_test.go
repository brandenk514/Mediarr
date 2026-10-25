package historyfeed

import (
	"context"
	"testing"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/historyfeed"
)

type fakeRepo struct {
	items []dom.Item
	got   dom.Filter
}

func (f *fakeRepo) List(_ context.Context, flt dom.Filter) ([]dom.Item, error) {
	f.got = flt
	// Behave like the SQL repo: filter in memory.
	return flt.Apply(f.items), nil
}

func TestListNormalizesFilter(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{items: []dom.Item{
		{Kind: "tv", ParentID: 1, Event: "added", At: now},
		{Kind: "tv", ParentID: 1, Event: "imported", At: now.Add(time.Hour)},
	}}
	s := New(repo)
	got, err := s.List(context.Background(), dom.Filter{Event: " ADDED "})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Event != "added" {
		t.Fatalf("event filter (case-insensitive + trimmed): got %v", got)
	}
	if repo.got.Limit != dom.DefaultLimit {
		t.Fatalf("limit not defaulted: %d", repo.got.Limit)
	}
	if repo.got.Event != "ADDED" {
		t.Fatalf("event not trimmed: %q", repo.got.Event)
	}
}

func TestListClampsLimit(t *testing.T) {
	s := New(&fakeRepo{})
	_, _ = s.List(context.Background(), dom.Filter{Limit: 9999})
	if s.repo.(*fakeRepo).got.Limit != dom.MaxLimit {
		t.Fatalf("limit = %d, want %d", s.repo.(*fakeRepo).got.Limit, dom.MaxLimit)
	}
}
