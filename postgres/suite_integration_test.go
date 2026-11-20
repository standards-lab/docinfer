//go:build integration

package postgres_test

import (
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/docinfertest"
	"github.com/standards-lab/docinfer/postgres"
)

// TestLive_Cache runs the base module's cache contract against Cache. Each case gets a new,
// empty cache over its own scratch database with the docinfer set applied, a pgx stdlib pool
// pgtest opens and drops when the case ends.
func TestLive_Cache(t *testing.T) {
	docinfertest.Cache(t, func(t *testing.T) docinfer.Cache {
		c, err := postgres.New(t.Context(), migrated(t).Session())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	})
}
