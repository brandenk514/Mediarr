// Package settings is the pure domain layer for the settings page (#44). It
// holds the value types the UI reads and writes — the per-library overrides
// — plus the Store seam the REST layer and the Postgres adapter program
// against.
//
// Like internal/queue, this package does no I/O: the Postgres Store
// implementation (internal/postgres) is the only concrete one. It is the
// single place that knows the *shape* of a settings document, so the API and
// the UI agree on field names and defaults without either touching SQL.
//
// Download clients moved to their own domain package in Prowlarr PR5
// (internal/downloadclients): the PR5 migration seeds the legacy single
// client document the Postgres Store used to hold into the new plural
// download_clients table. Secrets cross the downloadclients.Repo boundary in
// plaintext (like indexer API keys) and are encrypted at rest by the Postgres
// repo with the app vault (PLAN §7); the REST layer reports only masked
// presence (api_key_set), never the key.
package settings

import (
	"context"
	"fmt"
)

// LibraryKind names one of the four libraries. It is the canonical singular
// form, matching the queue/dashboard Kind so a library override can be keyed
// by the same token the rest of the system uses.
type LibraryKind string

const (
	LibraryMovie LibraryKind = "movie"
	LibraryTV    LibraryKind = "tv"
	LibraryMusic LibraryKind = "music"
	LibraryBook  LibraryKind = "book"
)

// LibraryKinds is the fixed, ordered set of libraries the UI iterates.
var LibraryKinds = []LibraryKind{LibraryMovie, LibraryTV, LibraryMusic, LibraryBook}

// IsValid reports whether k is one of the four libraries.
func (k LibraryKind) IsValid() bool {
	for _, v := range LibraryKinds {
		if v == k {
			return true
		}
	}
	return false
}

// String is the wire form (e.g. "movie").
func (k LibraryKind) String() string { return string(k) }

// LibraryOverride is a per-library override (the #44 "per-library overrides:
// paths, quality, checksum opt-out"). All fields are optional: an empty value
// means "use the system default".
//
// Path is an override for the library's root directory (empty = the global
// media root). QualityProfile is a quality-profile name (empty = the default
// profile). ChecksumVerify is a tristate: nil means "use the default" (audio
// checksum verify is ON by default, PLAN §8), a pointer to false is an
// explicit opt-out, a pointer to true is an explicit opt-in.
type LibraryOverride struct {
	Path           string `json:"path"`
	QualityProfile string `json:"quality_profile"`
	// ChecksumVerify is nil unless the user explicitly set it.
	ChecksumVerify *bool `json:"checksum_verify,omitempty"`
}

// IsZero reports whether the override carries no overrides at all.
func (o *LibraryOverride) IsZero() bool {
	if o == nil {
		return true
	}
	return o.Path == "" && o.QualityProfile == "" && o.ChecksumVerify == nil
}

// Store is the persistence surface for the settings documents. GetX returns
// a non-nil document with empty/default values when nothing has been saved
// (so the UI always has something to render); PutX upserts. The Section
// document methods (P5) are the generic key/value surface: the stored value
// is the raw section JSON, and Get returns nil bytes when the section has
// never been saved (the API layer merges defaults over nil).
type Store interface {
	// GetLibrary returns the saved override for k, or a zero override when none
	// has been saved.
	GetLibrary(ctx context.Context, k LibraryKind) (*LibraryOverride, error)
	// PutLibrary upserts the override for k. A zero override clears it.
	PutLibrary(ctx context.Context, k LibraryKind, o *LibraryOverride) error
	// GetSectionDocument returns the stored JSON for section s, or nil (no
	// error) when the section has never been saved.
	GetSectionDocument(ctx context.Context, s Section) ([]byte, error)
	// PutSectionDocument upserts the stored JSON for section s.
	PutSectionDocument(ctx context.Context, s Section, doc []byte) error
}

// ErrInvalidLibraryKind is returned when a Store method is called with a kind
// that is not one of the four libraries.
var ErrInvalidLibraryKind = fmt.Errorf("settings: unknown library kind")
