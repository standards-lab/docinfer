package postgres_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer/postgres"
)

// The set is named for the module and recorded in its own history table, apart from a host's.
func TestMigrationsName(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if set.Name != "docinfer" || set.Table != "docinfer_schema_version" || len(set.Migrations) != 1 {
		t.Fatalf("set = %s/%s with %d migrations", set.Name, set.Table, len(set.Migrations))
	}
	if postgres.Source != set.Name || postgres.Table != set.Table {
		t.Errorf("Source/Table = %s/%s, set %s/%s", postgres.Source, postgres.Table, set.Name, set.Table)
	}
}

// Every object the set creates carries the docinfer_ prefix, so it sits beside a host's own
// schema without a clash.
func TestMigrationsPrefixTheirObjects(t *testing.T) {
	set, err := postgres.Migrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range set.Migrations {
		for line := range strings.SplitSeq(m.Up, "\n") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if (f == "TABLE" || f == "INDEX" || f == "CONSTRAINT") && i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "docinfer_") {
					t.Errorf("%d %s: %q names an object without the docinfer_ prefix", m.Version, m.Name, strings.TrimSpace(line))
				}
			}
		}
	}
}
