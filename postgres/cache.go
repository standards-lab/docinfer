package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/sqlate"
	"github.com/standards-lab/sqlate/query"
)

var _ docinfer.Cache = (*Cache)(nil)

// Cache is a [docinfer.Cache] over PostgreSQL, through a sqlate session, in the docinfer_cache
// table [Migrations] creates: one row per key. A Get of any number of keys is one query, and a
// Put one statement, which inserts the key's row or replaces its value, setting written_at to the
// database's clock either way.
//
// It is safe for concurrent use. Its zero value is not ready; [New] makes one.
type Cache struct {
	db *sqlate.DB
}

type held struct {
	key   string
	value []byte
}

var (
	lookupValues = statements.Statement("lookup_values").Scan(func(r query.Row) (held, error) {
		var h held
		return h, r.Scan(&h.key, &h.value)
	})
	holdValue = statements.Statement("hold_value")
)

// New returns a Cache over db, the session over the host's pool, which must be on pgx's
// database/sql driver, github.com/jackc/pgx/v5/stdlib, with the docinfer set applied. A nil db is
// the caller's mistake, and panics. A pool on another driver is refused, matching
// [docinfer.ErrRequest]. The driver is read from a connection New takes from the pool under ctx,
// so a pool it can't reach fails too.
func New(ctx context.Context, db *sqlate.DB) (*Cache, error) {
	if db == nil {
		panic("postgres: New: db is nil")
	}
	if err := checkDriver(ctx, db); err != nil {
		return nil, err
	}
	return &Cache{db: db}, nil
}

func checkDriver(ctx context.Context, db *sqlate.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("postgres: reading the pool's driver: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return conn.Raw(func(dc any) error {
		if _, ok := dc.(*stdlib.Conn); !ok {
			return fmt.Errorf("postgres: the pool's driver connection is %T, not pgx's database/sql driver's *stdlib.Conn: %w", dc, docinfer.ErrRequest)
		}
		return nil
	})
}

func checkKey(key string) error {
	capability, hash, ok := strings.Cut(key, "/")
	if !ok || !isCapability(capability) || !isHash(hash) {
		return fmt.Errorf("postgres: key %q isn't of Key's form: %w", key, docinfer.ErrRequest)
	}
	return nil
}

func isCapability(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Get returns keys' held values in one query, values[i] keys[i]'s, nil for a miss. Every key is
// checked before the query, and no keys asks nothing. A key asked twice is answered twice, each
// answer its own slice.
func (c *Cache) Get(ctx context.Context, keys ...string) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, k := range keys {
		if err := checkKey(k); err != nil {
			return nil, err
		}
	}
	values := make([][]byte, len(keys))
	if len(keys) == 0 {
		return values, nil
	}
	unique := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			unique = append(unique, k)
		}
	}
	rows, err := lookupValues.All(ctx, c.db, query.Args{"keys": unique})
	if err != nil {
		return nil, fmt.Errorf("postgres: get: %w", err)
	}
	found := make(map[string][]byte, len(rows))
	for _, r := range rows {
		found[r.key] = r.value
	}
	answered := make(map[string]bool, len(rows))
	for i, k := range keys {
		v, ok := found[k]
		if !ok {
			continue
		}
		if answered[k] {
			v = slices.Clone(v)
		}
		answered[k] = true
		values[i] = v
	}
	return values, nil
}

// Put holds value under key in one statement, replacing a held value. A key not of Key's form
// and an empty value are refused, matching [docinfer.ErrRequest], before the statement.
func (c *Cache) Put(ctx context.Context, key string, value []byte) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if len(value) == 0 {
		return fmt.Errorf("postgres: put %s: empty value: %w", key, docinfer.ErrRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := holdValue.Exec(ctx, c.db, query.Args{"key": key, "value": value}); err != nil {
		return fmt.Errorf("postgres: put %s: %w", key, err)
	}
	return nil
}
