// Integration tests for the auth repository against a real Postgres.
//
//	postgres container is started per test (testcontainers).
package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/brandenk514/mediarr/internal/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("mediarr"),
		tcpostgres.WithUsername("mediarr"),
		tcpostgres.WithPassword("mediarr-test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Wait for the entrypoint to finish creating the user/database.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	st := postgres.NewWithDB(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewRepo(db)
}

func TestRepo_UserLifecycle(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	if n, _ := r.CountUsers(ctx); n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}

	hash, err := HashPassword("initial-pass-1234")
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.CreateUser(ctx, "admin", "", "admin", hash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero user id")
	}

	u, err := r.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !VerifyPassword("initial-pass-1234", u.PasswordHash) {
		t.Fatal("stored hash should verify")
	}
	if VerifyPassword("wrong", u.PasswordHash) {
		t.Fatal("wrong password must not verify")
	}

	// Duplicate username must fail with ErrUserExists.
	if _, err := r.CreateUser(ctx, "admin", "", "admin", hash); err != ErrUserExists {
		t.Errorf("duplicate user: err = %v, want ErrUserExists", err)
	}

	// Unknown user must fail with ErrUserNotFound.
	if _, err := r.GetUserByUsername(ctx, "ghost"); err != ErrUserNotFound {
		t.Errorf("unknown user: err = %v, want ErrUserNotFound", err)
	}

	if err := r.TouchLastLogin(ctx, id); err != nil {
		t.Fatalf("touch last login: %v", err)
	}
	u, _ = r.GetUserByUsername(ctx, "admin")
	if u.LastLoginAt == nil {
		t.Error("last_login_at should be set after touch")
	}
}

func TestRepo_TokenLifecycle(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	hash, _ := HashPassword("initial-pass-1234")
	uid, err := r.CreateUser(ctx, "admin", "", "admin", hash)
	if err != nil {
		t.Fatal(err)
	}

	tp, err := NewToken("ma_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateToken(ctx, uid, "ci", tp, nil); err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Valid token resolves to the owning user.
	u, tokenID, err := r.TokenUser(ctx, tp.Raw)
	if err != nil {
		t.Fatalf("token user: %v", err)
	}
	if u.ID != uid {
		t.Errorf("user id = %d, want %d", u.ID, uid)
	}
	if tokenID == 0 {
		t.Error("expected non-zero token id")
	}

	// Garbage token is rejected.
	if _, _, err := r.TokenUser(ctx, "not-a-token"); err != ErrInvalidToken {
		t.Errorf("garbage token: err = %v, want ErrInvalidToken", err)
	}

	// A raw token sharing the prefix but with a different secret must not
	// resolve (the stored fingerprint will not match).
	corrupt := tp.Raw[:len(tp.Raw)-1]
	if corrupt[len(corrupt)-1] == 'a' {
		corrupt = corrupt[:len(corrupt)-1] + "b"
	} else {
		corrupt = corrupt[:len(corrupt)-1] + "a"
	}
	if _, _, err := r.TokenUser(ctx, corrupt); err != ErrInvalidToken {
		t.Errorf("corrupt token: err = %v, want ErrInvalidToken", err)
	}
}

func TestRepo_TokenRevocation(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	hash, _ := HashPassword("initial-pass-1234")
	uid, _ := r.CreateUser(ctx, "admin", "", "admin", hash)

	tp, _ := NewToken("ma_")
	id, err := r.CreateToken(ctx, uid, "ci", tp, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := r.TokenUser(ctx, tp.Raw); err != nil {
		t.Fatalf("pre-revoke: %v", err)
	}

	if _, err := r.db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.TokenUser(ctx, tp.Raw); err != ErrTokenRevoked {
		t.Errorf("post-revoke: err = %v, want ErrTokenRevoked", err)
	}
}

func TestRepo_TokenExpiry(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	hash, _ := HashPassword("initial-pass-1234")
	uid, _ := r.CreateUser(ctx, "admin", "", "admin", hash)

	tp, _ := NewToken("ma_")
	exp := time.Now().Add(-time.Hour) // already expired
	if _, err := r.CreateToken(ctx, uid, "expired", tp, &exp); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.TokenUser(ctx, tp.Raw); err != ErrInvalidToken {
		t.Errorf("expired token: err = %v, want ErrInvalidToken", err)
	}
}
