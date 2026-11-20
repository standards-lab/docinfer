// Package postgres is docinfer's cache in PostgreSQL: a docinfer.Cache that keeps one row per key
// in the docinfer_cache table of the database a sqlate session names, so several hosts running
// one pipeline share one cache. [Migrations] is the table as a migration set, named [Source] and
// recorded in the history table [Table], which a host applies; the package never migrates on its
// own.
package postgres
