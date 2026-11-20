// Package postgres is docinfer's cache in PostgreSQL: a docinfer.Cache that keeps one row per key
// in the docinfer_cache table of the database a sqlate session names, so several hosts running
// one pipeline share one cache. [Migrations] is the table as a migration set, named [Source] and
// recorded in the history table [Table], which a host applies; the package never migrates on its
// own.
//
// [Cache], made by [New] over a sqlate session whose pool is on pgx's database/sql driver, reads
// any number of keys in one query and writes a value in one statement, stamping written_at with
// the database's clock. [Verify] prepares its statements against a live schema, for a host's
// verify stage after its migrations, and [Statements] lists them.
package postgres
