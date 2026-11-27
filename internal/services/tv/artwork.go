package tv

import (
	"context"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
)

// Artworker is the narrow provider surface the series artwork enrichment
// needs (issue #149). *providers.TMDB satisfies it; tests use a fake.
type Artworker interface {
	// SearchTV resolves a title to TMDB series ids (the TMDBID-less path).
	SearchTV(ctx context.Context, q string) ([]providers.Result, error)
	// GetSeriesArtwork fetches one series' poster/backdrop image paths by
	// TMDB id. An unset API key returns ErrNotConfigured.
	GetSeriesArtwork(ctx context.Context, tmdbID string) (*providers.SeriesArtwork, error)
}

// enrichSeriesArtwork fills s.PosterURL / s.BackdropURL from the provider
// (issue #149). Nil-safe and failure-tolerant: a nil provider, a keyless
// state, or any upstream error leaves the fields as-is ("" — the R3
// keyless convention: artwork never fails the read).
//
//nolint:unused // scaffolding for the #149 fields population run (WO#40 A1): wired into the series read path there; kept so the RED scaffold's provider surface stays reviewable
func enrichSeriesArtwork(ctx context.Context, a Artworker, s *dom.Series) {
}
