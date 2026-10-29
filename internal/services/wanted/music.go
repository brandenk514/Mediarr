// music.go carries the L4 (Lidarr) music wanted flows: the Missing /
// Cutoff Unmet row listings, the Search All fan-out, and batch
// unmonitoring for kind=music. The cutoff resolution lives in the domain
// (the L3 seeded-profile rule); the repository owns the music_* reads and
// the monitor/wanted transitions.
package wanted

import (
	"context"
	"fmt"

	dom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/paging"
)

// musicCutoffs maps album id to the resolved effective cutoff level id (L3
// rule: the album's quality-profile name against the seeded profiles,
// falling back to the default profile for empty/unknown names). 0 = not
// evaluable.
func musicCutoffs(rows []dom.MusicRow) map[int64]int {
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		if _, ok := out[r.AlbumID]; ok {
			continue
		}
		out[r.AlbumID] = dom.MusicCutoffLevel(r.ProfileName)
	}
	return out
}

// ListMusic returns one server-side page of the tab's wanted music rows.
// The Q search matches the album title or the artist title (the music
// analogue of the TV series-title search).
func (s *Service) ListMusic(ctx context.Context, tab dom.Tab, q string, page, pageSize int) (paging.Page[dom.Item], error) {
	tab = tab.Normalize()
	if !tab.Valid() {
		return paging.Page[dom.Item]{Items: []dom.Item{}}, dom.ErrUnknownTab
	}
	rows, err := s.repo.MusicRows(ctx)
	if err != nil {
		return paging.Page[dom.Item]{}, err
	}
	items := dom.MusicFilter{Tab: tab, Q: q}.Apply(rows, musicCutoffs(rows))

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = paging.DefaultPageSize
	}
	if pageSize > paging.MaxPageSize {
		pageSize = paging.MaxPageSize
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		return paging.Page[dom.Item]{Items: []dom.Item{}, Total: total, Page: page, PageSize: pageSize}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return paging.Page[dom.Item]{Items: items[start:end], Total: total, Page: page, PageSize: pageSize}, nil
}

// SearchAllMusic fans out a search for the tab's wanted music: it ensures a
// pending music_wanted row for each item (the album when the row is
// album-level, else the track — re-requesting the search for
// cutoff-unmet albums) and records one search-requested event per affected
// artist (in music_history, the kind "music" source of the global feed).
// It returns how many items are now wanted for the search. Like TV's
// search-all it records the request; it does NOT run the pipeline.
func (s *Service) SearchAllMusic(ctx context.Context, tab dom.Tab) (int, error) {
	tab = tab.Normalize()
	if !tab.Valid() {
		return 0, dom.ErrUnknownTab
	}
	rows, err := s.repo.MusicRows(ctx)
	if err != nil {
		return 0, err
	}
	items := dom.MusicFilter{Tab: tab}.Apply(rows, musicCutoffs(rows))

	count := 0
	perArtist := make(map[int64]int)
	for _, it := range items {
		if err := s.repo.EnsureMusicWanted(ctx, it.AlbumID, it.Season, it.Episode); err != nil {
			return 0, err
		}
		count++
		perArtist[it.ArtistID]++
	}
	for id, n := range perArtist {
		detail := "Searched 1 wanted album"
		if n > 1 {
			detail = fmt.Sprintf("Searched %d wanted albums", n)
		}
		if err := s.repo.AddMusicHistory(ctx, id, "search-requested", detail); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// UnmonitorMusic turns monitoring off for each music ref (whole album or
// one track), counting how many existed (unknown refs are skipped).
func (s *Service) UnmonitorMusic(ctx context.Context, refs []dom.MusicRef) (int, error) {
	count := 0
	for _, r := range refs {
		ok, err := s.repo.SetMusicMonitoredOff(ctx, r.AlbumID, r.Disc, r.Number)
		if err != nil {
			return 0, err
		}
		if ok {
			count++
		}
	}
	return count, nil
}
