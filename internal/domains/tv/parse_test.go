package tv

import (
	"errors"
	"reflect"
	"testing"
)

// TestParseRelease is the table-driven unit test for the TV release-name
// parser (PLAN §13 / §14). Each case exercises one release-name shape; the
// parser is pure and these run with no I/O.
func TestParseRelease(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantTitle string
		wantYear  int
		wantSe    int
		wantEps   []int
		wantPack  bool
		wantPart  int
		wantH     int
		wantCodec string
		wantSrc   string
		wantErr   bool
	}{
		{
			name:      "single episode dotted",
			in:        "Breaking.Bad.S01E01.Piloto.1080p.WEB.x264",
			wantTitle: "Breaking Bad",
			wantSe:    1, wantEps: []int{1},
			wantH: 1080, wantCodec: CodecH264, wantSrc: "web",
		},
		{
			name:      "single episode with scene group",
			in:        "The.Wire.S01E01.720p.HDTV.x264-KILLERS",
			wantTitle: "The Wire",
			wantSe:    1, wantEps: []int{1},
			wantH: 720, wantCodec: CodecH264, wantSrc: "hdtv",
		},
		{
			name:      "multi episode E01E02",
			in:        "Game.of.Thrones.S01E01E02.1080p.BluRay.x265",
			wantTitle: "Game of Thrones",
			wantSe:    1, wantEps: []int{1, 2},
			wantH: 1080, wantCodec: CodecH265, wantSrc: "blu-ray",
		},
		{
			name:      "dashed range E01-04",
			in:        "Fringe.S01E01-04.720p.WEB.x264",
			wantTitle: "Fringe",
			wantSe:    1, wantEps: []int{1, 2, 3, 4},
			wantH: 720, wantCodec: CodecH264, wantSrc: "web",
		},
		{
			name:      "year and title",
			in:        "Stranger.Things.2016.S01E01.1080p.WEB-DL.x265",
			wantTitle: "Stranger Things",
			wantYear:  2016,
			wantSe:    1, wantEps: []int{1},
			wantH: 1080, wantCodec: CodecH265, wantSrc: "web",
		},
		{
			name:      "season pack complete",
			in:        "Better.Calls.Saul.S01.COMPLETE.1080p.WEB.x264",
			wantTitle: "Better Calls Saul",
			wantSe:    1, wantPack: true,
			wantH: 1080, wantCodec: CodecH264, wantSrc: "web",
		},
		{
			name:      "multi part 1 of 2",
			in:        "Westworld.S01E01.Part.1.of.2.1080p.WEB.x264",
			wantTitle: "Westworld",
			wantSe:    1, wantEps: []int{1}, wantPart: 1,
			wantH: 1080, wantCodec: CodecH264, wantSrc: "web",
		},
		{
			name:      "2160p uhd",
			in:        "Breaking.Bad.S01E01.2160p.WEB.x265.10bit",
			wantTitle: "Breaking Bad",
			wantSe:    1, wantEps: []int{1},
			wantH: 2160, wantCodec: CodecH265, wantSrc: "web",
		},
		{
			name:    "no episode marker -> typed error",
			in:      "Breaking.Bad.1080p.WEB.x264",
			wantErr: true,
		},
		{
			name:    "bare garbage -> typed error",
			in:      "totally-unrecognizable-name-123",
			wantErr: true,
		},
		{
			name:    "empty -> typed error",
			in:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRelease(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRelease(%q) expected error, got %+v", tt.in, got)
				}
				if !errors.Is(err, ErrNoEpisodeMarker) {
					t.Errorf("expected ErrNoEpisodeMarker, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRelease(%q) unexpected error: %v", tt.in, err)
			}
			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.Year != tt.wantYear {
				t.Errorf("Year = %d, want %d", got.Year, tt.wantYear)
			}
			if got.Season != tt.wantSe {
				t.Errorf("Season = %d, want %d", got.Season, tt.wantSe)
			}
			if !reflect.DeepEqual(got.Episodes, tt.wantEps) {
				t.Errorf("Episodes = %v, want %v", got.Episodes, tt.wantEps)
			}
			if got.SeasonPack != tt.wantPack {
				t.Errorf("SeasonPack = %v, want %v", got.SeasonPack, tt.wantPack)
			}
			if tt.wantPart != 0 && got.Part != tt.wantPart {
				t.Errorf("Part = %d, want %d", got.Part, tt.wantPart)
			}
			if got.Height != tt.wantH {
				t.Errorf("Height = %d, want %d", got.Height, tt.wantH)
			}
			if tt.wantCodec != "" && got.Codec != tt.wantCodec {
				t.Errorf("Codec = %q, want %q", got.Codec, tt.wantCodec)
			}
			if tt.wantSrc != "" && got.Source != tt.wantSrc {
				t.Errorf("Source = %q, want %q", got.Source, tt.wantSrc)
			}
		})
	}
}

// TestParseRelease_TypedError confirms the parser returns a *typed* error
// (wrapping ErrNoEpisodeMarker), not a silent guess, per PLAN §14.
func TestParseRelease_TypedError(t *testing.T) {
	_, err := ParseRelease("Some.Show.Without.Episodes.1080p.WEB")
	if err == nil {
		t.Fatal("expected a typed error for a name with no episode marker")
	}
	if !errors.Is(err, ErrNoEpisodeMarker) {
		t.Fatalf("error %v does not wrap ErrNoEpisodeMarker", err)
	}
}

// TestParseRelease_EpisodeMarkerPreserved confirms a name that looks like a
// movie (has a year) but also has an episode marker still parses as TV.
func TestParseRelease_EpisodeMarkerPreserved(t *testing.T) {
	got, err := ParseRelease("The.Office.2005.S01E02.1080p.WEB.x264")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Season != 1 || len(got.Episodes) != 1 || got.Episodes[0] != 2 {
		t.Errorf("expected S01E02, got S%02dE%v", got.Season, got.Episodes)
	}
}
