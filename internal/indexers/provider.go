// Package indexers is the Prowlarr-equivalent indexer layer (PLAN §7).
//
// It is deliberately the narrow surface every media pipeline programs against:
// a one-method Searcher interface (search.go) plus, for M5 (#30), the provider
// framework that sits *behind* that interface. Real protocol adapters
// (Torznab, TorrentRSS, usenet NZB) land in #31-33 and register themselves with
// the Registry so the rest of the codebase never learns their specifics.
//
// Dependency rule (PLAN §3, "outermost"): this package may import net/http and
// third-party HTTP clients, but no service or domain package may import an
// adapter directly — they depend on Searcher / Provider, the seam this package
// owns.
package indexers

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Provider is the full surface a real indexer adapter implements. It extends
// Searcher with the lifecycle the framework needs: a stable name, a protocol
// kind (for the definition store), a health probe (for #34), and the ability to
// build itself from a persisted Definition (for the factory).
//
// The fake adapter (fake.go) satisfies Searcher only — it is the reference
// implementation the M1/M2 pipelines and tests still use, and it never leaves
// the test/dev path. Real adapters implement Provider.
type Provider interface {
	Searcher
	// Kind returns the protocol kind (e.g. "torznab", "torrent-rss", "nzb").
	// It must match the registry key used to construct the adapter.
	Kind() string
	// Test performs a live health probe and reports whether the indexer is
	// reachable and authenticating. It must be cheap (a single request) so it
	// can run on a schedule (#34).
	Test(ctx context.Context) error
}

// Definition is one configured indexer as stored in the indexers table
// (migration 0007). It is the value object the repository hands to the factory
// to build a Provider.
//
// APIKey holds the *decrypted* key. The repository is the only layer that
// encrypts/decrypts (PLAN §7: key from env, never logged); everything else in
// this package sees the plaintext key exactly as the adapter needs it and is
// responsible for not logging it.
type Definition struct {
	ID           int64
	Name         string
	Kind         string
	BaseURL      string
	APIKey       string
	SettingsJSON string
	Enabled      bool
	LastTest     *time.Time
}

// Registry maps a protocol kind to a constructor that builds a Provider from a
// Definition. It is the M5 equivalent of Prowlarr's "indexer definitions as
// code + user config": the definitions are the user's rows in the indexers
// table, and the registry is the code that knows how to turn each kind into a
// working adapter.
//
// The zero Registry knows no adapters. Real adapters (#31-33) call Register at
// init (or be wired in main). The fake is intentionally NOT registered by
// default so a misconfiguration cannot silently fall back to fake data in
// production.
type Registry struct {
	constructors map[string]func(def Definition) (Provider, error)
	// Deps are the shared AdapterDeps (HTTP client + health tracker) handed to
	// every real adapter at Build time. A single shared HealthTracker is what
	// makes the per-indexer stats endpoint (#35) reflect the health of the
	// *live* search path, not just the on-demand Test probe: both funnel into
	// the same tracker. A zero Deps uses the AdapterDeps defaults (a fresh
	// 15s client and a private tracker per adapter), which is correct for the
	// reference/dev registry and for unit tests that build adapters directly.
	Deps AdapterDeps
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{constructors: make(map[string]func(Definition) (Provider, error))}
}

// NewDefaultRegistry returns a registry pre-registered with the adapters this
// build knows how to construct. The "fake" kind is the reference Provider
// (PLAN §7, #30 — "the fake adapter becomes the reference implementation"), so
// a definition of kind "fake" materializes into a working adapter with no
// network I/O. The real protocol adapters registered here are Torznab (#31),
// TorrentRSS (#32), and usenet NZB (#33); adding a new adapter means
// registering it here so the running build can materialize any configured kind
// without a schema change.
//
// Note: NewDefaultRegistry is the dev/reference composition. A production
// deployment that does not want to risk a "fake" definition silently producing
// fake results can instead start from NewRegistry() and register only the real
// adapters it has been compiled with.
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register("fake", func(def Definition) (Provider, error) {
		return NewFakeIndexer(def.Name), nil
	})
	RegisterTorznab(r)
	RegisterTorrentRSS(r)
	RegisterNZB(r)
	return r
}

// Register associates a protocol kind with its constructor. Registering the
// same kind twice panics — a duplicated kind is a wiring bug, not a runtime
// condition to absorb silently.
func (r *Registry) Register(kind string, ctor func(def Definition) (Provider, error)) {
	if _, exists := r.constructors[kind]; exists {
		panic(fmt.Sprintf("indexers: adapter kind %q registered twice", kind))
	}
	r.constructors[kind] = ctor
}

// Kinds returns the registered protocol kinds, sorted, for deterministic
// reporting (e.g. /health or an admin endpoint).
func (r *Registry) Kinds() []string {
	kinds := make([]string, 0, len(r.constructors))
	for k := range r.constructors {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

// Lookup reports whether a constructor is registered for the kind.
func (r *Registry) Lookup(kind string) bool {
	_, ok := r.constructors[kind]
	return ok
}

// Build constructs a Provider for the given definition using the registered
// constructor. It returns an error (not a panic) for an unregistered kind so a
// persisted definition referencing a kind the running build doesn't have
// degrades to a clear, loggable error rather than crashing the service.
func (r *Registry) Build(def Definition) (Provider, error) {
	ctor, ok := r.constructors[def.Kind]
	if !ok {
		return nil, fmt.Errorf("indexers: no adapter registered for kind %q (name %q)", def.Kind, def.Name)
	}
	p, err := ctor(def)
	if err != nil {
		return nil, fmt.Errorf("indexers: build %s (%q): %w", def.Kind, def.Name, err)
	}
	return p, nil
}
