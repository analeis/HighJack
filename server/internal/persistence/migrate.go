package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// migrationFile is one parsed *.sql file from the migrations directory.
type migrationFile struct {
	Version int64
	Name    string
	Path    string
}

var migrationNameRe = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

// LoadMigrations reads and validates a migrations directory. Files must be
// named NNNN_description.sql with strictly increasing, unique versions.
func LoadMigrations(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var files []migrationFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := migrationNameRe.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration %q does not match NNNN_description.sql", e.Name())
		}
		version, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("migration %q has invalid version: %w", e.Name(), err)
		}
		files = append(files, migrationFile{Version: version, Name: m[2], Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Version != files[j].Version {
			return files[i].Version < files[j].Version
		}
		return files[i].Name < files[j].Name
	})
	for i := 1; i < len(files); i++ {
		if files[i].Version == files[i-1].Version {
			return nil, fmt.Errorf("duplicate migration version %d (%s and %s)",
				files[i].Version, files[i-1].Name, files[i].Name)
		}
	}
	return files, nil
}

// Migrate applies all pending migrations in order. Each migration runs in
// its own transaction together with its schema_migrations row insert, so a
// failed migration leaves the database untouched.
func (s *Store) Migrate(ctx context.Context, dir string) ([]string, error) {
	files, err := LoadMigrations(dir)
	if err != nil {
		return nil, err
	}
	if err := s.ensureMigrationsTable(ctx); err != nil {
		return nil, err
	}

	appliedMax, err := s.maxAppliedVersion(ctx)
	if err != nil {
		return nil, err
	}

	var applied []string
	for _, f := range files {
		if f.Version <= appliedMax {
			continue
		}
		sqlBytes, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", f.Path, err)
		}
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return nil, fmt.Errorf("begin migration %d: %w", f.Version, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("apply migration %04d_%s: %w", f.Version, f.Name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
			f.Version, f.Name); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("record migration %04d_%s: %w", f.Version, f.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit migration %04d_%s: %w", f.Version, f.Name, err)
		}
		s.log.Info("migration applied",
			"version", f.Version, "name", f.Name)
		applied = append(applied, fmt.Sprintf("%04d_%s", f.Version, f.Name))
	}
	return applied, nil
}

const migrationsTableName = "schema_migrations"

func (s *Store) ensureMigrationsTable(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS `+migrationsTableName+` (
			version    BIGINT PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func (s *Store) maxAppliedVersion(ctx context.Context) (int64, error) {
	var max pgtype.Int8
	row := s.pool.QueryRow(ctx, `SELECT MAX(version) FROM `+migrationsTableName)
	if err := row.Scan(&max); err != nil {
		return 0, fmt.Errorf("read applied migrations: %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}
