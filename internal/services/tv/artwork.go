package tv

import (
	"context"
	"strconv"

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
// keyless convention: artwork never fails the read). A series without a
// stored TMDB id resolves one via title search first (best year match).
func enrichSeriesArtwork(ctx context.Context, a Artworker, s *dom.Series) {
	if a == nil || s == nil {
		return
	}
	id := ""
	if s.TMDBID != 0 {
		id = strconv.Itoa(s.TMDBID)
	}
	if id == "" {
		results, err := a.SearchTV(ctx, s.Title)
		if err != nil {
			return
		}
		for _, r := range results {
			if s.Year == 0 || r.Year == 0 || r.Year == s.Year {
				id = r.ID
				break
			}
		}
		if id == "" {
			return
		}
	}
	art, err := a.GetSeriesArtwork(ctx, id)
	if err != nil || art == nil {
		return
	}
	// Upstream TMDB URLs (the w342 poster / w1280 backdrop sizes), never
	// proxy URLs. Empty provider paths stay empty (keyless/no-art
	// degradation).
	if art.PosterPath != "" {
		s.PosterURL = providers.TMDBPosterURL(art.PosterPath)
	}
	if art.BackdropPath != "" {
		s.BackdropURL = providers.TMDBBackdropURL(art.BackdropPath)
	}
}
