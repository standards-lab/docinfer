package postgres

import "github.com/standards-lab/sqlate"

// NewUnchecked returns a Cache over db without New's driver check, so the unit tier runs a Cache
// over sqlate's scripted driver and counts the queries it sends.
func NewUnchecked(db *sqlate.DB) *Cache { return &Cache{db: db} }
