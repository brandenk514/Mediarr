package music

import (
	"context"
	"errors"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the artists list endpoint validates ?sort= server-side
// (unknown field/direction → paging.ErrInvalidSort, the API maps it to
// 400) and orders the full set before paging. The music allowlist is
// title/added/rating (the persisted Lidarr fields).

func TestListArtistsQuery_UnknownSortRejected(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	for _, sort := range []string{"tempo-asc", "rating-sideways"} {
		_, err := svc.ListArtistsQuery(context.Background(), paging.Query{Sort: sort})
		if !errors.Is(err, paging.ErrInvalidSort) {
			t.Errorf("sort %q: err = %v, want paging.ErrInvalidSort", sort, err)
		}
	}
}

func TestListArtistsQuery_SortsByRating(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	repo.artists[1] = dom.Artist{ID: 1, Name: "Low", Rating: 3.5}
	repo.artists[2] = dom.Artist{ID: 2, Name: "High", Rating: 4.9}
	repo.artists[3] = dom.Artist{ID: 3, Name: "Unknown", Rating: 0}

	page, err := svc.ListArtistsQuery(context.Background(), paging.Query{Sort: "rating-desc"})
	if err != nil {
		t.Fatalf("ListArtistsQuery: %v", err)
	}
	var got []string
	for _, it := range page.Items {
		got = append(got, it.Name)
	}
	// Descending: best → worst, unrated (0) last.
	if len(got) != 3 || got[0] != "High" || got[1] != "Low" || got[2] != "Unknown" {
		t.Fatalf("rating-desc = %v, want [High, Low, Unknown]", got)
	}
}
