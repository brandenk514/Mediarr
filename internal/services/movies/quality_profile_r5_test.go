package movies

import (
	"context"
	"errors"
	"testing"

	"github.com/brandenk514/mediarr/internal/settings"
)

// R5a: AddMovieRequest.QualityProfile is a Radarr v6 movie profile name.
// Unknown names are 400-shaped (ErrInvalidAddParams); empty resolves to the
// factory default ("4K Best Effort"); explicit valid names persist verbatim
// into the movie row's quality_profile column.
func TestR5a_AddMovieQualityProfile(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := New(Deps{Repo: repo})

	// A non-seeded name (the legacy TV-era default) is rejected.
	_, err := svc.AddMovie(ctx, AddMovieRequest{Title: "T", QualityProfile: "HD-1080p"})
	if !errors.Is(err, ErrInvalidAddParams) {
		t.Fatalf("unknown profile: got %v, want ErrInvalidAddParams", err)
	}

	// Empty → factory default, persisted into the movie row.
	id, err := svc.AddMovie(ctx, AddMovieRequest{Title: "A"})
	if err != nil {
		t.Fatalf("add default: %v", err)
	}
	if got := repo.movies[id].QualityProfile; got != "4K Best Effort" {
		t.Errorf("default add persisted %q, want %q", got, "4K Best Effort")
	}

	// Explicit valid name persists verbatim.
	id2, err := svc.AddMovie(ctx, AddMovieRequest{Title: "B", QualityProfile: "1080p"})
	if err != nil {
		t.Fatalf("add 1080p: %v", err)
	}
	if got := repo.movies[id2].QualityProfile; got != "1080p" {
		t.Errorf("explicit add persisted %q, want %q", got, "1080p")
	}

	// Every seeded movie profile name is accepted.
	i := 0
	for name := range settings.MovieProfileNames() {
		i++
		if _, err := svc.AddMovie(ctx, AddMovieRequest{Title: "M" + string(rune('A'+i-1)), QualityProfile: name}); err != nil {
			t.Errorf("seeded profile %q rejected: %v", name, err)
		}
	}
}
