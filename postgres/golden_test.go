package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/standards-lab/docinfer/postgres"
)

// released pins every migration as Migrations returns it: its name, the sha256 of its up and down
// texts, and whether it runs in a transaction. A released migration never changes: a new
// migration adds a row here, and a changed row is a breaking change to the schema's version line.
// The texts are the files' bodies without their "--|" header, which Transactional carries. 0001,
// the cache's table, is released in the module's first version.
var released = map[int]struct {
	name, up, down string
	transactional  bool
}{
	1: {"cache", "fc9cf6f8a5f6da36855cb53cf340d75772cc9da696934983bd39a78c09f3a566", "f00a08e90ea92e51020891a11aefe42687d1a946e803da968e92ee2a78873b8c", true},
}

// sum is the hex sha256 of text.
func sum(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// TestReleasedMigrations checks the migration set against the pins: a released migration whose
// text, name, or transaction changed, a migration the table does not list, and a listed migration
// missing from the set are each a failure.
func TestReleasedMigrations(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatalf("Migrations: %v", err)
	}
	seen := map[int]bool{}
	for _, m := range set.Migrations {
		seen[m.Version] = true
		want, ok := released[m.Version]
		if !ok {
			t.Errorf("version %d %s is not in the released table; add it with up %s and down %s", m.Version, m.Name, sum(m.Up), sum(m.Down))
			continue
		}
		if m.Name != want.name || sum(m.Up) != want.up || sum(m.Down) != want.down || m.Transactional != want.transactional {
			t.Errorf("released version %d changed: %s up %s down %s transactional %v, released %s up %s down %s transactional %v",
				m.Version, m.Name, sum(m.Up), sum(m.Down), m.Transactional, want.name, want.up, want.down, want.transactional)
		}
	}
	for version, want := range released {
		if !seen[version] {
			t.Errorf("released version %d %s is missing from the set", version, want.name)
		}
	}
}
