package persistence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A migration added late, with a version below one that has already run, must
// be refused rather than silently skipped.
//
// The runner used to compare each file's version against the *highest* applied
// version, so a file numbered below that high-water mark was skipped whether it
// had already run or not. The two cases are opposites: one is a migration that
// is done, the other is a migration that will never happen. Silently skipping
// the second leaves the schema behind the code that expects it, and the server
// starts happily against a database it cannot write to.
//
// The runner now remembers the *set* of applied versions, so it can tell them
// apart.

func writeMigrationFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMigrationAddedLateIsRefused(t *testing.T) {
	store := integrationDBUnmigrated(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Apply version 2.
	first := writeMigrationFiles(t, map[string]string{
		"0002_second.sql": "CREATE TABLE mig_late_probe (id int primary key);",
	})
	if _, err := store.Migrate(ctx, first); err != nil {
		t.Fatalf("apply first migration: %v", err)
	}

	// Now add version 1 — below one that has already run.
	second := writeMigrationFiles(t, map[string]string{
		"0001_late.sql":   "CREATE TABLE mig_late_probe_v1 (id int primary key);",
		"0002_second.sql": "CREATE TABLE mig_late_probe (id int primary key);",
	})
	_, err := store.Migrate(ctx, second)
	if err == nil {
		t.Fatal("a migration numbered below an applied one must be refused, not skipped")
	}
	if !strings.Contains(err.Error(), "below the highest applied migration") {
		t.Fatalf("error = %v, want it to name the problem", err)
	}

	// The late migration must not have run.
	var n int
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		  WHERE table_schema = current_schema() AND table_name = 'mig_late_probe_v1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the late migration ran, which is exactly what it must not do")
	}
}

func TestMigrationAlreadyAppliedIsSkippedNotRefused(t *testing.T) {
	store := integrationDBUnmigrated(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	files := writeMigrationFiles(t, map[string]string{
		"0001_first.sql": "CREATE TABLE mig_rerun_probe (id int primary key);",
	})
	if _, err := store.Migrate(ctx, files); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	// Re-running the same directory is a no-op, not an error: the version is
	// already applied, and that is the ordinary case at every boot.
	applied, err := store.Migrate(ctx, files)
	if err != nil {
		t.Fatalf("re-applying an applied migration must be a no-op: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("re-applying reported %v, want nothing", applied)
	}
}

func TestMigrationAboveTheAppliedWatermarkStillRuns(t *testing.T) {
	store := integrationDBUnmigrated(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first := writeMigrationFiles(t, map[string]string{
		"0001_first.sql": "CREATE TABLE mig_above_probe (id int primary key);",
	})
	if _, err := store.Migrate(ctx, first); err != nil {
		t.Fatalf("apply first migration: %v", err)
	}

	second := writeMigrationFiles(t, map[string]string{
		"0001_first.sql": "CREATE TABLE mig_above_probe (id int primary key);",
		"0003_third.sql": "CREATE TABLE mig_above_probe_v3 (id int primary key);",
	})
	applied, err := store.Migrate(ctx, second)
	if err != nil {
		t.Fatalf("apply the higher migration: %v", err)
	}
	if len(applied) != 1 || !strings.Contains(applied[0], "0003") {
		t.Fatalf("applied = %v, want the 0003 migration", applied)
	}
}
