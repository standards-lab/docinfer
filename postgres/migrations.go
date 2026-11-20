package postgres

import (
	"embed"
	"fmt"

	"github.com/standards-lab/sqlate/migrate"
)

const (
	// Source is the migration set's name and the prefix of every object the set owns.
	Source = "docinfer"

	// Table is the history table the set's migrations are recorded in, apart from a host's own.
	Table = "docinfer_schema_version"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations returns the cache's migration set for PostgreSQL: the docinfer_cache table. A host
// declares it in its migrator and applies it; the cache never migrates on its own.
func Migrations() (migrate.Set, error) {
	files, err := migrate.Files(migrationFiles, "migrations")
	if err != nil {
		return migrate.Set{}, fmt.Errorf("docinfer/postgres: %w", err)
	}
	return migrate.Set{Name: Source, Table: Table, Migrations: files}, nil
}
