package music

import (
	"context"
	"testing"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	"github.com/brandenk514/mediarr/internal/paging"
)

// Issue #150: the music filter drawer's facets (the persisted Lidarr
// artist fields): genre and status (continuing / ended / inactive).

func artistNames(t *testing.T, svc *Service, f ArtistFilters) []string {
	t.Helper()
	page, err := svc.ListArtistsQuery(context.Background(), paging.Query{Page: 1, PageSize: 100}, f)
	if err != nil {
		t.Fatalf("ListArtistsQuery: %v", err)
	}
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, it.Name)
	}
	return out
}

func TestListArtistsQuery_FiltersByGenre(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	repo.artists[1] = dom.Artist{ID: 1, Name: "Metal Band", Genre: "Metal"}
	repo.artists[2] = dom.Artist{ID: 2, Name: "Jazz Trio", Genre: "Jazz"}
	repo.artists[3] = dom.Artist{ID: 3, Name: "No Genre"}

	got := artistNames(t, svc, ArtistFilters{Genre: "metal"})
	if len(got) != 1 || got[0] != "Metal Band" {
		t.Fatalf("genre=metal = %v, want [Metal Band]", got)
	}
}

func TestListArtistsQuery_FiltersByStatus(t *testing.T) {
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})
	repo.artists[1] = dom.Artist{ID: 1, Name: "Active", Status: "continuing", Genre: "Metal"}
	repo.artists[2] = dom.Artist{ID: 2, Name: "Done", Status: "ended"}
	repo.artists[3] = dom.Artist{ID: 3, Name: "Quiet", Status: "inactive"}

	got := artistNames(t, svc, ArtistFilters{Status: "ended"})
	if len(got) != 1 || got[0] != "Done" {
		t.Fatalf("status=ended = %v, want [Done]", got)
	}
	// Combined facets AND together.
	got = artistNames(t, svc, ArtistFilters{Status: "continuing", Genre: "metal"})
	if len(got) != 1 || got[0] != "Active" {
		t.Fatalf("status+genre = %v, want [Active]", got)
	}
}
