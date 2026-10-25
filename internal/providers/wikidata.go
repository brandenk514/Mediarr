package providers

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// P5.7 keyless fallback for cross-language titles, summaries, and the
// external-id crosswalk: Wikipedia (opensearch) + Wikidata (entity
// resolution). Both endpoints are keyless and stable.

const (
	defaultWikipediaBase = "https://en.wikipedia.org"
	defaultWikidataBase  = "https://www.wikidata.org"
)

// Wikidata is the keyless crosswalk/titles adapter. Search returns Wikipedia
// titles (with descriptions); Resolve maps a Wikipedia title to its Wikidata
// entity (the Q-id that interlinks TMDB/IMDb/TVDB/Wikidata identifiers —
// good for matching across keyed providers).
type Wikidata struct {
	Client
	// WikipediaBase defaults to defaultWikipediaBase; WikidataBase to
	// defaultWikidataBase. Both overridable for tests.
	WikipediaBase string
	WikidataBase  string
}

// NewWikidata builds the adapter. No auth.
func NewWikidata() *Wikidata {
	return &Wikidata{Client: Client{Name: "wikidata", UserAgent: "mediarr/1.0 (https://github.com/brandenk514/Mediarr)"}}
}

// Name implements Searcher.
func (w *Wikidata) Name() string { return "wikidata" }

// opensearchResponse is the MediaWiki opensearch shape:
// [0] = search prefix, [1] = titles, [2] = descriptions, [3] = view URLs.
type opensearchResponse struct {
	Titles       []string `json:"-"`
	Descriptions []string `json:"-"`
}

func (r *opensearchResponse) UnmarshalJSON(b []byte) error {
	var raw [4]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) >= 2 {
		if titles, ok := raw[1].([]any); ok {
			for _, t := range titles {
				if s, ok := t.(string); ok {
					r.Titles = append(r.Titles, s)
				}
			}
		}
	}
	if len(raw) >= 3 {
		if descs, ok := raw[2].([]any); ok {
			for _, d := range descs {
				if s, ok := d.(string); ok {
					r.Descriptions = append(r.Descriptions, s)
				}
			}
		}
	}
	return nil
}

// Search searches Wikipedia (en) for q via opensearch. The Result ID is the
// page title (which Resolve turns into a Wikidata entity).
func (w *Wikidata) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	base := w.WikipediaBase
	if base == "" {
		base = defaultWikipediaBase
	}
	values := url.Values{}
	values.Set("action", "opensearch")
	values.Set("search", q)
	values.Set("limit", "25")
	values.Set("format", "json")

	w.Base = base
	var resp opensearchResponse
	if err := w.getJSON(ctx, "/w/api.php", values, &resp); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(resp.Titles))
	for i, title := range resp.Titles {
		desc := ""
		if i < len(resp.Descriptions) {
			desc = resp.Descriptions[i]
		}
		extra := map[string]string{}
		if desc != "" {
			extra["description"] = desc
		}
		out = append(out, Result{ID: title, Title: title, Extra: extra})
	}
	return out, nil
}

type wbEntitiesResponse struct {
	Entities map[string]struct {
		ID      string `json:"id"` // "Q42"
		Labels  map[string]struct {
			Value string `json:"value"`
		} `json:"labels"`
	} `json:"entities"`
}

// Resolve maps a Wikipedia page title to its Wikidata entity id (e.g.
// "Breaking_Bad" -> "Q1162946"). A title with no entity answers "".
func (w *Wikidata) Resolve(ctx context.Context, pageTitle string) (string, error) {
	base := w.WikidataBase
	if base == "" {
		base = defaultWikidataBase
	}
	values := url.Values{}
	values.Set("action", "wbgetentities")
	values.Set("sites", "enwiki")
	values.Set("titles", pageTitle)
	values.Set("language", "en")
	values.Set("props", "labels")
	values.Set("format", "json")

	w.Base = base
	var resp wbEntitiesResponse
	if err := w.getJSON(ctx, "/w/api.php", values, &resp); err != nil {
		return "", err
	}
	for _, e := range resp.Entities {
		if strings.HasPrefix(e.ID, "Q") {
			return e.ID, nil
		}
	}
	return "", nil
}
