// Package redis provides the Redis-backed Cache, Queue, and Events
// implementations. It is the only package that imports *redis.Client;
// everything else programs against the store interfaces.
//
// Rule: anything stored in Redis must be re-derivable from Postgres.
// Losing Redis data must never lose user data.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/brandenk514/mediarr/internal/store"
)

// Client wraps a single go-redis client for the service.
type Client struct {
	rdb *goredis.Client
}

// New creates a Redis client from a URL (redis:// or rediss://).
func New(url string) (*Client, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}
	rdb := goredis.NewClient(opts)
	return &Client{rdb: rdb}, nil
}

// RDB exposes the underlying go-redis client for use in repositories.
func (c *Client) RDB() *goredis.Client { return c.rdb }

// Close releases the client.
func (c *Client) Close() error { return c.rdb.Close() }

// Cache implements store.Cache.
type Cache struct{ c *Client }

var _ store.Cache = (*Cache)(nil)

// NewCache wraps a Client as a store.Cache.
func NewCache(c *Client) *Cache { return &Cache{c: c} }

func (c *Cache) Get(ctx context.Context, key string) (string, bool) {
	v, err := c.c.rdb.Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return v, true
}

func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 24 * time.Hour // default: one day
	}
	return c.c.rdb.Set(ctx, key, value, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.c.rdb.Del(ctx, key).Err()
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.c.rdb.Ping(ctx).Err()
}

// Queue implements store.Queue over Redis Streams.
type Queue struct {
	c     *Client
	group string // consumer group name; default "mediarr"
}

var _ store.Queue = (*Queue)(nil)

// NewQueue wraps a Client as a store.Queue.
func NewQueue(c *Client) *Queue { return &Queue{c: c, group: "mediarr"} }

func (q *Queue) Enqueue(ctx context.Context, stream string, job store.Job) (string, error) {
	// Ensure the consumer group exists before the first XADD.
	_, err := q.c.rdb.XGroupCreateMkStream(ctx, stream, q.group, "0").Result()
	if err != nil && !errors.Is(err, goredis.Nil) && !strings.Contains(err.Error(), "BUSYGROUP") {
		return "", fmt.Errorf("redis: ensure group on %s: %w", stream, err)
	}

	body, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("redis: marshal job: %w", err)
	}
	id, err := q.c.rdb.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"body": string(body)},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("redis: xadd %s: %w", stream, err)
	}
	return id, nil
}

func (q *Queue) Ack(ctx context.Context, stream, group, messageID string) error {
	return q.c.rdb.XAck(ctx, stream, group, messageID).Err()
}

func (q *Queue) Close() error { return nil }

// Events implements store.Events over Redis pub/sub.
type Events struct{ c *Client }

var _ store.Events = (*Events)(nil)

// NewEvents wraps a Client as a store.Events.
func NewEvents(c *Client) *Events { return &Events{c: c} }

func (e *Events) Publish(ctx context.Context, ev store.Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("redis: marshal event: %w", err)
	}
	return e.c.rdb.Publish(ctx, "mediarr:"+ev.Type, string(body)).Err()
}

func (e *Events) Subscribe(ctx context.Context, types ...string) (<-chan store.Event, error) {
	if len(types) == 0 {
		types = []string{"*"}
	}
	channels := make([]string, len(types))
	for i, t := range types {
		channels[i] = "mediarr:" + t
	}
	sub := e.c.rdb.Subscribe(ctx, channels...)
	out := make(chan store.Event, 64)
	go func() {
		defer close(out)
		msgCh := sub.Channel()
		for msg := range msgCh {
			var ev store.Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (e *Events) Ping(ctx context.Context) error {
	return e.c.rdb.Ping(ctx).Err()
}
