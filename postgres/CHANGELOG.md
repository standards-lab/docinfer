# Changelog

All notable changes to `github.com/standards-lab/docinfer/postgres`, docinfer's cache over sqlate
on PostgreSQL, are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `postgres/vX.Y.Z` tag carries, and is the release's notes. Its exported API is its contract: a
change to it is a change here. This changelog covers the postgres module only; the base module
keeps its own.

## [Unreleased]

## [v0.1.5] - 2026-11-28

This release requires docinfer v0.7.0, which adds the `profile` package, with nothing to adapt.

### Changed

- The module requires docinfer v0.7.0.

## [v0.1.4] - 2026-11-28

This release requires docinfer v0.6.0, which adds document and timeline extraction, with nothing
to adapt.

### Changed

- The module requires docinfer v0.6.0; its `Cache` passes docinfertest's suite unchanged, since
  window and combine calls hold their replies under ordinary keys.

## [v0.1.3] - 2026-11-27

This release requires docinfer v0.5.0, which skips vision for a page whose text layer holds its
words, with nothing to adapt.

### Changed

- The module requires docinfer v0.5.0; its `Cache` passes docinfertest's suite unchanged, since a
  skipped page image holds nothing.

## [v0.1.2] - 2026-11-27

This release requires docinfer v0.4.0, which embeds metadata records, with nothing to adapt.

### Changed

- The module requires docinfer v0.4.0; its `Cache` passes docinfertest's suite unchanged, since
  record texts add keys of the same form.

## [v0.1.1] - 2026-11-25

This release requires docinfer v0.3.0, whose `Held` gains chunk spans, and sqlate's additive
releases.

### Changed

- The module requires docinfer v0.3.0; its `Cache` passes docinfertest's suite, chunk spans
  included.
- The `sqlate` requirement is v0.8.0 and `sqlate/postgres` v0.9.0, both additive (extension
  management), with nothing to adapt.

## [v0.1.0] - 2026-11-20

The first release. The module is new: the spike has no PostgreSQL cache to copy. It requires
`github.com/standards-lab/docinfer` v0.2.0, whose `docinfer.Cache` it implements and whose
`docinfertest.Cache` suite it passes; `github.com/standards-lab/sqlate` v0.7.0 and
`sqlate/postgres` v0.8.0, whose `pgtest` its live tier runs on; and pgx v5.13.0, whose
`database/sql` driver the host's pool must be on.

### Added

- `Migrations`, the cache's schema as a sqlate migration set named `Source`, `docinfer`, recorded
  in `Table`, `docinfer_schema_version`: `docinfer_cache`, a row per key, with `key`, the
  `docinfer.Key`'s string, its primary key; `value`, bytea, never empty; and `written_at`, the
  time of the `Put` that last wrote the row on the database's clock. A host applies the set; the
  cache never migrates. docinfer removes no row, and a host prunes by age over `written_at` with
  its own SQL.
- `Verify`, which prepares every statement against a live schema, and `Statements`, which lists
  them: `lookup_values` and `hold_value`, both at the native tier.
- `Cache`, the `docinfer.Cache` over PostgreSQL, made by `New` from a `*sqlate.DB` whose pool
  must be on pgx's `database/sql` driver; another driver is refused, matching
  `docinfer.ErrRequest`, and a nil `*sqlate.DB` panics. `Get` reads any number of keys in one query,
  the keys bound as one `text[]`; `Put` inserts a key's row or replaces its value in one
  statement, setting `written_at` to the database's clock. A key not of `Key`'s form and an empty
  value are refused, matching `docinfer.ErrRequest`, and an ended context with its own error,
  before any query.

[Unreleased]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.5...HEAD
[v0.1.5]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.4...postgres/v0.1.5
[v0.1.4]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.3...postgres/v0.1.4
[v0.1.3]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.2...postgres/v0.1.3
[v0.1.2]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.1...postgres/v0.1.2
[v0.1.1]: https://github.com/standards-lab/docinfer/compare/postgres/v0.1.0...postgres/v0.1.1
[v0.1.0]: https://github.com/standards-lab/docinfer/releases/tag/postgres/v0.1.0
