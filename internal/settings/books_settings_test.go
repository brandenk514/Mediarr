package settings

import (
	"strings"
	"testing"
)

// B5: the factory-default book media-management + metadata documents validate
// end-to-end (the book blocks are now part of the shared section documents).
func TestB5_DefaultsValidate(t *testing.T) {
	if err := DefaultMediaManagement().Validate(); err != "" {
		t.Errorf("default media-management must validate: %q", err)
	}
	if err := DefaultMetadata().Validate(); err != "" {
		t.Errorf("default metadata must validate: %q", err)
	}
}

func TestB5_DefaultBookNaming(t *testing.T) {
	bn := DefaultBookNaming()
	if !bn.RenameBooks || !bn.ReplaceIllegalCharacters {
		t.Errorf("expected rename + replace-illegal on by default: %+v", bn)
	}
	if bn.StandardFormat != DefaultBookStandardFormat {
		t.Errorf("standard format = %q, want %q", bn.StandardFormat, DefaultBookStandardFormat)
	}
	if bn.AuthorFormat != DefaultBookAuthorFormat {
		t.Errorf("author format = %q, want %q", bn.AuthorFormat, DefaultBookAuthorFormat)
	}
	if bn.SpaceHandling != "space" || bn.CaseHandling != "default" {
		t.Errorf("space/case handling = %q/%q, want space/default", bn.SpaceHandling, bn.CaseHandling)
	}
}

func TestB5_BookNamingValidation(t *testing.T) {
	cases := []struct {
		name string
		fn   func() BookNaming
		want string
	}{
		{"empty standard format", func() BookNaming { b := DefaultBookNaming(); b.StandardFormat = ""; return b }, "standard_format is required"},
		{"empty author format", func() BookNaming { b := DefaultBookNaming(); b.AuthorFormat = ""; return b }, "author_format is required"},
		{"bad space handling", func() BookNaming { b := DefaultBookNaming(); b.SpaceHandling = "camel"; return b }, "space_handling"},
		{"bad case handling", func() BookNaming { b := DefaultBookNaming(); b.CaseHandling = "title"; return b }, "case_handling"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn().Validate(); !strings.Contains(got, tc.want) {
				t.Errorf("want error containing %q, got %q", tc.want, got)
			}
		})
	}
	if got := DefaultBookNaming().Validate(); got != "" {
		t.Errorf("valid book naming must not error: %q", got)
	}
}

func TestB5_BookManagementValidation(t *testing.T) {
	cases := []struct {
		name string
		fn   func() BookFileManagement
		want string
	}{
		{"bad propers", func() BookFileManagement {
			m := DefaultBookManagement()
			m.DownloadPropersAndRepacks = "maybe"
			return m
		}, "download_propers_and_repacks"},
		{"bad file date", func() BookFileManagement { m := DefaultBookManagement(); m.FileDate = "now"; return m }, "file_date"},
		{"bad rescan", func() BookFileManagement { m := DefaultBookManagement(); m.RescanAfterRefresh = "sometimes"; return m }, "rescan_after_refresh"},
		{"bad fingerprint", func() BookFileManagement { m := DefaultBookManagement(); m.AllowFingerprinting = "kinda"; return m }, "allow_fingerprinting"},
		{"negative recycle days", func() BookFileManagement { m := DefaultBookManagement(); m.RecycleBinCleanupDays = -1; return m }, "recycle_bin_cleanup_days"},
		{"negative free space", func() BookFileManagement {
			m := DefaultBookManagement()
			m.MinimumFreeSpaceWhenImportingGB = -1
			return m
		}, "min_free_space_gb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn().Validate(); !strings.Contains(got, tc.want) {
				t.Errorf("want error containing %q, got %q", tc.want, got)
			}
		})
	}
	if got := DefaultBookManagement().Validate(); got != "" {
		t.Errorf("valid book management must not error: %q", got)
	}
}

func TestB5_CalibreValidation(t *testing.T) {
	// Disabled is always legal (a half-configured but off root).
	if got := DefaultCalibre().Validate(); got != "" {
		t.Errorf("disabled calibre must validate: %q", got)
	}

	en := func(mutate func(*CalibreSettings)) CalibreSettings {
		c := DefaultCalibre()
		c.Enabled = true
		c.Host, c.Port, c.Username = "calibre.example", 8080, "user"
		mutate(&c)
		return c
	}

	if got := en(func(*CalibreSettings) {}).Validate(); got != "" {
		t.Errorf("valid enabled calibre must validate: %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.Host = "" }).Validate(); !strings.Contains(got, "host") {
		t.Errorf("missing host: got %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.Port = 0 }).Validate(); !strings.Contains(got, "port") {
		t.Errorf("port 0: got %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.Port = 70000 }).Validate(); !strings.Contains(got, "port") {
		t.Errorf("port 70000: got %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.Username = "" }).Validate(); !strings.Contains(got, "username") {
		t.Errorf("missing username: got %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.OutputFormat = "PDF" }).Validate(); !strings.Contains(got, "output format") {
		t.Errorf("bad format: got %q", got)
	}
	if got := en(func(c *CalibreSettings) { c.OutputFormat = "MOBI,EPUB" }).Validate(); got != "" {
		t.Errorf("valid multi-format should pass, got %q", got)
	}
}

func TestB5_CalibreConvertFormats(t *testing.T) {
	for _, want := range []string{"MOBI", "EPUB", "AZW3", "DOCX", "FB2"} {
		if !containsString(CalibreConvertFormats, want) {
			t.Errorf("CalibreConvertFormats must include %q", want)
		}
	}
}

func TestB5_CalibreHardlinksDisabled(t *testing.T) {
	off := DefaultCalibre()
	if off.HardlinksDisabled() {
		t.Errorf("disabled calibre must not disable hard links")
	}
	on := DefaultCalibre()
	on.Enabled = true
	if !on.HardlinksDisabled() {
		t.Errorf("enabled calibre must disable hard links (Calibre's requirement)")
	}
}

func TestB5_BookTagWriteValidation(t *testing.T) {
	if got := DefaultBookTagWrite().Validate(); got != "" {
		t.Errorf("default book tag write must validate: %q", got)
	}
	if got := (BookTagWrite{BookTagType: "all", AudioTagType: "bogus"}).Validate(); !strings.Contains(got, "audio_tag_type") {
		t.Errorf("bad audio tag type: got %q", got)
	}
	if got := (BookTagWrite{BookTagType: "bogus", AudioTagType: "no"}).Validate(); !strings.Contains(got, "book_tag_type") {
		t.Errorf("bad book tag type: got %q", got)
	}
}
