package providers

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Lidarr 3.x Add Artist page enrichment (L2): the search endpoint returns
// enriched artist hits plus release-group hits, and a bare MusicBrainz id
// (optionally with the client-side `lidarr:` prefix) resolves directly to
// its artist.
//
// Rate-limit discipline (keyless MusicBrainz, ~1 req/s): one search request
// per endpoint call, at most mbDetailEnrichLimit best-effort detail fetches
// per artist search, and one detail fetch for a direct id lookup. A failed
// detail fetch degrades to empty badge fields (the spec's "empty when
// unknown") instead of failing the whole search.

// mbDetailEnrichLimit bounds the per-hit detail fetches (genres/links) on an
// artist search: the first N hits get enriched, the rest ship with empty
// genres/links.
const mbDetailEnrichLimit = 3

// mbidRe is a canonical 36-character UUID (MusicBrainz ids).
var mbidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ArtistSearchHit is one enriched artist hit for the Add Artist page (L2).
// Score stays 0: MusicBrainz exposes no popularity number (the add page must
// not fabricate ratings), so the UI hides the heart. AlreadyAdded is computed
// by the API layer against the library — never by the provider.
type ArtistSearchHit struct {
	ID             string // MusicBrainz artist id (mbid)
	Title          string
	Type           string   // "Group" / "Person"; "" = unknown
	Score          float64  // 0-100; 0 = unknown
	Country        string   // "" = unknown
	Disambiguation string   // "" = unknown
	Genres         []string // empty = unknown
	Links          []string // external URLs; empty = unknown
	AlreadyAdded   bool     // set by the API layer
}

// ReleaseGroupHit is one MusicBrainz release-group hit for the Add Artist
// page (L2). Year 0 / ReleaseCount 0 = unknown, Artist "" = unknown.
type ReleaseGroupHit struct {
	ID           string // MusicBrainz release-group id (mbid)
	Title        string
	Year         int
	ReleaseCount int
	Type         string // album/single/ep/compilation/live/remix/broadcast/demo/other/djmix
	Score        float64
	Artist       string
}

// extractMbid returns the MusicBrainz id when q is a bare 36-char UUID or the
// client-side `lidarr:<uuid>` form; "" otherwise (q is a name query).
func extractMbid(q string) string {
	s := strings.TrimSpace(q)
	const prefix = "lidarr:"
	if len(s) > len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		s = strings.TrimSpace(s[len(prefix):])
	}
	if mbidRe.MatchString(s) {
		return s
	}
	return ""
}

// mbArtistSearchResponse is the ws/2 /artist search payload (the subset we
// use; the search response carries type/country/disambiguation directly).
type mbArtistSearchResponse struct {
	Artists []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		Country        string `json:"country"`
		Disambiguation string `json:"disambiguation"`
	} `json:"artists"`
}

// mbArtistRelation is one entry of the artist detail `relations` array.
// URL relations (target-type "url") carry the external link in
// url.resource.
type mbArtistRelation struct {
	TargetType string `json:"target-type"`
	Type       string `json:"type"`
	URL        struct {
		Resource string `json:"resource"`
	} `json:"url"`
}

// mbArtistDetail is the ws/2 /artist/{id} payload with
// inc=genres+url-rels.
type mbArtistDetail struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Country        string `json:"country"`
	Disambiguation string `json:"disambiguation"`
	Status         string `json:"status"` // continuing / ended / inactive; "" = unknown
	Genres         []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Relations []mbArtistRelation `json:"relations"`
}

// mbReleaseGroupSearchResponse is the ws/2 /release-group search payload.
// The live API names the group's title `title`, the type `primary-type`,
// and carries `first-release-date` (a date prefix, e.g. "1998") and `count`
// (number of releases in the group).
type mbReleaseGroupSearchResponse struct {
	ReleaseGroups []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first-release-date"`
		PrimaryType      string `json:"primary-type"`
		Count            int    `json:"count"`
		ArtistCredit     []struct {
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

// SearchArtistHits searches MusicBrainz artists for q and enriches the first
// few hits with genres/external links from the artist detail endpoint. A
// bare MusicBrainz id (or `lidarr:<id>`) resolves directly to its artist.
// An empty result set (no match) is not an error.
func (m *MusicBrainz) SearchArtistHits(ctx context.Context, q string) ([]ArtistSearchHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []ArtistSearchHit{}, nil
	}
	m.Base = m.baseURL()

	if id := extractMbid(q); id != "" {
		return m.lookupArtistHit(ctx, id)
	}

	values := url.Values{}
	values.Set("query", "artist:"+q)
	values.Set("fmt", "json")
	var resp mbArtistSearchResponse
	if err := m.getJSON(ctx, "/artist", values, &resp); err != nil {
		return nil, err
	}

	hits := make([]ArtistSearchHit, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		if a.Name == "" {
			continue
		}
		hits = append(hits, ArtistSearchHit{
			ID:             a.ID,
			Title:          a.Name,
			Type:           a.Type,
			Country:        a.Country,
			Disambiguation: a.Disambiguation,
			Genres:         []string{},
			Links:          []string{},
		})
	}
	// Best-effort enrichment of the top hits (genres + external links).
	for i := range hits {
		if i >= mbDetailEnrichLimit {
			break
		}
		m.enrichArtistHit(ctx, &hits[i])
	}
	return hits, nil
}

// lookupArtistHit resolves a MusicBrainz artist id to a single fully enriched
// hit (one detail request — the lookup itself).
func (m *MusicBrainz) lookupArtistHit(ctx context.Context, id string) ([]ArtistSearchHit, error) {
	values := url.Values{}
	values.Set("inc", "genres+url-rels")
	var d mbArtistDetail
	if err := m.getJSON(ctx, "/artist/"+id, values, &d); err != nil {
		return nil, err
	}
	if d.ID == "" && d.Name == "" {
		return []ArtistSearchHit{}, nil
	}
	return []ArtistSearchHit{hitFromDetail(d)}, nil
}

// hitFromDetail builds a hit from an artist detail response (direct lookup).
func hitFromDetail(d mbArtistDetail) ArtistSearchHit {
	return ArtistSearchHit{
		ID:             d.ID,
		Title:          d.Name,
		Type:           d.Type,
		Country:        d.Country,
		Disambiguation: d.Disambiguation,
		Genres:         genreNames(d.Genres),
		Links:          linkURLs(d.Relations),
	}
}

// enrichArtistHit fills the best-effort badge fields of a search hit from the
// artist detail endpoint. Any failure leaves the existing (empty or
// search-derived) values in place.
func (m *MusicBrainz) enrichArtistHit(ctx context.Context, h *ArtistSearchHit) {
	if h.ID == "" {
		return
	}
	values := url.Values{}
	values.Set("inc", "genres+url-rels")
	var d mbArtistDetail
	if err := m.getJSON(ctx, "/artist/"+h.ID, values, &d); err != nil {
		return // best-effort: "empty when unknown"
	}
	if d.Type != "" {
		h.Type = d.Type
	}
	if d.Country != "" {
		h.Country = d.Country
	}
	if d.Disambiguation != "" {
		h.Disambiguation = d.Disambiguation
	}
	if d.Genres != nil {
		h.Genres = genreNames(d.Genres)
	}
	if d.Relations != nil {
		h.Links = linkURLs(d.Relations)
	}
}

func genreNames(gs []struct {
	Name string `json:"name"`
}) []string {
	out := []string{}
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

func linkURLs(rels []mbArtistRelation) []string {
	out := []string{}
	for _, u := range rels {
		if u.TargetType != "url" {
			continue
		}
		if u.URL.Resource != "" {
			out = append(out, u.URL.Resource)
		}
	}
	return out
}

// SearchReleaseGroupHits searches MusicBrainz release groups for q (the same
// query the add page searched with) and maps them to display hits: the
// normalized type, first-release year, and the number of releases in the
// group. The group's artist comes from the search payload's artist-credit
// (cheap — no extra request); "" when the payload has none.
func (m *MusicBrainz) SearchReleaseGroupHits(ctx context.Context, q string) ([]ReleaseGroupHit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []ReleaseGroupHit{}, nil
	}
	m.Base = m.baseURL()
	values := url.Values{}
	values.Set("query", "release-group:"+q)
	values.Set("fmt", "json")
	var resp mbReleaseGroupSearchResponse
	if err := m.getJSON(ctx, "/release-group", values, &resp); err != nil {
		return nil, err
	}
	out := make([]ReleaseGroupHit, 0, len(resp.ReleaseGroups))
	for _, g := range resp.ReleaseGroups {
		if g.Title == "" {
			continue
		}
		var artists []string
		for _, c := range g.ArtistCredit {
			if c.Artist.Name != "" {
				artists = append(artists, c.Artist.Name)
			}
		}
		out = append(out, ReleaseGroupHit{
			ID:           g.ID,
			Title:        g.Title,
			Year:         releaseGroupYear(g.FirstReleaseDate),
			ReleaseCount: g.Count,
			Type:         normalizeReleaseGroupType(g.PrimaryType),
			Artist:       strings.Join(artists, ", "),
		})
	}
	return out, nil
}

// releaseGroupYear extracts the year from the MusicBrainz first-release-date
// search field ("1998", "1998-06-01", ...); 0 when unknown.
func releaseGroupYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	n, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return n
}

// normalizeReleaseGroupType maps a MusicBrainz release-group type onto the
// L2 enum (album/single/ep/compilation/live/remix/broadcast/demo/other/
// djmix); unmapped types (soundtrack, spoken word, video, ...) become
// "other".
func normalizeReleaseGroupType(t string) string {
	n := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(t))
	switch n {
	case "album":
		return "album"
	case "single":
		return "single"
	case "ep":
		return "ep"
	case "compilation":
		return "compilation"
	case "live":
		return "live"
	case "remix":
		return "remix"
	case "broadcast":
		return "broadcast"
	case "demo":
		return "demo"
	case "djmix":
		return "djmix"
	default:
		return "other"
	}
}
