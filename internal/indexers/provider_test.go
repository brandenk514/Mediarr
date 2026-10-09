package indexers

import (
	"context"
	"testing"
)

// --- Registry ---------------------------------------------------------------

// stubProvider is a minimal Provider for registry tests.
type stubProvider struct {
	def  Definition
	kind string
}

func (p *stubProvider) Name() string                   { return p.def.Name }
func (p *stubProvider) Kind() string                   { return p.kind }
func (p *stubProvider) Test(ctx context.Context) error { return nil }
func (p *stubProvider) Search(ctx context.Context, q SearchQuery) ([]SearchResult, error) {
	return nil, nil
}

func TestRegistry_RegisterAndBuild(t *testing.T) {
	r := NewRegistry()
	r.Register("torznab", func(def Definition) (Provider, error) {
		return &stubProvider{def: def, kind: "torznab"}, nil
	})
	if !r.Lookup("torznab") {
		t.Fatal("Lookup(torznab) = false, want true")
	}
	if r.Lookup("nope") {
		t.Fatal("Lookup(nope) = true, want false")
	}

	def := Definition{Name: "nyaa", Kind: "torznab", BaseURL: "https://x", APIKey: "k"}
	p, err := r.Build(def)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if p.Name() != "nyaa" || p.Kind() != "torznab" {
		t.Errorf("built provider = (%q,%q), want (nyaa, torznab)", p.Name(), p.Kind())
	}
	// The constructed provider received the definition (key in hand, plaintext).
	if sp, ok := p.(*stubProvider); !ok || sp.def.APIKey != "k" {
		t.Errorf("provider did not receive the definition's API key: %+v", sp)
	}
}

func TestRegistry_Build_UnregisteredKind_Errors(t *testing.T) {
	r := NewRegistry()
	_, err := r.Build(Definition{Name: "x", Kind: "bogus"})
	if err == nil {
		t.Fatal("expected an error building an unregistered kind, got nil")
	}
}

func TestRegistry_Register_DuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic on duplicate Register, got none")
		}
	}()
	r := NewRegistry()
	r.Register("k", func(Definition) (Provider, error) { return nil, nil })
	r.Register("k", func(Definition) (Provider, error) { return nil, nil })
}

func TestRegistry_Build_ConstructorErrorWraps(t *testing.T) {
	r := NewRegistry()
	r.Register("bad", func(def Definition) (Provider, error) {
		return nil, &buildErr{"config invalid"}
	})
	_, err := r.Build(Definition{Name: "n", Kind: "bad"})
	if err == nil {
		t.Fatal("expected constructor error to propagate")
	}
}

type buildErr struct{ msg string }

func (e *buildErr) Error() string { return e.msg }

func TestRegistry_Kinds_Sorted(t *testing.T) {
	r := NewRegistry()
	for _, k := range []string{"z", "a", "m"} {
		r.Register(k, func(Definition) (Provider, error) { return nil, nil })
	}
	got := r.Kinds()
	want := []string{"a", "m", "z"}
	if len(got) != 3 {
		t.Fatalf("Kinds = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Kinds[%d] = %q, want %q (sorted)", i, got[i], want[i])
		}
	}
}
