package settings

import (
	"encoding/json"
	"testing"
)

// TestSections_ParseSection covers the slug vocabulary: the eleven generic
// slugs parse, and the two dedicated-endpoint slugs (plus junk) do not — the
// generic route must not swallow indexers/download-clients.
func TestSections_ParseSection(t *testing.T) {
	for _, s := range AllSections {
		if got, ok := ParseSection(string(s)); !ok || got != s {
			t.Errorf("ParseSection(%q) = (%v, %v), want (%q, true)", s, got, ok, s)
		}
	}
	for _, bad := range []string{"", "indexers", "download-clients", "bogus", "GENERAL", "general "} {
		if _, ok := ParseSection(bad); ok {
			t.Errorf("ParseSection(%q) unexpectedly ok", bad)
		}
	}
}

// TestSections_Key covers the storage key contract.
func TestSections_Key(t *testing.T) {
	if got := SectionConnect.Key(); got != "section.connect" {
		t.Errorf("Key() = %q, want section.connect", got)
	}
}

// TestSections_DecodeDocument_Defaults covers the "fresh install reads
// complete documents" contract: nil bytes decode to each section's defaults.
func TestSections_DecodeDocument_Defaults(t *testing.T) {
	for _, s := range AllSections {
		doc, err := s.DecodeDocument(nil)
		if err != nil {
			t.Fatalf("%s: decode nil: %v", s, err)
		}
		msg := s.ValidateDocument(doc)
		if msg != "" {
			t.Errorf("%s: default document invalid: %s", s, msg)
		}
	}
	dg, _ := SectionGeneral.DecodeDocument(nil)
	g := dg.(*GeneralSettings)
	if g.Port != 8080 || g.Host != "0.0.0.0" || !g.AuthEnabled {
		t.Errorf("general defaults wrong: %+v", g)
	}
	du, _ := SectionUI.DecodeDocument(nil)
	ui := du.(*UISettings)
	if ui.DefaultView != "dashboard" || ui.ItemsPerPage != 50 {
		t.Errorf("ui defaults wrong: %+v", ui)
	}
	dq, _ := SectionQuality.DecodeDocument(nil)
	q := dq.(*QualitySettings)
	if len(q.Levels) != 10 || q.Levels[0].ID != 1 {
		t.Errorf("quality defaults wrong: %+v", q)
	}
	dp, _ := SectionProfiles.DecodeDocument(nil)
	p := dp.(*ProfilesSettings)
	if len(p.Profiles) != 1 || len(p.Profiles[0].Items) != 10 || p.Profiles[0].Cutoff != 10 {
		t.Errorf("profiles defaults wrong: %+v", p)
	}
	dmm, _ := SectionMediaManagement.DecodeDocument(nil)
	mm := dmm.(*MediaManagementSettings)
	if mm.Naming.Episode == "" || !mm.Management.ImportExtraFiles {
		t.Errorf("media-management defaults wrong: %+v", mm)
	}
	dms, _ := SectionMetadataSource.DecodeDocument(nil)
	ms := dms.(*MetadataSourceSettings)
	if len(ms.Sources["tv"]) != 3 || ms.Sources["tv"][0] != "tvdb" {
		t.Errorf("metadata-source defaults wrong: %+v", ms)
	}
}

// TestSections_DecodeDocument_Merge covers stored-values-over-defaults: a
// partial stored document reads with its missing fields default-filled.
func TestSections_DecodeDocument_Merge(t *testing.T) {
	stored := []byte(`{"port": 9090}`)
	docG, err := SectionGeneral.DecodeDocument(stored)
	if err != nil {
		t.Fatal(err)
	}
	got := docG.(*GeneralSettings)
	if got.Port != 9090 {
		t.Errorf("port: got %d, want 9090", got.Port)
	}
	if got.Host != "0.0.0.0" || got.BaseURL != "http://localhost:8080" {
		t.Errorf("missing fields must default: %+v", got)
	}
}

// TestSections_DecodeDocument_Corrupt covers malformed stored JSON (a 500 at
// the API, an error here).
func TestSections_DecodeDocument_Corrupt(t *testing.T) {
	if _, err := SectionGeneral.DecodeDocument([]byte(`{nope`)); err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
}

// TestSections_MaskDocument covers the read contract: connect tokens and
// import-list API keys come back blank with their *_set booleans intact, and
// sections without secrets pass through untouched.
func TestSections_MaskDocument(t *testing.T) {
	conn := &ConnectSettings{Notifications: []Notification{{
		ID: 1, Name: "n", Kind: "webhook",
		Options: NotificationOptions{URL: "https://h.example/hook", Token: "secret", TokenSet: true},
	}}}
	masked := SectionConnect.MaskDocument(conn).(*ConnectSettings)
	if masked.Notifications[0].Options.Token != "" {
		t.Error("mask must blank the token")
	}
	if !masked.Notifications[0].Options.TokenSet {
		t.Error("mask must keep token_set")
	}
	// The original must be untouched (the mask returns a copy).
	if conn.Notifications[0].Options.Token != "secret" {
		t.Error("mask must not mutate the original")
	}

	il := &ImportListsSettings{Lists: []ImportList{{
		ID: 1, Name: "l", Kind: "sonarr", URL: "https://s.example", APIKey: "k", APIKeySet: true,
	}}}
	maskedIL := SectionImportLists.MaskDocument(il).(*ImportListsSettings)
	if maskedIL.Lists[0].APIKey != "" || !maskedIL.Lists[0].APIKeySet {
		t.Errorf("import-list mask wrong: %+v", maskedIL.Lists[0])
	}

	// Non-secret sections pass through.
	g := &GeneralSettings{Port: 8080}
	if got := SectionGeneral.MaskDocument(g); got != g {
		t.Error("general must pass through unmasked")
	}
}

// TestSections_MergeSecrets covers the masked-edit round-trip: an incoming
// document with a blank secret keeps the stored secret; a fresh secret
// replaces it; a new entry (id not in the stored doc) stays secret-less.
func TestSections_MergeSecrets(t *testing.T) {
	stored := &ConnectSettings{Notifications: []Notification{
		{ID: 1, Options: NotificationOptions{Token: "stored-token"}},
		{ID: 2, Options: NotificationOptions{Token: "other"}},
	}}
	incoming := &ConnectSettings{Notifications: []Notification{
		{ID: 1, Options: NotificationOptions{Token: ""}},         // keep stored
		{ID: 2, Options: NotificationOptions{Token: "replaced"}}, // replace
		{ID: 3, Options: NotificationOptions{Token: ""}},         // new: no stored secret
	}}
	SectionConnect.MergeSecrets(stored, incoming)
	if incoming.Notifications[0].Options.Token != "stored-token" || !incoming.Notifications[0].Options.TokenSet {
		t.Errorf("id 1 should keep the stored token: %+v", incoming.Notifications[0])
	}
	if incoming.Notifications[1].Options.Token != "replaced" || !incoming.Notifications[1].Options.TokenSet {
		t.Errorf("id 2 should carry the new token: %+v", incoming.Notifications[1])
	}
	if incoming.Notifications[2].Options.Token != "" || incoming.Notifications[2].Options.TokenSet {
		t.Errorf("id 3 should stay secret-less: %+v", incoming.Notifications[2])
	}

	storedIL := &ImportListsSettings{Lists: []ImportList{{ID: 7, APIKey: "sk"}}}
	inIL := &ImportListsSettings{Lists: []ImportList{{ID: 7, APIKey: ""}}}
	SectionImportLists.MergeSecrets(storedIL, inIL)
	if inIL.Lists[0].APIKey != "sk" || !inIL.Lists[0].APIKeySet {
		t.Errorf("import-list key should be preserved: %+v", inIL.Lists[0])
	}
}

// TestSections_Validate covers the per-section validation rules (one failure
// each way through).
func TestSections_Validate(t *testing.T) {
	cases := []struct {
		section Section
		doc     any
		want    bool // wantErr
	}{
		{SectionGeneral, &GeneralSettings{Port: 0}, true},
		{SectionGeneral, &GeneralSettings{Port: 8080, Host: "h", BaseURL: "ftp://x.example"}, true},
		{SectionUI, &UISettings{DefaultView: "nope", TimeFormat: "24h", DateFormat: "YYYY", ItemsPerPage: 50}, true},
		{SectionUI, &UISettings{DefaultView: "dashboard", TimeFormat: "11h", DateFormat: "YYYY", ItemsPerPage: 50}, true},
		{SectionUI, &UISettings{DefaultView: "dashboard", TimeFormat: "24h", DateFormat: "YYYY", ItemsPerPage: 0}, true},
		{SectionQuality, &QualitySettings{Levels: []QualityLevel{{ID: 1}, {ID: 1, Name: "x"}}}, true},
		{SectionProfiles, &ProfilesSettings{Profiles: []QualityProfile{{ID: 1, Name: "p", Cutoff: 1, Items: []QualityProfileItem{{Quality: 1}, {Quality: 1}}}}}, true},
		{SectionTags, &TagsSettings{Tags: []TagInfo{{ID: 1, Label: "a"}, {ID: 2, Label: "a"}}}, true},
		{SectionMediaManagement, &MediaManagementSettings{Naming: NamingTemplate{Series: ""}, RemotePathMappings: []RemotePathMapping{}}, true},
		{SectionCustomFormats, &CustomFormatsSettings{Formats: []CustomFormat{{ID: 1, Name: "f", Rules: []CFRule{{Field: "nope", Operator: "contains", Value: "x"}}}}}, true},
		{SectionCustomFormats, &CustomFormatsSettings{Formats: []CustomFormat{{ID: 1, Name: "f", Rules: []CFRule{{Field: "title", Operator: "regex", Value: "([a"}}}}}, true},
		{SectionConnect, &ConnectSettings{Notifications: []Notification{{ID: 1, Name: "n", Kind: "slack", Options: NotificationOptions{URL: "https://x.example"}}}}, true},
		{SectionMetadata, &MetadataSettings{Language: "x", Kinds: map[string]bool{"tv": true}}, true},
		{SectionMetadataSource, &MetadataSourceSettings{Sources: map[string][]string{"tv": {"tvdb", "plex"}}}, true},
		{SectionImportLists, &ImportListsSettings{Lists: []ImportList{{ID: 1, Name: "t", Kind: "trakt", URL: "https://not-trakt.example/list"}}}, true},
		{SectionImportLists, &ImportListsSettings{Lists: []ImportList{{ID: 1, Name: "s", Kind: "sonarr", URL: "not-a-url"}}}, true},
		// A fully valid document passes.
		{SectionGeneral, DefaultGeneral(), false},
		{SectionUI, DefaultUI(), false},
		{SectionQuality, DefaultQuality(), false},
		{SectionProfiles, DefaultProfiles(), false},
		{SectionTags, DefaultTags(), false},
		{SectionMediaManagement, DefaultMediaManagement(), false},
		{SectionCustomFormats, DefaultCustomFormats(), false},
		{SectionConnect, DefaultConnect(), false},
		{SectionMetadata, DefaultMetadata(), false},
		{SectionMetadataSource, DefaultMetadataSource(), false},
		{SectionImportLists, DefaultImportLists(), false},
	}
	for i, tc := range cases {
		msg := tc.section.ValidateDocument(tc.doc)
		if (msg != "") != tc.want {
			t.Errorf("%d: ValidateDocument: got %q, wantErr %v", i, msg, tc.want)
		}
	}
}

// TestSections_EncodeDecode_RoundTrip covers the wire form: every default
// document survives a JSON round trip and validates on the other side.
func TestSections_EncodeDecode_RoundTrip(t *testing.T) {
	for _, s := range AllSections {
		original, _ := s.DecodeDocument(nil)
		b, err := s.EncodeDocument(original)
		if err != nil {
			t.Fatalf("%s: encode: %v", s, err)
		}
		var probe map[string]any
		if err := json.Unmarshal(b, &probe); err != nil {
			t.Fatalf("%s: encoded form is not a JSON object: %v", s, err)
		}
		back, err := s.DecodeDocument(b)
		if err != nil {
			t.Fatalf("%s: decode round trip: %v", s, err)
		}
		if msg := s.ValidateDocument(back); msg != "" {
			t.Errorf("%s: round-tripped document invalid: %s", s, msg)
		}
	}
}
