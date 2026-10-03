package movies

import "testing"

func TestParseRelease_Standard(t *testing.T) {
	cases := []struct {
		name      string
		wantTitle string
		wantYear  int
		wantH     int
		wantCodec string
		wantSrc   string
	}{
		{
			name:      "Inception.2010.1080p.BluRay.x265",
			wantTitle: "Inception",
			wantYear:  2010,
			wantH:     1080,
			wantCodec: CodecH265,
			wantSrc:   "blu-ray",
		},
		{
			name:      "The.Matrix.1999.720p.WEBRip.AVC.x264",
			wantTitle: "Matrix", // "The" is a noise token
			wantYear:  1999,
			wantH:     720,
			wantCodec: CodecH264,
			wantSrc:   "web",
		},
		{
			name:      "Interstellar.2014.2160p.UHD.BluRay.x265-10bit",
			wantTitle: "Interstellar",
			wantYear:  2014,
			wantH:     2160,
			wantCodec: CodecH265,
			wantSrc:   "blu-ray",
		},
		{
			name:      "Dune.Part.Two.2024.1080p.WEB-DL.DDP5.1.H.265",
			wantTitle: "Dune Part Two",
			wantYear:  2024,
			wantH:     1080,
			wantCodec: CodecH265,
			wantSrc:   "web",
		},
		{
			name:      "Some.Movie.2020.480p.xvid",
			wantTitle: "Some Movie",
			wantYear:  2020,
			wantH:     480,
			wantCodec: "xvid",
		},
		{
			name:      "Noisy.Movie.2015.1080p.x264-FLYt3R",
			wantTitle: "Noisy Movie",
			wantYear:  2015,
			wantH:     1080,
			wantCodec: CodecH264,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRelease(tc.name)
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Year != tc.wantYear {
				t.Errorf("year = %d, want %d", got.Year, tc.wantYear)
			}
			if got.Height != tc.wantH {
				t.Errorf("height = %d, want %d", got.Height, tc.wantH)
			}
			if got.Codec != tc.wantCodec {
				t.Errorf("codec = %q, want %q", got.Codec, tc.wantCodec)
			}
			if tc.wantSrc != "" && got.Source != tc.wantSrc {
				t.Errorf("source = %q, want %q", got.Source, tc.wantSrc)
			}
		})
	}
}

func TestParseRelease_QualityName(t *testing.T) {
	if got := ParseRelease("Movie.2020.1080p").QualityName; got != "1080p" {
		t.Errorf("quality = %q, want 1080p", got)
	}
	if got := ParseRelease("Movie.2020.480p").QualityName; got != "480p" {
		t.Errorf("quality = %q, want 480p", got)
	}
	// Unknown height falls to sd.
	if got := ParseRelease("Movie.2020").QualityName; got != "sd" {
		t.Errorf("quality = %q, want sd", got)
	}
}

func TestMatchRelease_Preferences(t *testing.T) {
	p, _ := ProfileByName("HD-1080p")

	// 1080p release matches at rank 1080p.
	m := MatchRelease(p, ParsedRelease{Height: 1080, Codec: CodecH264})
	if !m.Matched {
		t.Fatalf("1080p should match HD-1080p profile")
	}

	// 2160p release also matches (better-than-requested), with a higher score.
	m2160 := MatchRelease(p, ParsedRelease{Height: 2160, Codec: CodecH265})
	if !m2160.Matched {
		t.Fatalf("2160p should match HD-1080p profile (accept better)")
	}
	if m2160.Score <= m.Score {
		t.Errorf("2160p score %d should exceed 1080p score %d", m2160.Score, m.Score)
	}

	// 480p release does not match a HD-1080p profile.
	m480 := MatchRelease(p, ParsedRelease{Height: 480, Codec: CodecH264})
	if m480.Matched {
		t.Errorf("480p should not match HD-1080p profile")
	}
}

func TestMatchRelease_CodecConstraint(t *testing.T) {
	p, _ := ProfileByName("Full-1080p")

	// An h264 1080p release matches the h264 item.
	m := MatchRelease(p, ParsedRelease{Height: 1080, Codec: CodecH264})
	if !m.Matched {
		t.Fatalf("h264 1080p should match Full-1080p")
	}
	// The h264 item (rank 0) must be preferred over h265 (rank 1) — both
	// match; the best (lowest rank) wins.
	if m.Item.Quality != "1080p" {
		t.Errorf("matched item quality = %q, want 1080p", m.Item.Quality)
	}
}

func TestMatchRelease_AnyProfile(t *testing.T) {
	p, _ := ProfileByName("Any")
	for _, h := range []int{0, 480, 720, 1080, 2160} {
		m := MatchRelease(p, ParsedRelease{Height: h})
		if !m.Matched {
			t.Errorf("height %d should match Any profile", h)
		}
	}
}

func TestSortReleases_BestFirst(t *testing.T) {
	p, _ := ProfileByName("HD-1080p")
	cands := []string{
		"Movie.2020.480p",       // 480p — below cutoff, won't match
		"Movie.2020.720p.x264",  // 720p
		"Movie.2020.1080p.x265", // 1080p
		"Movie.2020.2160p.x265", // 2160p
	}
	var matches []ProfileMatch
	for _, c := range cands {
		matches = append(matches, MatchRelease(p, ParseRelease(c)))
	}
	SortReleases(matches)
	// The best (2160p) should come first among matched; 480p last (unmatched).
	if !matches[0].Matched {
		t.Fatalf("best candidate should be matched")
	}
	if matches[0].Score <= matches[1].Score {
		t.Errorf("scores not descending: %d <= %d", matches[0].Score, matches[1].Score)
	}
}

func TestBuiltInProfiles_HaveItems(t *testing.T) {
	for _, p := range BuiltInProfiles() {
		if len(p.Items) == 0 {
			t.Errorf("profile %q has no items", p.Name)
		}
	}
}
