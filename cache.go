package docinfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
)

// Cache holds one result per [Key]: an image's, a window's, a chunk's, or an extraction's model
// result, or a text's chunk spans, each a [Held] as JSON. docinfer reads and replaces values and never removes one; a host removes
// what it no longer wants from its own store. docinfertest.Cache is the contract every
// implementation passes.
type Cache interface {
	// Get returns keys' values in one call, values[i] keys[i]'s, nil for a miss; a held value is
	// never empty.
	Get(ctx context.Context, keys ...string) ([][]byte, error)
	// Put holds value under key, replacing a held value. An empty value is refused, matching
	// [ErrRequest].
	Put(ctx context.Context, key string, value []byte) error
}

// Key is the cache key of content's result under id: "<capability>/" and the lowercase hex
// SHA-256 of [Version], id's Capability, Model, and Settings, and content, each prefixed by its
// length in bytes as a big-endian uint64. content is the content's SHA-256: the image's, the
// window's, the source text's, or the chunk's text's. Since Version is hashed, a new shape of
// [Held] or of a key's parts misses rather than decoding wrong.
func Key(id Identity, content string) string {
	return string(id.Capability) + "/" + hashParts(Version, string(id.Capability), id.Model, id.Settings, content)
}

// Held is a held value's JSON, the bytes a [Cache] holds under a [Key]: one model result, or one
// text's chunk spans. A refusal and an incomplete reply are held; a failure never is.
type Held struct {
	Text       string `json:"text,omitzero"`       // vision, transcription, extraction: inline
	Vector     string `json:"vector,omitzero"`     // embeddings: as Chunk.Vector
	Incomplete string `json:"incomplete,omitzero"` // why the model stopped: max_output_tokens
	Refused    string `json:"refused,omitzero"`    // the refusal's text, or the refusing reply's
	// Spans is chunking's: each chunk's [start, end) bytes of the source text, in order, [] for a
	// text of no chunks.
	Spans [][2]int `json:"spans,omitzero"`
}

func decodeHeld(b []byte) (Held, bool) {
	if trimmed := bytes.TrimLeft(b, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return Held{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var h Held
	if err := dec.Decode(&h); err != nil {
		return Held{}, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return Held{}, false
	}
	if h.Vector != "" {
		if _, err := DecodeVector(h.Vector); err != nil {
			return Held{}, false
		}
	}
	return h, true
}

// MemoryCache is a [Cache] in memory, for tests and a host whose results fit there, safe for
// concurrent use. It holds a copy of each value and returns a copy, so neither side's slice is
// shared. Its zero value is not ready; [NewMemoryCache] makes one.
type MemoryCache struct {
	mu     sync.RWMutex
	values map[string][]byte
}

// NewMemoryCache returns an empty MemoryCache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{values: map[string][]byte{}}
}

// Get returns a copy of each key's held value, nil for a miss.
func (c *MemoryCache) Get(ctx context.Context, keys ...string) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values := make([][]byte, len(keys))
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i, k := range keys {
		if v, ok := c.values[k]; ok {
			values[i] = slices.Clone(v)
		}
	}
	return values, nil
}

// Put holds a copy of value under key, replacing a held value.
func (c *MemoryCache) Put(ctx context.Context, key string, value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("docinfer: memory cache: put %s: empty value: %w", key, ErrRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	v := slices.Clone(value)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = v
	return nil
}
