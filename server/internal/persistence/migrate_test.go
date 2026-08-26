package persistence

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMigrations(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	dir := writeMigrations(t, "notnumbered_init.sql")
	if _, err := LoadMigrations(dir); err == nil {
		t.Fatal("expected naming-convention rejection")
	}
}

func TestLoadMigrationsIgnoresNonSQLFiles(t *testing.T) {
	dir := writeMigrations(t, "0001_init.sql", "README.md")
	files, err := LoadMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "init" {
		t.Fatalf("expected only 0001_init.sql, got %+v", files)
	}
}

func TestLoadMigrationsRejectsDuplicateVersions(t *testing.T) {
	dir := writeMigrations(t, "0001_init.sql", "0001_dup.sql")
	if _, err := LoadMigrations(dir); err == nil {
		t.Fatal("expected duplicate-version rejection")
	}
}

func TestLoadMigrationsSortsByVersion(t *testing.T) {
	dir := writeMigrations(t, "0002_later.sql", "0001_first.sql", "0010_last.sql")

	files, err := LoadMigrations(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range files {
		got = append(got, filepath.Base(f.Path))
	}
	want := []string{"0001_first.sql", "0002_later.sql", "0010_last.sql"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch: got %v want %v", got, want)
		}
	}
}
