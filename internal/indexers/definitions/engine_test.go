package definitions

import (
	"reflect"
	"testing"
)

func testDef() *Definition {
	return &Definition{
		ID:   "test",
		Name: "Test",
		Fields: []Field{
			{Name: "baseurl", Type: FieldTypeTextbox, Required: true},
			{Name: "apikey", Type: FieldTypePassword},
			{Name: "port", Type: FieldTypeNumber, Default: float64(8080)},
			{Name: "rss", Type: FieldTypeCheckbox, Default: false},
			{Name: "quality", Type: FieldTypeSelect,
				Options: map[string]string{"1080p": "1080p", "720p": "720p"}},
			{Name: "notes", Type: FieldTypeSelect, Default: "1080p",
				Options: map[string]string{"1080p": "1080p", "720p": "720p"}},
			{Name: "hint", Type: FieldTypeInfo, Label: "Some hint"},
		},
	}
}

func TestResolve_HappyPath(t *testing.T) {
	d := testDef()
	in := map[string]any{
		"baseurl": "https://example.com",
		"apikey":  "secret",
		"port":    "9090", // string number is accepted and normalized
		"rss":     "true", // string bool is accepted and normalized
		"quality": "720p",
	}
	out, err := Resolve(d, in)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := map[string]any{
		"baseurl": "https://example.com",
		"apikey":  "secret",
		"port":    float64(9090),
		"rss":     true,
		"quality": "720p",
		"notes":   "1080p", // default applied
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("Resolve =\n%v\nwant\n%v", out, want)
	}
	// Input must not be mutated.
	if in["port"] != "9090" {
		t.Error("Resolve mutated its input")
	}
}

func TestResolve_NilValues(t *testing.T) {
	out, err := Resolve(testDef(), nil)
	if err == nil {
		t.Fatal("expected required-baseurl error for nil values")
	}
	if !IsValidationError(err) {
		t.Errorf("expected ValidationError, got %T: %v", err, err)
	}
	_ = out
}

func TestResolve_MissingRequiredBaseURL(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"quality": "720p"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestResolve_EmptyRequiredBaseURL(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": ""})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for empty required baseurl, got %v", err)
	}
}

func TestResolve_UnknownField(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "bogus": "1"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for unknown field, got %v", err)
	}
}

func TestResolve_InfoFieldIsNotAnInput(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "hint": "nope"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for info field, got %v", err)
	}
}

func TestResolve_BadNumber(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "port": "not-a-number"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for bad number, got %v", err)
	}
}

func TestResolve_BadCheckbox(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "rss": "maybe"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for bad checkbox, got %v", err)
	}
	_, err = Resolve(testDef(), map[string]any{"baseurl": "x", "rss": float64(1)})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for numeric checkbox, got %v", err)
	}
}

func TestResolve_SelectOutOfOptions(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "quality": "480p"})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for out-of-option select, got %v", err)
	}
}

func TestResolve_SelectNonString(t *testing.T) {
	_, err := Resolve(testDef(), map[string]any{"baseurl": "x", "quality": float64(1)})
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError for numeric select, got %v", err)
	}
}

func TestMaskSecrets(t *testing.T) {
	d := testDef()
	values := map[string]any{
		"baseurl": "https://example.com",
		"apikey":  "super-secret",
		"port":    float64(9090),
	}
	masked := MaskSecrets(d, values)
	if masked["apikey"] != "" {
		t.Errorf("apikey should be masked, got %q", masked["apikey"])
	}
	if masked["baseurl"] != "https://example.com" {
		t.Error("non-secret field must pass through")
	}
	// Original must be untouched.
	if values["apikey"] != "super-secret" {
		t.Error("MaskSecrets must not mutate its input")
	}
	// Nil values yields an empty (non-nil) object.
	if got := MaskSecrets(d, nil); len(got) != 0 {
		t.Errorf("MaskSecrets(nil) = %v, want empty", got)
	}
	// Missing secret key stays missing (nothing to mask).
	got := MaskSecrets(d, map[string]any{"baseurl": "x"})
	if _, ok := got["apikey"]; ok {
		t.Error("MaskSecrets must not invent missing secret keys")
	}
}

func TestSecretFieldNames(t *testing.T) {
	got := testDef().SecretFieldNames()
	if !reflect.DeepEqual(got, []string{"apikey"}) {
		t.Errorf("SecretFieldNames = %v, want [apikey]", got)
	}
}

func TestResolve_RealSeedDefinition(t *testing.T) {
	c := Default()
	d := c.GetByID("1337x")
	if d == nil {
		t.Skip("1337x not in seed")
	}
	// 1337x's contract has no baseurl input; all values must be contract
	// fields or it must fail with a validation error.
	ok := true
	for i := range d.Fields {
		if d.Fields[i].Name == "baseurl" {
			ok = false
		}
	}
	if ok {
		if _, err := Resolve(d, map[string]any{}); err != nil {
			t.Errorf("empty config should be valid for 1337x (no required fields), got %v", err)
		}
	}
}
