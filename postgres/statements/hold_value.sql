--| tier: native
--| native: INSERT ... ON CONFLICT DO UPDATE, so a first put and a replacement are one statement that never fails on the key's row existing. Port: an engine must insert or replace in one statement (SQL Server's MERGE, retried on a unique violation two first puts of a key can still raise), setting written_at to the engine's current timestamp.
-- Holds value under key, Put's write: inserts key's row, or replaces a held value. written_at
-- is the database's clock at the put, on an insert and a replacement alike.
INSERT INTO docinfer_cache (key, value, written_at)
VALUES ({{key}}, {{value}}, now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, written_at = now()
