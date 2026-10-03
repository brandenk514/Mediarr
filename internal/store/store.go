// Package store defines the data-access interfaces the rest of the service
// programs against. Concrete implementations live in the postgres and redis
// sub-packages; tests can satisfy these interfaces with fakes.
//
// Dependency rule: domain and service packages depend on store interfaces,
// never on *sql.DB or *redis.Client directly.
package store

import (
	"context"
	"time"
)

// Store is the durable persistence surface (Postgres).
type Store interface {
	// Migrate applies any pending migrations. Safe to call on every boot.
	Migrate(ctx context.Context) error
	// Ping verifies a healthy round-trip to the database.
	Ping(ctx context.Context) error
	// Version reports the applied schema version (e.g. "0001").
	Version(ctx context.Context) (string, error)
}

// Cache is the Redis-backed key/value cache. Values are re-derivable from
// Postgres — losing them must never lose data.
type Cache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
}

// Job is a unit of work dispatched to a worker via the queue.
type Job struct {
	// Type is the worker that consumes this job (e.g. "scan", "import").
	Type string
	// Payload is the JSON body the worker expects.
	Payload []byte
	// At is when the job becomes available (default: now).
	At time.Time
}

// Queue is the Redis Streams job queue with consumer groups.
type Queue interface {
	// Enqueue adds a job to a named stream.
	Enqueue(ctx context.Context, stream string, job Job) (string, error)
	// Ack confirms processing of a job message ID.
	Ack(ctx context.Context, stream, group, messageID string) error
	// Close releases the consumer.
	Close() error
}

// Event is a domain event published to subscribers (WebSocket push, metrics).
type Event struct {
	// Type is a stable event name, e.g. "queue.updated".
	Type string
	// Payload is the JSON body.
	Payload []byte
}

// Events is the Redis pub/sub bus.
type Events interface {
	// Publish sends an event to all subscribers of the event's type.
	Publish(ctx context.Context, ev Event) error
	// Subscribe opens a subscription channel for the given event types.
	// The returned channel is closed by the implementation; callers must
	// drain it to avoid blocking.
	Subscribe(ctx context.Context, types ...string) (<-chan Event, error)
	Ping(ctx context.Context) error
}
