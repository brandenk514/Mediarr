package api

import (
	"net/http"
	"strings"

	indexerdefinitions "github.com/brandenk514/mediarr/internal/indexers/definitions"
)

// ListIndexerDefinitions handles GET /api/v1/indexer/definitions (Prowlarr
// PR2): the built-in definition library the Add/Edit Indexer modal renders.
// Optional query filters, all combinable:
//
//	q         case-insensitive substring match on the display name or id
//	protocol  exact match ("torrent" | "usenet")
//	privacy   exact match ("public" | "private" | "semi-private")
//	category  case-insensitive membership in the definition's categories
//
// The response is ordered by display name. `presets` is always an empty
// array for now (the field exists so the UI shape is stable when presets
// land later).
func (s *Server) ListIndexerDefinitions(w http.ResponseWriter, r *http.Request) {
	store := s.indexersDeps.Definitions
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "definition library is not available")
		return
	}
	defs, err := store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	indexerdefinitions.SortByDisplay(defs)

	q := strings.ToLower(r.URL.Query().Get("q"))
	protocol := r.URL.Query().Get("protocol")
	privacy := r.URL.Query().Get("privacy")
	category := strings.ToLower(r.URL.Query().Get("category"))

	out := make([]definitionJSON, 0, len(defs))
	for i := range defs {
		d := defs[i]
		if q != "" && !strings.Contains(strings.ToLower(d.Name), q) && !strings.Contains(strings.ToLower(d.ID), q) {
			continue
		}
		if protocol != "" && d.Protocol != protocol {
			continue
		}
		if privacy != "" && d.Privacy != privacy {
			continue
		}
		if category != "" && !categoryMatches(d.Categories, category) {
			continue
		}
		out = append(out, definitionToJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// categoryMatches reports whether the definition's categories contain the
// wanted category (case-insensitive; a "TV/Anime" query matches exactly).
func categoryMatches(cats []string, want string) bool {
	for _, c := range cats {
		if strings.EqualFold(c, want) {
			return true
		}
	}
	return false
}

// definitionJSON is the wire shape of one library definition. Slices are
// always non-nil (JSON arrays, never null) so the UI can rely on a stable
// shape.
type definitionJSON struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Protocol   string                     `json:"protocol"`
	Privacy    string                     `json:"privacy"`
	Language   string                     `json:"language"`
	Categories []string                   `json:"categories"`
	URLs       []string                   `json:"urls"`
	LegacyURLs []string                   `json:"legacyUrls"`
	Fields     []indexerdefinitions.Field `json:"fields"`
	Presets    []any                      `json:"presets"`
}

func definitionToJSON(d indexerdefinitions.Definition) definitionJSON {
	cats := d.Categories
	if cats == nil {
		cats = []string{}
	}
	urls := d.URLs
	if urls == nil {
		urls = []string{}
	}
	legacy := d.LegacyURLs
	if legacy == nil {
		legacy = []string{}
	}
	fields := d.Fields
	if fields == nil {
		fields = []indexerdefinitions.Field{}
	}
	return definitionJSON{
		ID:         d.ID,
		Name:       d.Name,
		Protocol:   d.Protocol,
		Privacy:    d.Privacy,
		Language:   d.Language,
		Categories: cats,
		URLs:       urls,
		LegacyURLs: legacy,
		Fields:     fields,
		Presets:    []any{},
	}
}
