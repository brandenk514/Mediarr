// Package metawrite is the metadata file writer (P5 wave 2): it generates
// .nfo sidecar files next to imported media (Sonarr's "nfo file" metadata
// option). The writer is driven by the metadata section document
// (generate_nfo / language); the wanted import service calls it
// best-effort after a successful import, so an NFO failure can never fail
// the import itself.
package metawrite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EpisodeInfo is the episode metadata an NFO is generated from.
type EpisodeInfo struct {
	SeriesTitle  string
	EpisodeTitle string
	Season       int
	Episode      int
	AirDate      string // empty = omit the aired element
	Overview     string // empty = omit the plot element
}

// MovieInfo is the movie metadata an NFO is generated from.
type MovieInfo struct {
	Title    string
	Year     int // 0 = omit
	Overview string // empty = omit
}

// Writer is a metadata file writer configured from the metadata section
// document.
type Writer struct {
	// Enabled mirrors metadata.generate_nfo; when false every Write call
	// is a no-op returning ("", nil) — the caller can skip it too.
	Enabled bool
	// Language is the default metadata language code (documented in the
	// <episodedetails> lang attribute; empty = unset).
	Language string
}

// WriteEpisodeNFO writes <mediaPath>.nfo with the episode's NFO XML and
// returns the NFO path.
func (w *Writer) WriteEpisodeNFO(mediaPath string, info EpisodeInfo) (string, error) {
	if !w.Enabled {
		return "", nil
	}
	content := episodeNFOContent(info, w.Language)
	return w.writeSidecar(mediaPath, content)
}

// WriteMovieNFO writes <mediaPath>.nfo with the movie's NFO XML and returns
// the NFO path.
func (w *Writer) WriteMovieNFO(mediaPath string, info MovieInfo) (string, error) {
	if !w.Enabled {
		return "", nil
	}
	content := movieNFOContent(info, w.Language)
	return w.writeSidecar(mediaPath, content)
}

func (w *Writer) writeSidecar(mediaPath, content string) (string, error) {
	dir := filepath.Dir(mediaPath)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("metawrite: media dir missing: %v", err)
	}
	path := mediaPath + ".nfo"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("metawrite: write nfo: %v", err)
	}
	return path, nil
}

func episodeNFOContent(i EpisodeInfo, lang string) string {
	var b strings.Builder
	b.WriteString("<episodedetails>\n")
	if lang != "" {
		fmt.Fprintf(&b, "  <lang>%s</lang>\n", xmlEscape(lang))
	}
	fmt.Fprintf(&b, "  <title>%s</title>\n", xmlEscape(i.SeriesTitle))
	fmt.Fprintf(&b, "  <showtitle>%s</showtitle>\n", xmlEscape(i.SeriesTitle))
	if i.EpisodeTitle != "" {
		fmt.Fprintf(&b, "  <episode_title>%s</episode_title>\n", xmlEscape(i.EpisodeTitle))
	}
	fmt.Fprintf(&b, "  <season>%d</season>\n", i.Season)
	fmt.Fprintf(&b, "  <episode>%d</episode>\n", i.Episode)
	if i.AirDate != "" {
		fmt.Fprintf(&b, "  <aired>%s</aired>\n", xmlEscape(i.AirDate))
	}
	if i.Overview != "" {
		fmt.Fprintf(&b, "  <plot>%s</plot>\n", xmlEscape(i.Overview))
	}
	b.WriteString("</episodedetails>\n")
	return b.String()
}

func movieNFOContent(i MovieInfo, lang string) string {
	var b strings.Builder
	b.WriteString("<movie>\n")
	if lang != "" {
		fmt.Fprintf(&b, "  <lang>%s</lang>\n", xmlEscape(lang))
	}
	fmt.Fprintf(&b, "  <title>%s</title>\n", xmlEscape(i.Title))
	if i.Year > 0 {
		fmt.Fprintf(&b, "  <premiered>%04d-01-01</premiered>\n", i.Year)
	}
	if i.Overview != "" {
		fmt.Fprintf(&b, "  <plot>%s</plot>\n", xmlEscape(i.Overview))
	}
	b.WriteString("</movie>\n")
	return b.String()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
