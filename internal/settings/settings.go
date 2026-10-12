// Package settings is the pure domain layer for the settings page (#44). It
// holds the value types the UI reads and writes — the download-client
// configuration and the per-library overrides — plus the Store seam the REST
// layer and the Postgres adapter program against.
//
// Like internal/queue, this package does no I/O: the Postgres Store
// implementation (internal/postgres) is the only concrete one. It is the
// single place that knows the *shape* of a settings document, so the API and
// the UI agree on field names and defaults without either touching SQL.
//
// Secrets (the download client's API key / password) cross the Store boundary
// in *plaintext* in the DownloadClient value, exactly as indexer API keys do
// across internal/indexers.IndexerRepo. The Postgres Store is the layer that
// encrypts them at rest with the app vault (PLAN §7) and decrypts on read;
// nothing above it ever sees ciphertext, and the REST layer never echoes them
// back (it reports only masked presence, like the indexer list endpoint).
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

// DownloadClient is the download-client configuration (the config-driven swap
// of #36/#37). The Kind field selects the implementation; the per-implementation
// fields are the same fields the factory's ClientOptions carries, so the API
// and the factory agree on names. APIKey / Password cross the Store boundary in
// plaintext and are encrypted at rest by the Postgres Store.
type DownloadClient struct {
	// Kind selects the client: "mock" (default), "qbittorrent", or "sabnzbd".
	Kind string `json:"kind"`

	// qBittorrent Web API (used when Kind == "qbittorrent").
	QBBase     string `json:"qb_base"`
	QBAPIKey   string `json:"qb_api_key"`
	QBUsername string `json:"qb_username"`
	QBPassword string `json:"qb_password"`

	// SABnzbd JSON API (used when Kind == "sabnzbd").
	SABBase        string `json:"sab_base"`
	SABAPIKey      string `json:"sab_api_key"`
	SABCompleteDir string `json:"sab_complete_dir"`
}

// ClientKinds is the fixed, ordered set of download-client implementations the
// UI offers. It mirrors the downloads.ClientKind constants without importing
// that package (keeps the pure layer dependency-free).
var ClientKinds = []string{"mock", "qbittorrent", "sabnzbd"}

// IsValidClientKind reports whether kind is a known download client.
func IsValidClientKind(kind string) bool {
	for _, k := range ClientKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Store is the persistence surface for the two settings documents. GetX
// returns a non-nil document with empty/default values when nothing has been
// saved (so the UI always has something to render); PutX upserts.
type Store interface {
	// GetDownloadClient returns the saved download-client config, or a default
	// (kind "mock", all fields empty) when none has been saved.
	GetDownloadClient(ctx context.Context) (*DownloadClient, error)
	// PutDownloadClient upserts the download-client config.
	PutDownloadClient(ctx context.Context, dc *DownloadClient) error
	// GetLibrary returns the saved override for k, or a zero override when none
	// has been saved.
	GetLibrary(ctx context.Context, k LibraryKind) (*LibraryOverride, error)
	// PutLibrary upserts the override for k. A zero override clears it.
	PutLibrary(ctx context.Context, k LibraryKind, o *LibraryOverride) error
}

// ErrInvalidLibraryKind is returned when a Store method is called with a kind
// that is not one of the four libraries.
var ErrInvalidLibraryKind = fmt.Errorf("settings: unknown library kind")

// ErrInvalidClientKind is returned when a PutDownloadClient is called with a
// kind that is not a known download client.
var ErrInvalidClientKind = fmt.Errorf("settings: unknown download client kind")
