package tv

import (
	"context"
	"reflect"
	"strconv"
	"time"

	dom "github.com/brandenk514/mediarr/internal/domains/tv"
	"github.com/brandenk514/mediarr/internal/providers"
)

// SeriesDetailer is the narrow provider surface the series metadata
// enrichment needs (issue #157). *providers.TMDB satisfies it; tests use
// a fake.
type SeriesDetailer interface {
	// SearchTV resolves a title to TMDB series ids (the TMDBID-less
	// path).
	SearchTV(ctx context.Context, q string) ([]providers.Result, error)
	// GetSeriesDetail fetches one series' TMDB detail payload by TMDB id
	// (overview, genres, keywords, score, credits, network, first air
	// date, status, language, production countries, trailer, artwork
	// paths). An unset API key returns ErrNotConfigured.
	GetSeriesDetail(ctx context.Context, tmdbID string) (*providers.SeriesDetail, error)
}

// enrichSeriesDetail fills the series' enrichment wire fields (issue
// #157) from one TMDB detail fetch — including the artwork URLs that
// ride in the same payload (the movie-enrichment precedent: one detail
// call per detail read). Nil-safe and failure-tolerant: a nil Detailer
// (keyless), a missing payload, or any upstream error leaves the fields
// as they are — the R3 keyless convention: enrichment never fails the
// read, and never produces "not configured" text. A series without a
// stored TMDB id resolves one via title search first (best year match) —
// the #149 TVDB-only fallback.
func enrichSeriesDetail(ctx context.Context, d SeriesDetailer, s *dom.Series) {
	if d == nil || s == nil {
		return
	}
	id := ""
	if s.TMDBID != 0 {
		id = strconv.Itoa(s.TMDBID)
	}
	if id == "" {
		results, err := d.SearchTV(ctx, s.Title)
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
	detail, err := d.GetSeriesDetail(ctx, id)
	if err != nil || detail == nil {
		return
	}
	applySeriesDetail(s, detail)
}

// applySeriesDetail maps the provider payload onto the series'
// enrichment fields. It fills a field only when the provider reports a
// value, so a partial payload never clobbers a persisted value (partial
// degradation). Artwork URLs are upstream TMDB CDN URLs (w342 poster /
// w1280 backdrop), never proxy URLs.
func applySeriesDetail(s *dom.Series, d *providers.SeriesDetail) {
	if d.PosterPath != "" {
		s.PosterURL = providers.TMDBPosterURL(d.PosterPath)
	}
	if d.BackdropPath != "" {
		s.BackdropURL = providers.TMDBBackdropURL(d.BackdropPath)
	}
	if d.Overview != "" {
		s.Overview = &d.Overview
	}
	if d.Genres != nil {
		s.Genres = d.Genres
	}
	if d.Keywords != nil {
		s.Keywords = d.Keywords
	}
	if d.Status != "" {
		s.Status = &d.Status
	}
	if d.OriginalLanguage != "" {
		s.OriginalLanguage = &d.OriginalLanguage
	}
	if d.Network != "" {
		s.Network = &d.Network
	}
	// first_air_date is always present in the /tv/{id} payload; keep it
	// only when it is a full, parseable date (the movie
	// NormalizeReleaseDate rule) — otherwise it is unknown.
	s.FirstAirDate = normalizeSeriesDate(d.FirstAirDate)
	if d.ProductionCountries != nil {
		s.ProductionCountries = d.ProductionCountries
	}
	if d.ImdbID != "" {
		s.ImdbID = d.ImdbID
	}
	if d.TraktID != "" {
		s.TraktID = d.TraktID
	}
	if d.TrailerURL != "" {
		s.TrailerURL = d.TrailerURL
	}
	// TMDB 0-10 vote_average -> 0-100 percentage (movie convention).
	if d.VoteAverage > 0 {
		pct := d.VoteAverage * 10
		s.Scores = &dom.SeriesScores{TmdbPct: &pct}
	}
	if d.Cast != nil {
		s.Cast = make([]dom.SeriesCast, 0, len(d.Cast))
		for _, c := range d.Cast {
			s.Cast = append(s.Cast, dom.SeriesCast{Name: c.Name, Role: c.Role})
		}
	}
	if d.Crew != nil {
		s.Crew = make([]dom.SeriesCrew, 0, len(d.Crew))
		for _, c := range d.Crew {
			s.Crew = append(s.Crew, dom.SeriesCrew{Name: c.Name, Job: c.Job})
		}
	}
}

// normalizeSeriesDate keeps a provider date only when it is a full,
// parseable "YYYY-MM-DD"; a malformed value becomes "" (unknown).
func normalizeSeriesDate(v string) string {
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return ""
	}
	return v
}

// normalizeSeriesArrays guarantees the enrichment array fields serialize
// as [] on the wire, never null (the docs/openapi.md contract; Radarr
// parity). Applied on every read (detail and list), keyless included.
func normalizeSeriesArrays(s *dom.Series) {
	if s == nil {
		return
	}
	if s.Genres == nil {
		s.Genres = []string{}
	}
	if s.Keywords == nil {
		s.Keywords = []string{}
	}
	if s.ProductionCountries == nil {
		s.ProductionCountries = []string{}
	}
	if s.Cast == nil {
		s.Cast = []dom.SeriesCast{}
	}
	if s.Crew == nil {
		s.Crew = []dom.SeriesCrew{}
	}
}

// seriesMetadataOf snapshots the #157 persisted-metadata fields of a
// series as the flat zero-is-unknown shape the repository stores.
// Pointer-typed enrichment fields collapse to their zero values when
// unknown.
func seriesMetadataOf(s dom.Series) dom.SeriesMetadata {
	meta := dom.SeriesMetadata{
		Genres:              s.Genres,
		Keywords:            s.Keywords,
		FirstAirDate:        s.FirstAirDate,
		ProductionCountries: s.ProductionCountries,
	}
	if s.OriginalLanguage != nil {
		meta.OriginalLanguage = *s.OriginalLanguage
	}
	if s.Status != nil {
		meta.Status = *s.Status
	}
	if s.Network != nil {
		meta.Network = *s.Network
	}
	if s.Scores != nil && s.Scores.TmdbPct != nil {
		meta.TmdbPct = *s.Scores.TmdbPct
	}
	return meta
}

// seriesMetadataEqual reports whether two persisted-metadata snapshots
// carry the same values — the persist guard that keeps unchanged detail
// reads from re-writing the metadata row (the movie
// movieMetadataEqual).
func seriesMetadataEqual(a, b dom.SeriesMetadata) bool {
	return reflect.DeepEqual(a, b)
}
