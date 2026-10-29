package notify

import (
	"context"
	"time"
)

// SinkRecord is one saved notification sink crossing the repository
// boundary (Prowlarr PR4, migration 0023). Token is PLAINTEXT here: the
// repository layer (internal/postgres/notifications_repo.go) is the only
// layer that encrypts / decrypts it at rest, mirroring the indexer
// api_key convention.
type SinkRecord struct {
	ID        int64
	Name      string
	Kind      SinkKind
	Enabled   bool
	URL       string
	SectionID string
	Token     string
	UserAgent string
	CreatedAt time.Time
}

// SinkRepo persists saved notification sinks. List returns rows in creation
// order (id ascending). Get returns sql.ErrNoRows for an unknown id so
// callers can map it to a 404 with errors.Is.
type SinkRepo interface {
	// Create stores r and returns the DB-assigned id and created_at.
	Create(ctx context.Context, r SinkRecord) (id int64, createdAt time.Time, err error)
	Get(ctx context.Context, id int64) (*SinkRecord, error)
	List(ctx context.Context) ([]SinkRecord, error)
	// Update fully replaces the row (the caller merges kept secrets before
	// calling).
	Update(ctx context.Context, r SinkRecord) error
	Delete(ctx context.Context, id int64) error
}
