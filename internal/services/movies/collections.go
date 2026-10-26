package movies

import (
	"context"
	"sort"
	"strings"

	dom "github.com/brandenk514/mediarr/internal/domains/movies"
	"github.com/brandenk514/mediarr/internal/providers"
)

// TMDBCollectionParter is the narrow provider surface the collections page
// needs for missing_count (Radarr R3). *providers.TMDB satisfies it; tests
// use a fake. Best-effort: a failing (or absent) fetch leaves missing_count
// 0 — the collections endpoint never fails on a collection fetch.
type TMDBCollectionParter interface {
	GetCollectionParts(ctx context.Context, collectionID string) ([]providers.CollectionPart, error)
}

// collectionIDUnknown marks groups that the library knows only by name (no
// TMDB id was persisted): the missing count is 0 for them.
const collectionIDUnknown = "unknown"

// ListCollections groups the library by TMDB collection (Radarr R3
// collections page). Movies with a persisted collection id group by id;
// movies with only a collection name fall back to name grouping (id
// reported as "unknown"); ungrouped movies are skipped. Members carry their
// full list-read state (editions, files, status) with no provider
// enrichment. The result is sorted by collection name.
func (s *Service) ListCollections(ctx context.Context) ([]dom.MovieCollection, error) {
	all, err := s.deps.Repo.ListMovies(ctx)
	if err != nil {
		return nil, err
	}

	type group struct {
		id      string
		name    string
		members []dom.Movie
	}
	groups := map[string]*group{}
	var order []string
	for _, m := range all {
		var key, id, name string
		if m.CollectionID != "" {
			key = "id:" + m.CollectionID
			id = m.CollectionID
			if m.CollectionName != nil {
				name = *m.CollectionName
			}
		} else if m.CollectionName != nil && *m.CollectionName != "" {
			key = "name:" + *m.CollectionName
			id = collectionIDUnknown
			name = *m.CollectionName
		} else {
			continue // ungrouped: not part of any collection
		}
		g, ok := groups[key]
		if !ok {
			g = &group{id: id}
			groups[key] = g
			order = append(order, key)
		}
		if g.name == "" {
			g.name = name
		}
		g.members = append(g.members, m)
	}
	if len(order) == 0 {
		return []dom.MovieCollection{}, nil
	}

	// buildViews preserves order, so the flat list splits back into groups
	// by the recorded member counts.
	grouped := make([]dom.Movie, 0)
	for _, key := range order {
		grouped = append(grouped, groups[key].members...)
	}
	views, err := s.buildViews(ctx, grouped, false)
	if err != nil {
		return nil, err
	}
	out := make([]dom.MovieCollection, 0, len(order))
	offset := 0
	for _, key := range order {
		g := groups[key]
		n := len(g.members)
		members := views[offset : offset+n]
		offset += n
		out = append(out, dom.MovieCollection{
			ID:           g.id,
			Name:         g.name,
			Movies:       members,
			MissingCount: s.missingCount(ctx, g.id, members),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// missingCount fetches the collection's TMDB parts and counts the ones with
// no matching library member. Best-effort: a nil parter (keyless), an
// unknown id, or any fetch failure returns 0 — never an error.
func (s *Service) missingCount(ctx context.Context, collectionID string, members []dom.Movie) int {
	if s.deps.CollectionParts == nil || collectionID == "" || collectionID == collectionIDUnknown {
		return 0
	}
	parts, err := s.deps.CollectionParts.GetCollectionParts(ctx, collectionID)
	if err != nil {
		return 0
	}
	missing := 0
	for _, p := range parts {
		if !partInLibrary(p, members) {
			missing++
		}
	}
	return missing
}

// partInLibrary reports whether a collection part matches a library member.
// The v1 identity is title+year (the library does not store TMDB ids), so a
// part counts as present when a member has the same normalized title and a
// compatible year (either year unknown = compatible).
func partInLibrary(p providers.CollectionPart, members []dom.Movie) bool {
	pt := normTitle(p.Title)
	if pt == "" {
		return false
	}
	for _, m := range members {
		if normTitle(m.Title) != pt {
			continue
		}
		if p.Year == 0 || m.Year == 0 || p.Year == m.Year {
			return true
		}
	}
	return false
}

// normTitle normalizes a title for loose comparison: case-insensitive,
// whitespace-collapsed.
func normTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// collectionNameOf flattens the nullable collection name for persistence.
func collectionNameOf(name *string) string {
	if name == nil {
		return ""
	}
	return *name
}
