// Integration tests for the Redis-backed store.Cache. They require a running
// Docker daemon (testcontainers) and spin up a real redis:7 container per test.
// The pure hit/miss / failure-isolation logic of indexers.CacheSearcher is unit
// tested with an in-memory cache; these tests prove the real go-redis Set/Get/
// TTL/Delete paths the production cache uses.
package redis

import (
	"context"
	"net"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newTestRedis boots a redis container and returns a connected *goredis.Client.
func newTestRedis(t *testing.T) *goredis.Client {
	t.Helper()
	ctx := context.Background()

	c, err := testcontainers.Run(ctx,
		"redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("6379/tcp").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	ip, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("host ip: %v", err)
	}
	p, err := c.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	addr := net.JoinHostPort(ip, p.Port())

	rdb := goredis.NewClient(&goredis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })

	// The redis image opens its port before the server is ready to answer PING.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := rdb.Ping(ctx).Err(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("redis ping: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return rdb
}

func TestCache_RealRedis_RoundTrip(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()

	cl, err := New("redis://" + addrOf(rdb))
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	defer cl.Close()
	cache := NewCache(cl)

	// Miss on an empty key.
	if v, ok := cache.Get(ctx, "nope"); ok || v != "" {
		t.Fatalf("expected miss, got ok=%v v=%q", ok, v)
	}

	// Set then Get.
	if err := cache.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok := cache.Get(ctx, "k")
	if !ok || got != "v" {
		t.Fatalf("get = %q ok=%v, want %q true", got, ok, "v")
	}

	// Delete then miss.
	if err := cache.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if v, ok := cache.Get(ctx, "k"); ok {
		t.Fatalf("expected miss after delete, got %q", v)
	}

	// Ping.
	if err := cache.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestCache_RealRedis_TTLExpires(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()

	cl, _ := New("redis://" + addrOf(rdb))
	defer cl.Close()
	cache := NewCache(cl)

	if err := cache.Set(ctx, "eph", "x", 300*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, ok := cache.Get(ctx, "eph"); !ok {
		t.Fatal("expected present immediately after set")
	}
	time.Sleep(600 * time.Millisecond)
	if v, ok := cache.Get(ctx, "eph"); ok {
		t.Fatalf("expected expiry after TTL, got %q", v)
	}
}

// addrOf recovers the host:port a go-redis client is pointed at, so the test can
// build a fresh Client (redis.New) for the store.Cache under test.
func addrOf(rdb *goredis.Client) string {
	return rdb.Options().Addr
}
