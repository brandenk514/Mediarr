package providers

import (
	"context"
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
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		Name        string  `json:"name"`           // populated for /search/tv
		ReleaseDate string  `json:"release_date"`   // movies: "2020-09-03" or ""
		FirstAir    string  `json:"first_air_date"` // tv: "2019-01-11" or ""
		Overview    string  `json:"overview"`
		VoteAverage float64 `json:"vote_average"`
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

	t.Client.Base = t.baseURL()
	var resp tmdbSearchResponse
	if err := t.Client.getJSON(ctx, path, values, &resp); err != nil {
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
