// Package downloadclients is the pure domain layer for the saved download
// clients (Prowlarr PR5). It holds the value types the settings page and the
// grab dispatch program against: the client record and the Repo seam the
// REST layer and the Postgres adapter implement.
//
// Like internal/notify, this package does no I/O. Secrets cross the Repo
// boundary in *plaintext* in Record.APIKey, exactly as indexer API keys cross
// indexers.IndexerRepo: the Postgres repository (internal/postgres/
// download_clients_repo.go) is the only layer that encrypts them at rest with
// the app vault and decrypts on read, and the REST layer never echoes the key
// back (it reports only api_key_set, like the indexer list endpoint).
package downloadclients

import (
	"context"
	"time"
)

// Kind names one saved download-client implementation (Prowlarr PR5,
// migration 0026). The values are the OpenAPI wire enum.
type Kind string

const (
	// KindQB is the qBittorrent Web API client.
	KindQB Kind = "qbittorrent"
	// KindSAB is the SABnzbd JSON API client.
	KindSAB Kind = "sabnzbd"
	// KindTransmission is the Transmission RPC client.
	KindTransmission Kind = "transmission"
)

// AllKinds is the fixed, ordered set of supported client kinds.
var AllKinds = []Kind{KindQB, KindSAB, KindTransmission}

// Valid reports whether k is a supported client kind.
func (k Kind) Valid() bool {
	for _, v := range AllKinds {
		if v == k {
			return true
		}
	}
	return false
}

// String is the wire form (e.g. "qbittorrent").
func (k Kind) String() string { return string(k) }

// HandlesProtocol reports whether this client kind can handle a release
// protocol for grab routing (the indexers.Protocol* constant values):
// qBittorrent and Transmission take "torrent" and "torrent-file" releases,
// SABnzbd takes "nzb".
func (k Kind) HandlesProtocol(protocol string) bool {
	switch k {
	case KindQB, KindTransmission:
		return protocol == "torrent" || protocol == "torrent-file"
	case KindSAB:
		return protocol == "nzb"
	default:
		return false
	}
}

// Record is one saved download client crossing the Repo boundary. APIKey is
// the *decrypted* key (the repository is the only ciphertext toucher).
type Record struct {
	ID          int64
	Name        string
	Kind        Kind
	Enabled     bool
	Position    int
	BaseURL     string
	Username    string
	APIKey      string
	CompleteDir string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repo persists saved download clients (Prowlarr PR5). List returns the rows
// ordered by position (ascending) then id (ascending) — the display and
// default-routing order. Get/Update/Delete return sql.ErrNoRows for an
// unknown id so the REST layer can map it to a 404.
type Repo interface {
	// Create stores r and returns the DB-assigned id and created_at.
	Create(ctx context.Context, r Record) (id int64, createdAt time.Time, err error)
	// Get returns the client with id, or sql.ErrNoRows.
	Get(ctx context.Context, id int64) (*Record, error)
	// List returns every saved client in display order.
	List(ctx context.Context) ([]Record, error)
	// GetByName returns the client named name, or sql.ErrNoRows when none.
	// Names are the migration/import identity ("qBittorrent" / "SABnzbd").
	GetByName(ctx context.Context, name string) (*Record, error)
	// Update fully replaces the row (callers merge kept secrets before
	// calling). sql.ErrNoRows when no row has id.
	Update(ctx context.Context, r Record) error
	// Delete removes the client. sql.ErrNoRows when no row has id.
	Delete(ctx context.Context, id int64) error
}
