package indexers

import (
	"context"
	"time"
)

// IndexerRepo is the persistence surface for indexer definitions (the
// "indexer definition store" in PLAN §4 / #30). It is the seam the REST layer
// and the provider factory program against; the Postgres implementation
// (internal/postgres) is the only concrete one.
//
// API keys cross this boundary in *plaintext* in the Definition value object.
// The repository is the single layer that encrypts at rest (PLAN §7) and
// decrypts on read; nothing above it ever sees ciphertext, and nothing below
// it (raw SQL) is exposed. This keeps "encrypted at rest" a property of one
// place, not scattered across call sites.
type IndexerRepo interface {
	// Create inserts a new indexer definition and returns its id.
	Create(ctx context.Context, def Definition) (int64, error)
	// Get loads a definition by id, decrypting its API key.
	Get(ctx context.Context, id int64) (*Definition, error)
	// GetByName loads a definition by its unique name, decrypting its key.
	GetByName(ctx context.Context, name string) (*Definition, error)
	// List returns all definitions, ordered by id.
	List(ctx context.Context) ([]Definition, error)
	// ListEnabled returns only enabled definitions, ordered by id. This is the
	// set the search fan-out is built from.
	ListEnabled(ctx context.Context) ([]Definition, error)
	// Update replaces a definition's mutable fields (name/kind/base_url/key/
	// settings). It re-encrypts the API key.
	Update(ctx context.Context, def Definition) error
	// Delete removes a definition by id.
	Delete(ctx context.Context, id int64) error
	// SetEnabled toggles a definition's enabled flag.
	SetEnabled(ctx context.Context, id int64, enabled bool) error
	// SetLastTest records the timestamp of a health probe (for #34). A nil time
	// clears it.
	SetLastTest(ctx context.Context, id int64, at *time.Time) error
}
