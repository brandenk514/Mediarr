package providers

import (
	"context"
	"errors"
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
	Runtime          int // minutes; 0 when unknown
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
		Overview          string  `json:"overview"`
		Runtime           int     `json:"runtime"`
		OriginalLanguage  string  `json:"original_language"`
		VoteAverage       float64 `json:"vote_average"`
		ReleaseDate       string  `json:"release_date"`
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
				Iso     string `json:"iso_3166_1"`
				Dates   []struct {
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
