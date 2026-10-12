package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// defaultTVDBBase is the TVDB v4 REST API.
const defaultTVDBBase = "https://api4.thetvdb.com/v4"

// TVDB searches series against The TVDB (https://thetvdb.com/api-documentation/).
// An API key is required (config: TVDB_API_KEY). The v4 API is a two-step
// exchange: POST /login with the X-Api-Key header returns a short-lived JWT,
// which is then sent as "Authorization: Bearer <jwt>" on the search call.
type TVDB struct {
	Client
	// Base defaults to defaultTVDBBase when empty (tests point it at a stub).
	BaseURL string
	apiKey  string
}

// NewTVDB builds the adapter. apiKey may be empty, in which case Search
// returns ErrNotConfigured so the API can report 503 "TVDB not configured".
func NewTVDB(apiKey string) *TVDB {
	return &TVDB{Client: Client{Name: "tvdb"}, apiKey: apiKey}
}

func (t *TVDB) baseURL() string {
	if t.BaseURL != "" {
		return t.BaseURL
	}
	return defaultTVDBBase
}

// Name implements Searcher.
func (t *TVDB) Name() string { return "tvdb" }

type tvdbLoginResponse struct {
	Token string `json:"token"`
}

// login exchanges the API key for a short-lived JWT.
func (t *TVDB) login(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL()+"/login", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Api-Key", t.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: login: %v", ErrBadResponse, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: login returned %d", ErrBadResponse, resp.StatusCode)
	}
	var lr tvdbLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("%w: login: invalid json: %v", ErrBadResponse, err)
	}
	if lr.Token == "" {
		return "", fmt.Errorf("%w: login returned no token", ErrBadResponse)
	}
	return lr.Token, nil
}

type tvdbSearchResponse struct {
	Data []struct {
		ID         int    `json:"id"`
		SeriesName string `json:"seriesName"`
		FirstAired string `json:"firstAired"` // "2011-04-17" or ""
		Overview   string `json:"overview"`
	} `json:"data"`
}

// Search searches TVDB series for q (login + search in one call).
func (t *TVDB) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if t.apiKey == "" {
		return nil, ErrNotConfigured
	}
	jwt, err := t.login(ctx)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("query", q)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.baseURL()+"/search/series", nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = values.Encode()
	req.Header.Set("Authorization", "Bearer "+jwt)
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: search: %v", ErrBadResponse, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: search returned %d", ErrBadResponse, resp.StatusCode)
	}
	var sr tvdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("%w: search: invalid json: %v", ErrBadResponse, err)
	}
	out := make([]Result, 0, len(sr.Data))
	for _, r := range sr.Data {
		if r.SeriesName == "" {
			continue
		}
		year := 0
		if len(r.FirstAired) >= 4 {
			year, _ = strconv.Atoi(r.FirstAired[:4])
		}
		out = append(out, Result{
			ID:    strconv.Itoa(r.ID),
			Title: r.SeriesName,
			Year:  year,
			Extra: map[string]string{"overview": r.Overview},
		})
	}
	return out, nil
}
