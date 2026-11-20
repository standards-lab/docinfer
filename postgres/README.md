# docinfer/postgres

`github.com/standards-lab/docinfer/postgres` is docinfer's cache in PostgreSQL: a `Cache`, the
`docinfer.Cache` that keeps one row per key in the database a sqlate session names, so several
hosts running one pipeline share one cache and ask a model once for content any of them has seen.
It is a nested module of [docinfer](../README.md), released on its own `postgres/vX.Y.Z` tags, and
requires the base module, sqlate, sqlate's `postgres` dialect, and pgx.

## Install

```sh
go get github.com/standards-lab/docinfer/postgres@v0.1.0
```

[`CHANGELOG.md`](CHANGELOG.md) states what each release changes. The package's documentation,
`go doc github.com/standards-lab/docinfer/postgres`, is the reference for its exported names, and
its doc comment lists them: `New`, and `Cache` with its `Get` and `Put`; `Migrations`, `Source`,
and `Table`; and `Verify` and `Statements`.

## Use

The pool is the host's, opened on pgx's `database/sql` driver, `github.com/jackc/pgx/v5/stdlib`,
and wrapped as a `*sqlate.DB`. Error handling is elided:

```go
set, err := postgres.Migrations() // declare it in the host's migrator, beside its own sets, and apply it
err = postgres.Verify(ctx, db)    // from the verify stage, after the migrations

cache, err := postgres.New(ctx, db) // a *sqlate.DB over a pool on pgx's stdlib driver
p, err := docinfer.New(docinfer.Config{
	Cache: cache,
	// the pipeline's other fields
})
```

`New` takes no configuration beyond the session. A nil session panics; a pool on any other driver
is refused, naming the driver connection's type it found, matching `docinfer.ErrRequest`. `New`
reads the driver from a connection it takes from the pool, so a pool it can't reach fails too.

## The host's responsibilities

- **Migrations the host applies.** `Migrations` is the `docinfer` migration set, recorded in its
  own history table, `docinfer_schema_version`: `docinfer_cache`, a row per key, with `key`, the
  `docinfer.Key`'s string, its primary key; `value`, the held bytes, never empty; and
  `written_at`, the time of the `Put` that last wrote the row on the database's clock. Every
  object it creates is prefixed `docinfer_`. The cache never migrates; a host declares the set and
  applies it, and `Verify` prepares every statement against the live schema, so a schema without
  the set, or behind it, fails at start. A released migration never changes; a golden-hash test
  pins each.
- **The pgx stdlib driver.** A `Get` binds its keys as one `text[]`, which pgx's `database/sql`
  driver encodes, so the pool must be on `github.com/jackc/pgx/v5/stdlib`; `New` refuses one that
  isn't.
- **Pruning, if any.** docinfer removes no row: a held value stays valid as long as its key, which
  changes with the content, the request, or `docinfer.Version`. A host that bounds the table
  prunes it by age with its own SQL over `written_at`; a pruned key's content is asked of the
  model again on its next run.

```sql
DELETE FROM docinfer_cache WHERE written_at < now() - interval '180 days';
```

## The contracts

- **One query per batch.** A `Get` of any number of keys is one query, `lookup_values`, the rows
  whose key is `= ANY` of the keys; a key asked twice is bound once and answered twice. A `Get` of
  no keys asks nothing.
- **One statement per `Put`.** `hold_value` is `INSERT ... ON CONFLICT DO UPDATE`: it inserts the
  key's row or replaces its value, and sets `written_at` to `now()` either way.
- **Guards before any query.** `Get` and `Put` refuse a key not of `Key`'s form,
  `<capability>/<hash>` with a capability of lowercase letters and a hash of 64 lowercase hex
  digits, and `Put` an empty value, each matching `docinfer.ErrRequest`; a context already ended
  is refused with its own error. A refused call reaches no database.

Both statements are at sqlate's native tier: each file names the PostgreSQL feature it uses and
how a port to another engine would do without it.

## Testing

Tasks run from the repository root, `mise run <task>`:

- `check`: the unit tier, with the base module, then `sqlint` over the module's SQL. It holds the
  migration's golden hash, the statement list, the driver refusal, and the guards; it reads no
  network.
- `integration:postgres`: the live tier, under the `integration` build tag. It starts the compose
  harness's PostgreSQL alone, applies the set and verifies it, and runs docinfertest's `Cache`
  suite whole, each test on a database of its own made through `sqlate/postgres/pgtest`, and stops
  the service whether the suite passes or fails, printing its log on a failure. `integration`
  runs it, and CI's integration job runs `integration` on a push to main and on demand.

PostgreSQL listens on port 5437; `DOCINFER_POSTGRES_PORT` moves it, and `DOCINFER_DSN` in the root
`mise.toml`, the admin DSN the tier reads, follows. A host's own tests make a migrated database
the same way, passing `Migrations` to `pgtest.Migrated`.

Apache-2.0.
