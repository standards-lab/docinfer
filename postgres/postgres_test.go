package postgres_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer/postgres"
)

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
