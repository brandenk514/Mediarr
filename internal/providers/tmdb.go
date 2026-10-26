package providers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// defaultTMDBBase is the TMDB v3 REST API.
const defaultTMDBBase = "https://api.themoviedb.org/3"

// TMDB searches movies and series against The Movie Database
// (https://developer.themoviedb.org/docs/search-movie). An API key is
// required (config: TMDB_API_KEY) and sent as the X-API-KEY header.
type TMDB struct {
	Client
	// Base defaults to defaultTMDBBase when empty (tests point it at a stub).
	BaseURL string
}

// NewTMDB builds the adapter. apiKey may be empty, in which case Search
// returns ErrNotConfigured so the API can report 503 "TMDB not configured".
func NewTMDB(apiKey string) *TMDB {
	return &TMDB{Client: Client{Name: "tmdb", AuthHeader: apiKey}}
}

func (t *TMDB) baseURL() string {
	if t.BaseURL != "" {
		return t.BaseURL
	}
	return defaultTMDBBase
}

// Name implements Searcher.
func (t *TMDB) Name() string { return "tmdb" }

type tmdbSearchResponse struct {
	Results []struct {
		ID               int     `json:"id"`
		Title            string  `json:"title"`
		Name             string  `json:"name"`           // populated for /search/tv
		ReleaseDate      string  `json:"release_date"`   // movies: "2020-09-03" or ""
		FirstAir         string  `json:"first_air_date"` // tv: "2019-01-11" or ""
		Overview         string  `json:"overview"`
		VoteAverage      float64 `json:"vote_average"`
		VoteCount        int     `json:"vote_count"`
		PosterPath       string  `json:"poster_path"`
		OriginalLanguage string  `json:"original_language"`
		GenreIDs         []int   `json:"genre_ids"`
	} `json:"results"`
}

// SearchMovies searches TMDB movies for q.
func (t *TMDB) SearchMovies(ctx context.Context, q string) ([]Result, error) {
	return t.search(ctx, "/search/movie", true, q)
}

// SearchTV searches TMDB series for q.
func (t *TMDB) SearchTV(ctx context.Context, q string) ([]Result, error) {
	return t.search(ctx, "/search/tv", false, q)
}

// CastCredit is one credited cast member (name + character).
type CastCredit struct {
	Name string
	Role string
}

// CrewCredit is one crew member (name + job, e.g. "Director").
type CrewCredit struct {
	Name string
	Job  string
}

// MovieDetail is the provider-neutral movie detail payload used for display
// enrichment (Radarr R1). Every field is optional in practice: a partial
// TMDB response leaves the corresponding fields empty, and the UI renders
// em-dashes for them.
type MovieDetail struct {
	TmdbID           string
	Overview         string
	Runtime          int    // minutes; 0 when unknown
	OriginalLanguage string // ISO code ("en"); "" when unknown
	VoteAverage      float64
	ReleaseDate      string // "YYYY-MM-DD"; "" when unknown
	Certification    string // US release rating (e.g. "PG-13"); "" when unknown
	Studio           string // first production company; "" when unknown
	Genres           []string
	CollectionName   string
	Cast             []CastCredit
	Crew             []CrewCredit
}

// GetMovieDetail fetches the TMDB detail for a movie by TMDB id, including
// credits, release dates (for the US certification) and genres via
// append_to_response. An unset API key returns ErrNotConfigured; callers on
// the enrichment path treat every error as "no data" and continue.
func (t *TMDB) GetMovieDetail(ctx context.Context, tmdbID string) (*MovieDetail, error) {
	tmdbID = strings.TrimSpace(tmdbID)
	if tmdbID == "" {
		return nil, errors.New("providers: tmdb id required")
	}
	if t.AuthHeader == "" {
		return nil, ErrNotConfigured
	}

	values := url.Values{}
	values.Set("append_to_response", "credits,release_dates,genres")
	var resp struct {
		Overview            string  `json:"overview"`
		Runtime             int     `json:"runtime"`
		OriginalLanguage    string  `json:"original_language"`
		VoteAverage         float64 `json:"vote_average"`
		ReleaseDate         string  `json:"release_date"`
		BelongsToCollection struct {
			Name string `json:"name"`
		} `json:"belongs_to_collection"`
		ProductionCompanies []struct {
			Name string `json:"name"`
		} `json:"production_companies"`
		Genres []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Credits struct {
			Cast []struct {
				Name      string `json:"name"`
				Character string `json:"character"`
			} `json:"cast"`
			Crew []struct {
				Name string `json:"name"`
				Job  string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
		ReleaseDates struct {
			Results []struct {
				Iso   string `json:"iso_3166_1"`
				Dates []struct {
					Certification string `json:"certification"`
				} `json:"release_dates"`
			} `json:"results"`
		} `json:"release_dates"`
	}

	t.Base = t.baseURL()
	if err := t.getJSON(ctx, "/movie/"+tmdbID, values, &resp); err != nil {
		return nil, err
	}

	d := &MovieDetail{
		TmdbID:           tmdbID,
		Overview:         resp.Overview,
		Runtime:          resp.Runtime,
		OriginalLanguage: resp.OriginalLanguage,
		VoteAverage:      resp.VoteAverage,
		ReleaseDate:      resp.ReleaseDate,
		CollectionName:   resp.BelongsToCollection.Name,
	}
	if len(resp.ProductionCompanies) > 0 {
		d.Studio = resp.ProductionCompanies[0].Name
	}
	for _, g := range resp.Genres {
		if g.Name != "" {
			d.Genres = append(d.Genres, g.Name)
		}
	}
	for _, r := range resp.ReleaseDates.Results {
		if r.Iso == "US" {
			for _, dt := range r.Dates {
				if dt.Certification != "" {
					d.Certification = dt.Certification
					break
				}
			}
			break
		}
	}
	// Keep the top of the bill: 10 cast, 10 crew (the detail page shows
	// these as scrollable lists; beyond that TMDB adds little).
	for i, c := range resp.Credits.Cast {
		if i == 10 {
			break
		}
		if c.Name != "" {
			d.Cast = append(d.Cast, CastCredit{Name: c.Name, Role: c.Character})
		}
	}
	for i, c := range resp.Credits.Crew {
		if i == 10 {
			break
		}
		if c.Name != "" && (c.Job == "Director" || c.Job == "Writer" || c.Job == "Screenplay") {
			d.Crew = append(d.Crew, CrewCredit{Name: c.Name, Job: c.Job})
		}
	}
	return d, nil
}

func (t *TMDB) search(ctx context.Context, path string, isMovie bool, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if t.AuthHeader == "" {
		return nil, ErrNotConfigured
	}
	values := url.Values{}
	values.Set("query", q)
	values.Set("include_adult", "false")

	t.Base = t.baseURL()
	var resp tmdbSearchResponse
	if err := t.getJSON(ctx, path, values, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Results))
	for _, r := range resp.Results {
		title := r.Title
		if !isMovie {
			title = r.Name
		}
		if title == "" {
			continue
		}
		date := r.ReleaseDate
		if !isMovie {
			date = r.FirstAir
		}
		year := 0
		if len(date) >= 4 {
			year, _ = strconv.Atoi(date[:4])
		}
		out = append(out, Result{
			ID:    strconv.Itoa(r.ID),
			Title: title,
			Year:  year,
			Extra: map[string]string{
				"overview": r.Overview,
				"rating":   strconv.FormatFloat(r.VoteAverage, 'f', 1, 64),
			},
		})
	}
	return out, nil
}

// ---- Movie add-flow search (Radarr R2) ----

// tmdbImageBase is where TMDB poster images are hosted.
const tmdbImageBase = "https://image.tmdb.org/t/p/w342"

// movieHitDetailCap bounds how many search hits get a detail (badge)
// fetch — the rest keep search-payload fields only.
const movieHitDetailCap = 10

// MovieSearchHit is one TMDB movie search hit enriched for the movie
// add-flow (Radarr R2). Search-payload fields (poster, year, rating,
// language, genres) are always present (zero when TMDB omitted them); the
// detail badge fields (Runtime, Certification, Studio, ImdbURL) are filled
// for the top hits from a /movie/{id} fetch — a failed detail fetch leaves
// them empty, it never fails the search.
type MovieSearchHit struct {
	ID               string
	Title            string
	Year             int
	Poster           string
	Overview         string
	OriginalLanguage string
	VoteAverage      float64
	VoteCount        int
	Genres           []string
	Runtime          int
	Certification    string
	Studio           string
	ImdbURL          string
}

func (t *TMDB) posterURL(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageBase + path
}

// movieGenreNames fetches the movie genre id→name map (a small static TMDB
// endpoint). Best-effort: on failure it returns an empty map and the hits
// simply lack genre names.
func (t *TMDB) movieGenreNames(ctx context.Context) map[int]string {
	names := map[int]string{}
	var resp struct {
		Genres []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
	}
	if err := t.getJSON(ctx, "/genre/movie/list", nil, &resp); err != nil {
		return names
	}
	for _, g := range resp.Genres {
		names[g.ID] = g.Name
	}
	return names
}

// tmdbMovieResponse is the /movie/{id} payload (genres and release_dates
// appended) used for badge enrichment.
type tmdbMovieResponse struct {
	ID                  int     `json:"id"`
	Title               string  `json:"title"`
	Overview            string  `json:"overview"`
	Runtime             int     `json:"runtime"`
	OriginalLanguage    string  `json:"original_language"`
	VoteAverage         float64 `json:"vote_average"`
	VoteCount           int     `json:"vote_count"`
	ReleaseDate         string  `json:"release_date"`
	PosterPath          string  `json:"poster_path"`
	ImdbID              string  `json:"imdb_id"`
	ProductionCompanies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ReleaseDates struct {
		Results []struct {
			Iso   string `json:"iso_3166_1"`
			Dates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// fetchMovie fetches /movie/{id} with genres and US release dates appended.
func (t *TMDB) fetchMovie(ctx context.Context, tmdbID string) (*tmdbMovieResponse, error) {
	values := url.Values{}
	values.Set("append_to_response", "genres,release_dates")
	var resp tmdbMovieResponse
	if err := t.getJSON(ctx, "/movie/"+tmdbID, values, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// applyMovieBadges folds a detail payload into a hit: badge fields, the
// vote (the detail is authoritative when it actually carries one), a year
// the search did not have, and — when detailGenres — the genre names from
// the detail (overriding the id-mapped ones).
func applyMovieBadges(h *MovieSearchHit, d *tmdbMovieResponse, detailGenres bool) {
	h.Runtime = d.Runtime
	if d.VoteAverage > 0 {
		h.VoteAverage = d.VoteAverage
		h.VoteCount = d.VoteCount
	}
	if h.OriginalLanguage == "" {
		h.OriginalLanguage = d.OriginalLanguage
	}
	if h.Year == 0 && len(d.ReleaseDate) >= 4 {
		h.Year, _ = strconv.Atoi(d.ReleaseDate[:4])
	}
	if len(d.ProductionCompanies) > 0 {
		h.Studio = d.ProductionCompanies[0].Name
	}
	if d.ImdbID != "" {
		h.ImdbURL = "https://www.imdb.com/title/" + d.ImdbID
	}
	for _, r := range d.ReleaseDates.Results {
		if r.Iso == "US" {
			for _, dt := range r.Dates {
				if dt.Certification != "" {
					h.Certification = dt.Certification
					break
				}
			}
			break
		}
	}
	if detailGenres && len(d.Genres) > 0 {
		var names []string
		for _, g := range d.Genres {
			if g.Name != "" {
				names = append(names, g.Name)
			}
		}
		h.Genres = names
	}
}

// SearchMovieHits searches TMDB movies and enriches the top
// movieHitDetailCap hits with detail badge fields. An unset API key
// returns ErrNotConfigured; a bad upstream search answer returns
// ErrBadResponse. A failed detail fetch degrades that hit to empty badge
// fields — never an error.
func (t *TMDB) SearchMovieHits(ctx context.Context, q string) ([]MovieSearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if t.AuthHeader == "" {
		return nil, ErrNotConfigured
	}
	values := url.Values{}
	values.Set("query", q)
	values.Set("include_adult", "false")
	t.Base = t.baseURL()
	var resp tmdbSearchResponse
	if err := t.getJSON(ctx, "/search/movie", values, &resp); err != nil {
		return nil, err
	}
	genres := t.movieGenreNames(ctx)
	hits := make([]MovieSearchHit, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.Title == "" {
			continue
		}
		year := 0
		if len(r.ReleaseDate) >= 4 {
			year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}
		h := MovieSearchHit{
			ID:               strconv.Itoa(r.ID),
			Title:            r.Title,
			Year:             year,
			Poster:           t.posterURL(r.PosterPath),
			Overview:         r.Overview,
			OriginalLanguage: r.OriginalLanguage,
			VoteAverage:      r.VoteAverage,
			VoteCount:        r.VoteCount,
		}
		for _, g := range r.GenreIDs {
			if name, ok := genres[g]; ok {
				h.Genres = append(h.Genres, name)
			}
		}
		hits = append(hits, h)
	}
	for i := 0; i < len(hits) && i < movieHitDetailCap; i++ {
		d, err := t.fetchMovie(ctx, hits[i].ID)
		if err != nil {
			continue // detail failure degrades the hit, not the search
		}
		applyMovieBadges(&hits[i], d, true)
	}
	return hits, nil
}

// FindMovieHit fetches one movie by TMDB id (the add-flow tmdb:<id> query
// form). An unset API key returns ErrNotConfigured; an id TMDB does not
// know returns an ErrBadResponse-wrapped error.
func (t *TMDB) FindMovieHit(ctx context.Context, tmdbID string) (*MovieSearchHit, error) {
	tmdbID = strings.TrimSpace(tmdbID)
	if tmdbID == "" {
		return nil, errors.New("providers: tmdb id required")
	}
	if t.AuthHeader == "" {
		return nil, ErrNotConfigured
	}
	t.Base = t.baseURL()
	d, err := t.fetchMovie(ctx, tmdbID)
	if err != nil {
		return nil, err
	}
	if d.Title == "" {
		return nil, fmt.Errorf("%w: tmdb movie %s not found", ErrBadResponse, tmdbID)
	}
	h := MovieSearchHit{
		ID:               tmdbID,
		Title:            d.Title,
		Poster:           t.posterURL(d.PosterPath),
		Overview:         d.Overview,
		OriginalLanguage: d.OriginalLanguage,
	}
	applyMovieBadges(&h, d, true)
	return &h, nil
}
