//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/standards-lab/docinfer/postgres"
	"github.com/standards-lab/sqlate/postgres/pgtest"
)

// dsnEnv names the PostgreSQL the live tier creates its databases on, which the root mise.toml
// sets for the compose harness's server, and prefix starts each database's name.
const (
	dsnEnv = "DOCINFER_DSN"
	prefix = "docinfer"
)

// TestLive_Migrations applies the set, as a host applies it, to a throwaway database pgtest
// creates on the server DOCINFER_DSN names, and finds docinfer_cache there. pgtest drops the
// database when the test ends.
func TestLive_Migrations(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	db := pgtest.Migrated(t, dsnEnv, prefix, set)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var exists bool
	if err := db.Pool.QueryRowContext(ctx, "SELECT to_regclass('docinfer_cache') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("look up docinfer_cache: %v", err)
	}
	if !exists {
		t.Fatal("docinfer_cache does not exist after Migrations is applied")
	}
}
