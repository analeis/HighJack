package persistence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/logging"
)

// Integration conventions: database-backed tests run only when
// HIGHJACK_TEST_DATABASE_URL points at an isolated disposable database.
// Locally: `docker compose -f infra/docker/docker-compose.dev.yml up -d`
// provides one; see docs/development/TESTING.md.
//
// Everything else in this package must stay hermetic so `go test ./...`
// never requires infrastructure.
func integrationDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("HIGHJACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HIGHJACK_TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log := logging.New(logging.Options{Service: "highjack-test", Level: logging.LevelWarn})
	store, err := Connect(ctx, log, dsn)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestMigrationsApplyIdempotently(t *testing.T) {
	store := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	first, err := store.Migrate(ctx, "../../migrations")
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected at least one migration to apply on a fresh database")
	}

	second, err := store.Migrate(ctx, "../../migrations")
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("migrations must be idempotent; re-applied %v", second)
	}
}
