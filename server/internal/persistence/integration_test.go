package persistence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/analeis/highjack/server/internal/logging"
	"github.com/jackc/pgx/v5"
)

// Integration conventions: database-backed tests run only when
// HIGHJACK_TEST_DATABASE_URL points at an isolated disposable database.
// Locally: `docker compose -f infra/docker/docker-compose.dev.yml up -d`
// provides one; see docs/development/TESTING.md.
//
// Everything else in this package must stay hermetic so `go test ./...`
// never requires infrastructure.
//
// Each test gets its own PostgreSQL *schema*, created before the test and
// dropped after it, with the connection's search_path pointed at it. Sharing one
// schema made every assertion about a "fresh" database a lie: the migration test
// only passed against a virgin schema, and any test using a fixed id collided
// with the previous run's rows. That is precisely the class of test that looks
// green and proves nothing on the second run.
func integrationDB(t *testing.T) *Store {
	t.Helper()
	store, dsn, schema := newTestSchema(t)
	t.Cleanup(func() {
		_ = store.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if c, err := pgx.Connect(dropCtx, dsn); err == nil {
			_, _ = c.Exec(dropCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			_ = c.Close(dropCtx)
		}
	})

	// Apply the migrations here rather than relying on another test to have done
	// it. Sharing one schema made every test depend on execution order: whichever
	// ran first created the tables and the rest silently assumed it. Each test now
	// starts from the real migration state, in isolation.
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer migrateCancel()
	if _, err := store.Migrate(migrateCtx, "../../migrations"); err != nil {
		t.Fatalf("migrate test schema: %v", err)
	}
	return store
}

// newTestSchema creates an empty schema and returns a store scoped to it, along
// with the original DSN and the schema name so the caller can drop it.
func newTestSchema(t *testing.T) (*Store, string, string) {
	t.Helper()
	dsn := os.Getenv("HIGHJACK_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("HIGHJACK_TEST_DATABASE_URL not set; skipping database integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	schema := "hj_test_" + randomSchemaSuffix(t)
	log := logging.New(logging.Options{Service: "highjack-test", Level: logging.LevelWarn})

	// Create the schema through a plain connection first, then connect with the
	// search_path already set so the store's own statements land in it.
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create test schema %s: %v", schema, err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Fatalf("close admin connection: %v", err)
	}

	store, err := Connect(ctx, log, appendSearchPath(dsn, schema))
	if err != nil {
		t.Fatalf("connect to integration database: %v", err)
	}
	return store, dsn, schema
}

// integrationDBUnmigrated is integrationDB without the migrations applied, for
// the one test whose subject is the migration run itself.
func integrationDBUnmigrated(t *testing.T) *Store {
	t.Helper()
	store, dsn, schema := newTestSchema(t)
	t.Cleanup(func() {
		_ = store.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if c, err := pgx.Connect(dropCtx, dsn); err == nil {
			_, _ = c.Exec(dropCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			_ = c.Close(dropCtx)
		}
	})
	return store
}

// randomSchemaSuffix derives a schema name unique to this test run.
func randomSchemaSuffix(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	s := hex.EncodeToString(b[:])
	t.Cleanup(func() { _ = s })
	return strings.ToLower(s)
}

// appendSearchPath adds search_path to a DSN, preserving any existing one.
func appendSearchPath(dsn, schema string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + url.QueryEscape(schema)
}

// Migrations must apply to a genuinely empty schema, and applying them again
// must be a no-op that leaves the schema intact. Both halves matter: the first
// proves the files are valid SQL in order, the second that a restart does not
// re-run or corrupt them.
func TestMigrationsApplyIdempotently(t *testing.T) {
	store := integrationDBUnmigrated(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Nothing exists yet: the schema was just created.
	var tables int
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`).Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("expected an empty schema, found %d table(s)", tables)
	}

	first, err := store.Migrate(ctx, "../../migrations")
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected every migration to apply to an empty schema")
	}

	second, err := store.Migrate(ctx, "../../migrations")
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("migrations must be idempotent; re-applied %v", second)
	}

	// The schema must still be complete and unchanged.
	for _, want := range []string{
		"users", "games", "game_configs", "match_snapshots", "match_events", "game_players",
	} {
		var n int
		if err := store.pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables
			  WHERE table_schema = current_schema() AND table_name = $1`, want).Scan(&n); err != nil {
			t.Fatalf("look up table %s: %v", want, err)
		}
		if n != 1 {
			t.Fatalf("table %q is missing after migration", want)
		}
	}
}
