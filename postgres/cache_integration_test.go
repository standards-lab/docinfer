//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/standards-lab/docinfer/postgres"
	"github.com/standards-lab/sqlate/postgres/pgtest"
)

// migrated returns a throwaway database with the docinfer set applied, as a host applies it.
func migrated(t *testing.T) *pgtest.Database {
	t.Helper()
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	return pgtest.Migrated(t, dsnEnv, prefix, set)
}

// Verify prepares every statement: it fails on a database without the set and passes once the
// set is applied.
func TestLive_VerifyNeedsTheMigratedSchema(t *testing.T) {
	if err := postgres.Verify(t.Context(), pgtest.Create(t, dsnEnv, prefix).Session()); err == nil {
		t.Fatal("Verify passed on a database without the docinfer set")
	}
	if err := postgres.Verify(t.Context(), migrated(t).Session()); err != nil {
		t.Fatalf("Verify on the migrated database: %v", err)
	}
}

// writtenAt returns key's written_at and the database's clock as it was read.
func writtenAt(t *testing.T, db *pgtest.Database, key string) (written, now time.Time) {
	t.Helper()
	err := db.Pool.QueryRowContext(t.Context(), "SELECT written_at, now() FROM docinfer_cache WHERE key = $1", key).Scan(&written, &now)
	if err != nil {
		t.Fatalf("read written_at of %s: %v", key, err)
	}
	return written, now
}

// dbNow returns the database's clock.
func dbNow(t *testing.T, db *pgtest.Database) time.Time {
	t.Helper()
	var now time.Time
	if err := db.Pool.QueryRowContext(t.Context(), "SELECT now()").Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now
}

// A Put is read back by a Get, a replacement replaces it, and written_at is the database's
// clock at the put that inserted the row, moved by the put that replaced it.
func TestLive_PutGetRoundtrip(t *testing.T) {
	db := migrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	c, err := postgres.New(ctx, db.Session())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, miss := key("vision", 1), key("vision", 2)

	before := dbNow(t, db)
	if err := c.Put(ctx, a, []byte("first")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := c.Get(ctx, a, miss, a)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 3 || !bytes.Equal(got[0], []byte("first")) || got[1] != nil || !bytes.Equal(got[2], []byte("first")) {
		t.Fatalf("Get = %q, want first, nil, first", got)
	}
	inserted, after := writtenAt(t, db, a)
	if inserted.Before(before) || inserted.After(after) {
		t.Errorf("written_at on insert %s, not within the database's clock around the put, %s to %s", inserted, before, after)
	}

	if err := c.Put(ctx, a, []byte("second")); err != nil {
		t.Fatalf("Put replacing: %v", err)
	}
	got, err = c.Get(ctx, a)
	if err != nil || len(got) != 1 || !bytes.Equal(got[0], []byte("second")) {
		t.Fatalf("Get after a replacement = %q, %v; want second", got, err)
	}
	replaced, _ := writtenAt(t, db, a)
	if !replaced.After(inserted) {
		t.Errorf("written_at on replace %s, not after the insert's %s", replaced, inserted)
	}
	var rows int
	if err := db.Pool.QueryRowContext(ctx, "SELECT count(*) FROM docinfer_cache").Scan(&rows); err != nil || rows != 1 {
		t.Errorf("rows after a replacement: %d, %v; want 1", rows, err)
	}
}
