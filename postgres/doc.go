// Package postgres is docinfer's cache in PostgreSQL: a [docinfer.Cache] that keeps one row per
// key in the database a sqlate session names, so several hosts running one pipeline share one
// cache and ask a model once for content any of them has seen.
//
// The package exports:
//
//   - [Cache], the docinfer.Cache, made by [New] from a context and a *sqlate.DB, whose pool
//     must be on pgx's database/sql driver, with its [Cache.Get] and [Cache.Put]
//   - [Migrations], the cache's table as a migration set, named [Source] and recorded in the
//     history table [Table]
//   - [Verify], which prepares the statements against a live schema, and [Statements], which
//     lists them
//
// # Schema
//
// The migration set's 0001 creates one table, docinfer_cache, a row per key. key, text, is the
// primary key: a [docinfer.Key]'s string, "<capability>/<hash>". value, bytea, is the held value,
// NOT NULL and never empty, a CHECK holding octet_length(value) > 0. written_at, timestamptz NOT
// NULL, is the time of the Put that last wrote the row on the database's clock, now(), which every
// Put sets, on an insert and a replacement alike. The table has no other index and no version
// column: [docinfer.Version] is in the key's hash. Every object the set creates is prefixed
// docinfer_, and a host declares the set in its migrator and applies it; the cache never
// migrates. A released migration never changes; a golden-hash test pins each one.
//
// docinfer removes no row: a held value is a model's result for content, and it stays valid for
// as long as its key, which changes with the content, the request, or the Version. A host that
// bounds the table prunes it by age with its own SQL over written_at, such as a DELETE of the
// rows written before a cutoff; a pruned key's content is asked of the model again on its next
// run.
//
// # Statements
//
// The statements are authored files compiled through sqlate's query package, each at the native
// tier, its PostgreSQL feature named in the file with how a port to another engine would do it:
//
//   - lookup_values, [Cache.Get]'s read: the rows whose key = ANY of the keys bound as one
//     text[], so a batch's Get of any number of keys is one query with one bound value
//   - hold_value, [Cache.Put]'s write: INSERT ... ON CONFLICT DO UPDATE, so a first put and a
//     replacement are one statement that never fails on the key's row existing, setting
//     written_at to now() either way
//
// [Verify], which prepares them all, is for a host's verify stage, after its migrations, so a
// schema without the docinfer set, or behind these statements, fails at start rather than at the
// first Get or Put.
//
// # The driver
//
// The host's pool must be on pgx's database/sql driver, github.com/jackc/pgx/v5/stdlib, wrapped
// as a *sqlate.DB: lookup_values binds a Go []string as text[], which that driver encodes. [New]
// reads the driver from a connection it takes from the pool, and refuses any other, naming the
// driver connection's type it found, matching [docinfer.ErrRequest]; a pool it can't reach fails
// too.
//
// # Guards
//
// A nil db panics in New, as the caller's mistake. Get and Put refuse a key not of Key's
// form, a capability of lowercase letters, a slash, and 64 lowercase hex digits, as docinfer's
// FileCache does, so the table holds only keys docinfer makes; Put refuses an empty value. Each
// refusal matches [docinfer.ErrRequest]. A context already ended is refused with its own error.
// Every guard runs before any query: a refused call reaches no database. A Get of no keys asks
// nothing, and a key asked twice in one Get is bound once and answered twice, each answer its own
// slice.
//
// # Testing
//
// A test that needs a throwaway database with the docinfer set applied makes one with
// github.com/standards-lab/sqlate/postgres/pgtest, passing [Migrations] to its Migrated, as this
// module's integration suite does; it runs docinfertest's Cache suite whole against the Cache.
package postgres
