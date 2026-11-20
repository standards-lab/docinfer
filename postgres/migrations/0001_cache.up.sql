-- The cache: one row per key, Key's string, holding its value, never empty, and written_at, the
-- time of the Put that last wrote it on the database's clock, by which a host prunes with its own
-- SQL. It has no other index and no version column: Version is in Key's hash. The DEFAULT stands
-- for a row a host writes itself; Put sets written_at to now() explicitly.
CREATE TABLE docinfer_cache (
    key        text        NOT NULL,
    value      bytea       NOT NULL,
    written_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT docinfer_pk_cache PRIMARY KEY (key),
    CONSTRAINT docinfer_ck_cache_value CHECK (octet_length(value) > 0)
);
