package docinfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileCache is a [Cache] in a directory, on the standard library alone, safe for concurrent use
// within a process and across processes sharing the directory. Each key's value is the file
// <dir>/<capability>/<xx>/<hash>.json, xx the hash's first two hex digits. A put writes a
// temporary file beside the value's, its name beginning ".tmp-", synced, and renames it into
// place, so a reader sees a whole value or the one it replaced, never part of one; a temporary
// file a process that died left is never read, and is left for the host to remove, since another
// process may be writing it. Its zero value is not ready; [NewFileCache] makes one.
type FileCache struct {
	dir string
}

// NewFileCache returns a FileCache in dir, making dir when it doesn't exist. An empty dir is
// refused, matching [ErrRequest].
func NewFileCache(dir string) (*FileCache, error) {
	if dir == "" {
		return nil, fmt.Errorf("docinfer: file cache: no directory: %w", ErrRequest)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("docinfer: file cache: %w", err)
	}
	return &FileCache{dir: dir}, nil
}

// path returns key's file, refusing a key not of [Key]'s form, "<capability>/<hash>" with a
// capability of lowercase letters and a hash of 64 lowercase hex digits, so no key names a file
// outside the directory.
func (c *FileCache) path(key string) (string, error) {
	capability, hash, ok := strings.Cut(key, "/")
	if !ok || !isCapability(capability) || !isHash(hash) {
		return "", fmt.Errorf("docinfer: file cache: key %q isn't of Key's form: %w", key, ErrRequest)
	}
	return filepath.Join(c.dir, capability, hash[:2], hash+".json"), nil
}

// isCapability reports whether s is one or more lowercase ASCII letters.
func isCapability(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// isHash reports whether s is a SHA-256 as lowercase hex.
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// Get reads each key's file, nil for a key with none. An empty file, which a put never leaves, is
// a miss too.
func (c *FileCache) Get(ctx context.Context, keys ...string) ([][]byte, error) {
	values := make([][]byte, len(keys))
	for i, k := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := c.path(k)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("docinfer: file cache: %w", err)
		}
		if len(b) > 0 {
			values[i] = b
		}
	}
	return values, nil
}

// Put writes value to key's file, as FileCache says, replacing a held value.
func (c *FileCache) Put(ctx context.Context, key string, value []byte) error {
	path, err := c.path(key)
	if err != nil {
		return err
	}
	if len(value) == 0 {
		return fmt.Errorf("docinfer: file cache: put %s: empty value: %w", key, ErrRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("docinfer: file cache: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("docinfer: file cache: %w", err)
	}
	_, err = tmp.Write(value)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name()) // the put failed: the temporary file is all it leaves
		return fmt.Errorf("docinfer: file cache: put %s: %w", key, err)
	}
	return nil
}
