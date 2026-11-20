--| tier: native
--| native: = ANY over a text[] parameter, so a lookup of any number of keys is one statement with one bound value. Port: an engine without array parameters must bind the keys as an IN list expanded per key count.
-- Reads the held values of keys, Key's strings bound as one text[]: Get's read. A key with no row
-- has none returned, and a key bound twice is returned once; Get maps the rows back to the keys
-- asked.
SELECT key, value
FROM docinfer_cache
WHERE key = ANY({{keys}})
