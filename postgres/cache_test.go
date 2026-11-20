package postgres_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/postgres"
	"github.com/standards-lab/sqlate"
	pgdialect "github.com/standards-lab/sqlate/postgres"
	"github.com/standards-lab/sqlate/sqltest"
)

// key returns a key of docinfer.Key's form for capability, its hash n's digit repeated.
func key(capability string, n int) string {
	return capability + "/" + strings.Repeat(string(rune('0'+n)), 64)
}

// scripted returns a session over sqltest's scripted driver, which answers responses in order,
// and its recorder.
func scripted(t *testing.T, responses ...sqltest.Response) (*sqlate.DB, *sqltest.Recorder) {
	t.Helper()
	pool, rec := sqltest.Open(t, responses...)
	return sqlate.Wrap(pool, pgdialect.Dialect{}), rec
}

// Statements lists the compiled statements in name order.
func TestStatementsList(t *testing.T) {
	var names []string
	for _, st := range postgres.Statements() {
		names = append(names, st.Name())
	}
	if want := []string{"hold_value", "lookup_values"}; !slices.Equal(names, want) {
		t.Fatalf("statements %v, want %v", names, want)
	}
}

// New refuses a pool on any driver but pgx's, naming the driver connection it found: here
// sqltest's.
func TestNewRefusesAnotherDriver(t *testing.T) {
	db, _ := scripted(t)
	_, err := postgres.New(t.Context(), db)
	if !errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), "*sqltest.conn") {
		t.Fatalf("New over sqltest's driver: %v, want ErrRequest naming *sqltest.conn", err)
	}
}

// New panics on a nil db, naming it.
func TestNewPanicsOnANilDB(t *testing.T) {
	defer func() {
		if p := recover(); p != "postgres: New: db is nil" {
			t.Errorf("New with a nil db: panic %v", p)
		}
	}()
	_, _ = postgres.New(t.Context(), nil)
}

// A Get of several keys is one query binding the keys asked, each once, as one slice; the rows
// map back to the keys, values[i] keys[i]'s, nil for a miss, and a key asked twice is answered
// twice, each answer its own slice.
func TestGetIsOneQuery(t *testing.T) {
	a, b, miss := key("vision", 1), key("embeddings", 2), key("vision", 3)
	db, rec := scripted(t, sqltest.Response{
		Columns: []string{"key", "value"},
		Rows:    [][]driver.Value{{b, []byte("bee")}, {a, []byte("ay")}},
	})
	c := postgres.NewUnchecked(db)
	got, err := c.Get(t.Context(), a, miss, b, a)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]byte{[]byte("ay"), nil, []byte("bee"), []byte("ay")}
	if !slices.EqualFunc(got, want, func(x, y []byte) bool { return string(x) == string(y) && (x == nil) == (y == nil) }) {
		t.Fatalf("Get = %q, want %q", got, want)
	}
	got[0][0] = 'X'
	if string(got[3]) != "ay" {
		t.Errorf("a key asked twice shares its answer: the second reads %q after the first changed", got[3])
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpQuery}) {
		t.Fatalf("Get of 4 keys ran %v, want one query", ops)
	}
	args := rec.Calls()[0].Args
	if len(args) != 1 || !slices.Equal(args[0].([]string), []string{a, miss, b}) {
		t.Errorf("Get bound %v, want the keys once each as one []string", args)
	}
	if rec.Pending() != 0 || rec.RowsLeaked() != 0 {
		t.Errorf("pending %d, rows leaked %d", rec.Pending(), rec.RowsLeaked())
	}
}

// A Get of no keys answers an empty, non-nil slice and asks nothing.
func TestGetOfNoKeysAsksNothing(t *testing.T) {
	db, rec := scripted(t)
	got, err := postgres.NewUnchecked(db).Get(t.Context())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Get() = %v (nil %v), %v; want an empty slice", got, got == nil, err)
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("Get() ran %v", rec.Ops())
	}
}

// A Put is one statement binding the key and the value.
func TestPutIsOneStatement(t *testing.T) {
	db, rec := scripted(t, sqltest.Response{Affected: 1})
	k := key("transcription", 4)
	if err := postgres.NewUnchecked(db).Put(t.Context(), k, []byte("held")); err != nil {
		t.Fatal(err)
	}
	if ops := rec.Ops(); !slices.Equal(ops, []sqltest.Op{sqltest.OpExec}) {
		t.Fatalf("Put ran %v, want one exec", ops)
	}
	if args := rec.Calls()[0].Args; len(args) != 2 || args[0] != k || string(args[1].([]byte)) != "held" {
		t.Errorf("Put bound %v, want the key and the value", args)
	}
}

// A database error surfaces from Get and Put, naming the operation.
func TestDatabaseErrorsSurface(t *testing.T) {
	boom := errors.New("boom")
	db, _ := scripted(t, sqltest.Response{Err: boom}, sqltest.Response{Err: boom})
	c := postgres.NewUnchecked(db)
	if _, err := c.Get(t.Context(), key("vision", 1)); !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "postgres: get: ") {
		t.Errorf("Get on a failing database: %v", err)
	}
	if err := c.Put(t.Context(), key("vision", 1), []byte("v")); !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "postgres: put ") {
		t.Errorf("Put on a failing database: %v", err)
	}
}

// Every guard refuses before any query: a key not of Key's form in a Get or a Put, and an empty
// value, matching ErrRequest; an ended context with its own error, unwrapped, which tells the
// guard's refusal from database/sql's, since database/sql also refuses an ended context without
// reaching the driver.
func TestGuardsRefuseBeforeAnyQuery(t *testing.T) {
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	good := key("vision", 1)
	bad := []string{
		"", "vision", "vision/", "/" + strings.Repeat("a", 64), "Vision/" + strings.Repeat("a", 64),
		"vision/" + strings.Repeat("A", 64), "vision/" + strings.Repeat("a", 63), "vision/" + strings.Repeat("a", 65),
		"vision/../" + strings.Repeat("a", 61), "vi5ion/" + strings.Repeat("a", 64), "vision/" + strings.Repeat("g", 64),
	}
	db, rec := scripted(t)
	c := postgres.NewUnchecked(db)
	for _, k := range bad {
		if _, err := c.Get(t.Context(), good, k); !errors.Is(err, docinfer.ErrRequest) {
			t.Errorf("Get of %q: %v, want ErrRequest", k, err)
		}
		if err := c.Put(t.Context(), k, []byte("v")); !errors.Is(err, docinfer.ErrRequest) {
			t.Errorf("Put of %q: %v, want ErrRequest", k, err)
		}
	}
	for _, v := range [][]byte{nil, {}} {
		err := c.Put(t.Context(), good, v)
		if !errors.Is(err, docinfer.ErrRequest) || err.Error() != "postgres: put "+good+": empty value: "+docinfer.ErrRequest.Error() {
			t.Errorf("Put of %v: %v, want an empty value refused", v, err)
		}
	}
	if _, err := c.Get(ended, good); err != context.Canceled {
		t.Errorf("Get under an ended context: %v", err)
	}
	if _, err := c.Get(ended); err != context.Canceled {
		t.Errorf("Get of no keys under an ended context: %v", err)
	}
	if err := c.Put(ended, good, []byte("v")); err != context.Canceled {
		t.Errorf("Put under an ended context: %v", err)
	}
	if calls := rec.Calls(); len(calls) != 0 {
		t.Errorf("the refusals ran %v", rec.Ops())
	}
}
