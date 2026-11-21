package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/standards-lab/docinfer/postgres"
)

// See docs/development.md
var released = map[int]struct {
	name, up, down string
	transactional  bool
}{
	1: {"cache", "fc9cf6f8a5f6da36855cb53cf340d75772cc9da696934983bd39a78c09f3a566", "f00a08e90ea92e51020891a11aefe42687d1a946e803da968e92ee2a78873b8c", true},
}

func sum(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

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
