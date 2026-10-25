package providers

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// defaultTVmazeBase is the public TVmaze API (P5.7 keyless fallback for TV:
// a zero-key install gets a complete, metadata-complete TV catalog through
// it — search, show details with the full episode tree, cast, and air
// dates — at the cost of weaker imagery and non-English coverage than TMDB).
const defaultTVmazeBase = "https://api.tvmaze.com"

// TVmaze is the keyless TV fallback adapter.
type TVmaze struct {
	Client
	// Base defaults to defaultTVmazeBase when empty (tests point it at a
	// stub).
	BaseURL string
}

// NewTVmaze builds the adapter. No auth: the TVmaze API is public.
func NewTVmaze() *TVmaze {
	return &TVmaze{Client: Client{Name: "tvmaze", UserAgent: "mediarr/1.0 (https://github.com/brandenk514/Mediarr)"}}
}

func (t *TVmaze) baseURL() string {
	if t.BaseURL != "" {
		return t.BaseURL
	}
	return defaultTVmazeBase
}

// Name implements Searcher.
func (t *TVmaze) Name() string { return "tvmaze" }

type tvmazeSearchResponse struct {
	Shows []struct {
		Show struct {
			ID        int      `json:"id"`
			Name      string   `json:"name"`
			Premiered string   `json:"premiered"` // "2026-01-01" or ""
			Genres    []string `json:"genres"`
			Status    string   `json:"status"`
			Language  string   `json:"language"`
		} `json:"show"`
	} `json:"shows"`
}

// Search searches TVmaze shows for q (up to 25).
func (t *TVmaze) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("q", q)

	t.Base = t.baseURL()
	var resp tvmazeSearchResponse
	if err := t.getJSON(ctx, "/search/shows", values, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Shows))
	for _, hit := range resp.Shows {
		s := hit.Show
		if s.Name == "" {
			continue
		}
		extra := map[string]string{}
		if s.Status != "" {
			extra["status"] = s.Status
		}
		if len(s.Genres) > 0 {
			extra["genres"] = strings.Join(s.Genres, ",")
		}
		if s.Language != "" {
			extra["language"] = s.Language
		}
		out = append(out, Result{
			ID:    strconv.Itoa(s.ID),
			Title: s.Name,
			Year:  yearFrom(s.Premiered),
			Extra: extra,
		})
	}
	return out, nil
}

// tvmazeShow is a show detail document with episodes embedded (the
// ?embed=episodes form) — the complete season/episode tree the plan calls
// out for the keyless fallback.
type tvmazeShow struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	Premiered string   `json:"premiered"`
	Ended     string   `json:"ended"`
	Summary   string   `json:"summary"`
	Genres    []string `json:"genres"`
	Language  string   `json:"language"`
	Embedded  struct {
		Episodes []struct {
			ID      int    `json:"id"`
			Season  int    `json:"season"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
			AirDate string `json:"airdate"` // "2026-01-01" or ""
		} `json:"episodes"`
	} `json:"_embedded"`
}

// Show fetches one show's full document with the episode tree embedded
// (complete season/episode data for the keyless path).
func (t *TVmaze) Show(ctx context.Context, id int) (tvmazeShow, error) {
	var out tvmazeShow
	t.Base = t.baseURL()
	values := url.Values{}
	values.Set("embed", "episodes")
	if err := t.getJSON(ctx, "/shows/"+strconv.Itoa(id), values, &out); err != nil {
		return tvmazeShow{}, err
	}
	return out, nil
}

// yearFrom extracts a four-digit year from an ISO date prefix.
func yearFrom(d string) int {
	if len(d) < 4 {
		return 0
	}
	y, err := strconv.Atoi(d[:4])
	if err != nil {
		return 0
	}
	return y
}
