package providers

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// defaultMusicBrainzBase is the MusicBrainz server API.
const defaultMusicBrainzBase = "https://musicbrainz.org/ws/2"

// defaultMusicBrainzUserAgent identifies mediarr to MusicBrainz, which
// requires a contact-bearing User-Agent (server guidelines).
const defaultMusicBrainzUserAgent = "mediarr/1.0 (https://github.com/brandenk514/Mediarr)"

// MusicBrainz searches artists against the MusicBrainz server
// (https://musicbrainz.org/meta/). It is keyless. Results carry the artist
// MBID, which the add flow stores on the artist row.
type MusicBrainz struct {
	Client
	// Base defaults to defaultMusicBrainzBase when empty (tests point it at
	// a stub).
	BaseURL string
}

// NewMusicBrainz builds the adapter.
func NewMusicBrainz() *MusicBrainz {
	return &MusicBrainz{Client: Client{Name: "musicbrainz", UserAgent: defaultMusicBrainzUserAgent}}
}

func (m *MusicBrainz) baseURL() string {
	if m.BaseURL != "" {
		return m.BaseURL
	}
	return defaultMusicBrainzBase
}

// Name implements Searcher.
func (m *MusicBrainz) Name() string { return "musicbrainz" }

type mbResponse struct {
	Artists []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Country string `json:"country"`
		Begins  string `json:"begin-date"` // "1970-06-14" or ""
	} `json:"artists"`
}

// Search searches MusicBrainz artists for q.
func (m *MusicBrainz) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	values := url.Values{}
	values.Set("query", "artist:"+q)
	values.Set("fmt", "json")

	m.Client.Base = m.baseURL()
	var resp mbResponse
	if err := m.Client.getJSON(ctx, "/artist", values, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		if a.Name == "" {
			continue
		}
		year := 0
		if len(a.Begins) >= 4 {
			if y, err := strconv.Atoi(a.Begins[:4]); err == nil {
				year = y
			}
		}
		out = append(out, Result{
			ID:    a.ID,
			Title: a.Name,
			Year:  year,
			Extra: map[string]string{"country": a.Country},
		})
	}
	return out, nil
}
