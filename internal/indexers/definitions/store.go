package definitions

import "context"

// Store is the persistence boundary for the definition library. The Postgres
// implementation (internal/postgres.IndexerDefinitionsRepo) seeds the table
// from the embedded seed at boot and serves the list endpoint from it; tests
// substitute an in-memory implementation over the embedded catalog.
type Store interface {
	// List returns every library definition ordered by display name.
	List(ctx context.Context) ([]Definition, error)
	// Get loads one definition by id (sql.ErrNoRows equivalent when absent).
	Get(ctx context.Context, id string) (*Definition, error)
}
