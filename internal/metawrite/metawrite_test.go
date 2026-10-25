package metawrite

import (
	"os"
	"strings"
	"testing"
)

func TestWriteEpisodeNFO(t *testing.T) {
	dir := t.TempDir()
	media := dir + "/Show S01E01.mkv"
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := Writer{Enabled: true, Language: "en"}
	path, err := w.WriteEpisodeNFO(media, EpisodeInfo{
		SeriesTitle:  "The <Show> & \"Friends\"",
		EpisodeTitle: "The One",
		Season:       1,
		Episode:      1,
		AirDate:      "2026-10-01",
		Overview:     "A <big> one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != media+".nfo" {
		t.Fatalf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"<episodedetails>",
		"<lang>en</lang>",
		`<title>The &lt;Show&gt; &amp; &quot;Friends&quot;</title>`,
		"<season>1</season>",
		"<episode>1</episode>",
		"<aired>2026-10-01</aired>",
		"<plot>A &lt;big&gt; one</plot>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("nfo missing %q:\n%s", want, s)
		}
	}
}

func TestWriteMovieNFO(t *testing.T) {
	dir := t.TempDir()
	media := dir + "/Movie (2020).mkv"
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := (&Writer{Enabled: true}).WriteMovieNFO(media, MovieInfo{Title: "A Movie", Year: 2020})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "<movie>") || !strings.Contains(s, "<premiered>2020-01-01</premiered>") {
		t.Errorf("unexpected movie nfo:\n%s", s)
	}
}

func TestWriteNFO_DisabledNoOp(t *testing.T) {
	dir := t.TempDir()
	path, err := (&Writer{}).WriteEpisodeNFO(dir+"/x.mkv", EpisodeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("disabled writer must not write, got %q", path)
	}
}

func TestWriteNFO_MissingDir(t *testing.T) {
	_, err := (&Writer{Enabled: true}).WriteEpisodeNFO("/nonexistent-dir-"+t.Name()+"/x.mkv", EpisodeInfo{})
	if err == nil {
		t.Fatal("expected an error for a missing media dir")
	}
}
