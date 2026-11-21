package postgres

import "github.com/standards-lab/sqlate"

func NewUnchecked(db *sqlate.DB) *Cache { return &Cache{db: db} }
