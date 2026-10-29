package indexers

import (
	"context"
	"time"
)

// Proxy is a FlareSolverr instance (Prowlarr PR4 "Indexer Proxies"): an
// anti-bot solver that indexers whose tags intersect the row's tags are
// routed through. Password is PLAINTEXT across this boundary: the
// repository layer (internal/postgres/indexer_proxies_repo.go) is the only
// layer that encrypts / decrypts it at rest, mirroring the indexer
// api_key convention.
type Proxy struct {
	ID        int64
	Name      string
	URL       string
	Username  string
	Password  string
	Tags      []string
	CreatedAt time.Time
}

// ProxyRepo persists indexer proxies. List returns rows in creation order
// (id ascending). Get returns sql.ErrNoRows for an unknown id so callers
// can map it to a 404 with errors.Is.
type ProxyRepo interface {
	// Create stores p and returns the DB-assigned id and created_at.
	Create(ctx context.Context, p Proxy) (id int64, createdAt time.Time, err error)
	Get(ctx context.Context, id int64) (*Proxy, error)
	List(ctx context.Context) ([]Proxy, error)
	// Update fully replaces the row (the caller merges kept secrets before
	// calling).
	Update(ctx context.Context, p Proxy) error
	Delete(ctx context.Context, id int64) error
}
