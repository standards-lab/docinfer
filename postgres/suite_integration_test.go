//go:build integration

package postgres_test

import (
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/docinfertest"
	"github.com/standards-lab/docinfer/postgres"
)

func TestLive_Cache(t *testing.T) {
	docinfertest.Cache(t, func(t *testing.T) docinfer.Cache {
		c, err := postgres.New(t.Context(), migrated(t).Session())
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	})
}
