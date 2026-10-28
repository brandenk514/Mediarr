// import_manual.go is the L4 manual import for kind=music (Wanted > Manual
// Import). It runs the same import path the download-client flow uses
// (PlanAlbumImport/PlanTrackImport + copyFile under the media root) minus
// the checksum and queue handling: the user points at a local audio file
// (a single track file or a whole-album file) or a directory of per-track
// files, the files are placed under the album folder, the covered tracks
// are marked imported with the quality string detected from the file
// extension (the phase-1 stand-in for a tag reader), and the covered
// wanted rows are satisfied. Archives (zip/rar) are not supported in
// phase 1 (ErrUnsupportedFile).
package music

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	dom "github.com/brandenk514/mediarr/internal/domains/music"
	wdom "github.com/brandenk514/mediarr/internal/domains/wanted"
	"github.com/brandenk514/mediarr/internal/fsutil"
)

// ManualImport records a manual import of a wanted music item from local
// files. The file path is confined to the media root (the same path-
// injection defense as the TV endpoint, mapped onto the wanted domain's
// sentinels so the API keeps one error→status mapping).
//
// Semantics (phase 1):
//   - A single audio file whose name carries a track prefix (NN-NN or NN)
//     that matches a known track imports that one track; any other single
//     audio file is a whole-album file and covers every track.
//   - A directory imports every recognizable audio file that matches a
//     known track; unmatched files are ignored (counted in the message).
//
// Covered tracks get the codec from their file's extension as their
// imported quality (flac→FLAC, mp3→MP3, m4a→AAC, ogg→OGG, ...). Covered
// wanted rows (the whole-album row, or the individual track rows) are
// satisfied. A "manual-import" event is recorded in the artist's history
// (the kind "music" source of the global Activity feed).
func (s *Service) ManualImport(ctx context.Context, req wdom.ManualImport) (wdom.ManualImportReport, error) {
	empty := wdom.ManualImportReport{}
	if req.AlbumID <= 0 {
		return empty, dom.ErrAlbumNotFound
	}
	album, err := s.deps.Repo.GetAlbum(ctx, req.AlbumID)
	if err != nil {
		return empty, err
	}
	artist, err := s.deps.Repo.GetArtist(ctx, album.ArtistID)
	if err != nil {
		return empty, err
	}

	impPath, err := fsutil.ResolveConfined(s.deps.MediaRoot, req.FilePath)
	if err != nil {
		switch {
		case errors.Is(err, fsutil.ErrEmptyPath):
			return empty, wdom.ErrEmptyPath
		case errors.Is(err, fsutil.ErrPathOutsideRoot):
			return empty, wdom.ErrPathOutsideRoot
		}
		return empty, err
	}
	// CodeQL [go/path-injection] impPath is validated by fsutil.ResolveConfined
	// to be strictly inside s.deps.MediaRoot, so the raw user value can
	// never reach the filesystem here.
	st, err := os.Stat(impPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, wdom.ErrFileNotFound
		}
		return empty, err
	}

	var covered []dom.Track // tracks marked imported
	var ignored int
	if st.IsDir() {
		covered, ignored, err = s.importMusicDir(ctx, impPath, artist, *album)
	} else {
		covered, err = s.importMusicFile(ctx, impPath, artist, *album)
	}
	if err != nil {
		return empty, err
	}
	if len(covered) == 0 {
		return empty, fmt.Errorf("%w: no file covered a known track of the album", dom.ErrUnsupportedFile)
	}

	release := strings.TrimSpace(req.ReleaseTitle)
	if release == "" {
		release = filepath.Base(impPath)
	}
	satisfied := s.satisfyWanted(ctx, *album, covered, release)

	detail := fmt.Sprintf("imported %s covering %d track(s), %d wanted row(s) satisfied", impPath, len(covered), satisfied)
	if release != filepath.Base(impPath) {
		detail += " (release: " + release + ")"
	}
	if ignored > 0 {
		detail += fmt.Sprintf(", %d unmatched file(s) ignored", ignored)
	}
	s.record(ctx, artist.ID, "manual-import", detail)
	return wdom.ManualImportReport{Imported: true, Message: detail}, nil
}

// importMusicFile places one audio file under the album folder (a single
// track file when its name matches a known track, else a whole-album file)
// and returns the covered tracks.
func (s *Service) importMusicFile(ctx context.Context, src string, artist *dom.Artist, album dom.Album) ([]dom.Track, error) {
	codec := dom.CodecFromExtension(filepath.Ext(src))
	if codec == "" {
		return nil, dom.ErrUnsupportedFile
	}
	tracks, err := s.deps.Repo.ListTracks(ctx, album.ID)
	if err != nil {
		return nil, err
	}
	if disc, number, ok := trackFileRef(filepath.Base(src)); ok {
		for _, t := range tracks {
			if t.Disc == disc && t.Number == number {
				dest, err := s.placeMusicFile(artist, album, src, t.Title, disc, number)
				if err != nil {
					return nil, err
				}
				if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
					if err := copyFile(src, dest); err != nil {
						return nil, err
					}
				}
				if _, err := s.deps.Repo.SetTrackStatus(ctx, album.ID, disc, number, codec); err != nil {
					return nil, err
				}
				return []dom.Track{t}, nil
			}
		}
	}
	// Whole-album file: the pipeline's layout, reusing the destination when
	// the album file is already in place (mirroring importAlbum).
	plan, err := dom.PlanAlbumImport(s.deps.MediaRoot, artist.Name, album.Name, src)
	if err != nil {
		if !errors.Is(err, dom.ErrCollision) {
			return nil, err
		}
		dest, derr := dom.AlbumDestPath(s.deps.MediaRoot, artist.Name, album.Name, src)
		if derr != nil {
			return nil, derr
		}
		plan = &dom.Import{SourcePath: src, DestPath: dest}
	}
	if _, statErr := os.Stat(plan.DestPath); os.IsNotExist(statErr) {
		if err := copyFile(plan.SourcePath, plan.DestPath); err != nil {
			return nil, err
		}
	}
	covered := make([]dom.Track, 0, len(tracks))
	for _, t := range tracks {
		if _, err := s.deps.Repo.SetTrackStatus(ctx, album.ID, t.Disc, t.Number, codec); err != nil {
			return nil, err
		}
		covered = append(covered, t)
	}
	return covered, nil
}

// importMusicDir walks a directory of per-track files, placing each
// recognizable audio file under the album folder when it matches a known
// track and marking that track imported. It returns the covered tracks and
// how many audio files were ignored (no matching track).
func (s *Service) importMusicDir(ctx context.Context, dir string, artist *dom.Artist, album dom.Album) ([]dom.Track, int, error) {
	tracks, err := s.deps.Repo.ListTracks(ctx, album.ID)
	if err != nil {
		return nil, 0, err
	}
	var covered []dom.Track
	ignored := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		codec := dom.CodecFromExtension(filepath.Ext(d.Name()))
		if codec == "" {
			return nil // not an audio file; leave it alone
		}
		disc, number, ok := trackFileRef(d.Name())
		if !ok {
			ignored++
			return nil
		}
		matched := false
		for _, t := range tracks {
			if t.Disc != disc || t.Number != number {
				continue
			}
			dest, err := s.placeMusicFile(artist, album, path, t.Title, disc, number)
			if err != nil {
				return err
			}
			if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
				if err := copyFile(path, dest); err != nil {
					return err
				}
			}
			if _, err := s.deps.Repo.SetTrackStatus(ctx, album.ID, disc, number, codec); err != nil {
				return err
			}
			covered = append(covered, t)
			matched = true
			break
		}
		// An audio file that matches no known track is ignored.
		if !matched {
			ignored++
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return covered, ignored, nil
}

// placeMusicFile computes where one track file lands under the album folder
// (the per-track layout the download-client import uses) and returns the
// destination.
func (s *Service) placeMusicFile(artist *dom.Artist, album dom.Album, src, title string, disc, number int) (string, error) {
	plan, err := dom.PlanTrackImport(s.deps.MediaRoot, artist.Name, album.Name, disc, number, title, src)
	if err != nil {
		if !errors.Is(err, dom.ErrCollision) {
			return "", err
		}
		// Already in place: reuse the destination.
		return plan.DestPath, nil
	}
	return plan.DestPath, nil
}

// trackFileRef parses a track file name's numeric prefix: "01-01 Title" →
// disc 1, number 1; "01 Title" → disc 1, number 1. ok=false when the name
// carries no usable prefix.
func trackFileRef(name string) (disc, number int, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	words := strings.Fields(base)
	if len(words) == 0 {
		return 0, 0, false
	}
	first := words[0]
	if i := strings.Index(first, "-"); i > 0 {
		if d, err1 := strconv.Atoi(first[:i]); err1 == nil && d > 0 {
			if n, err2 := strconv.Atoi(first[i+1:]); err2 == nil && n > 0 {
				return d, n, true
			}
		}
	}
	if n, err := strconv.Atoi(first); err == nil && n > 0 {
		return 1, n, true
	}
	return 0, 0, false
}

// satisfyWanted retires the wanted rows the import covers: for a
// whole-album import the whole-album row plus every covered track row, for a
// single-track import that track's row. Only rows that exist and are still
// pending are touched; it returns how many rows were satisfied.
func (s *Service) satisfyWanted(ctx context.Context, album dom.Album, covered []dom.Track, release string) int {
	satisfied := 0
	if s.isWholeAlbumImport(ctx, album.ID, covered) {
		if s.satisfyRow(ctx, album.ID, 0, release) {
			satisfied++
		}
	}
	for _, t := range covered {
		if s.satisfyRow(ctx, album.ID, t.ID, release) {
			satisfied++
		}
	}
	return satisfied
}

// isWholeAlbumImport reports whether covered spans every track of the album
// (the whole-album file case, as opposed to one matched track file).
func (s *Service) isWholeAlbumImport(ctx context.Context, albumID int64, covered []dom.Track) bool {
	all, err := s.deps.Repo.ListTracks(ctx, albumID)
	if err != nil {
		return false
	}
	return len(all) > 0 && len(covered) == len(all)
}

// satisfyRow satisfies one wanted row when it exists and is pending.
func (s *Service) satisfyRow(ctx context.Context, albumID, trackID int64, release string) bool {
	w, err := s.deps.Repo.GetWanted(ctx, albumID, trackID)
	if err != nil || w == nil || w.Status != dom.WantedPending {
		return false
	}
	if err := s.deps.Repo.MarkWantedSatisfied(ctx, albumID, trackID, release); err != nil {
		s.record(ctx, albumID, "satisfy-failed", err.Error())
		return false
	}
	return true
}
