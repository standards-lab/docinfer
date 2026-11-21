package docinfertest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
)

// Cache runs the cache contract against the caches newCache makes, a new, empty one for each case.
func Cache(t *testing.T, newCache func(t *testing.T) docinfer.Cache) {
	t.Helper()
	ctx := context.Background()

	t.Run("a miss is nil", func(t *testing.T) {
		c := newCache(t)
		got := get(t, c, key("absent"))
		if len(got) != 1 || got[0] != nil {
			t.Errorf("get of an absent key: %q, want [nil]", got)
		}
		if got := get(t, c); len(got) != 0 {
			t.Errorf("get of no keys: %q, want none", got)
		}
	})

	t.Run("a get of many keys answers each in order", func(t *testing.T) {
		c := newCache(t)
		a, b, absent := key("a"), key("b"), key("absent")
		put(t, c, a, []byte(`{"text":"a"}`))
		put(t, c, b, []byte(`{"text":"b"}`))
		got := get(t, c, b, absent, a, b)
		want := [][]byte{[]byte(`{"text":"b"}`), nil, []byte(`{"text":"a"}`), []byte(`{"text":"b"}`)}
		if !equal(got, want) {
			t.Errorf("get of b, an absent key, a, b: %q, want %q", got, want)
		}
	})

	t.Run("a put replaces", func(t *testing.T) {
		c := newCache(t)
		k := key("replaced")
		put(t, c, k, []byte(`{"text":"the first, longer value"}`))
		put(t, c, k, []byte(`{"text":"second"}`))
		if got := get(t, c, k); !equal(got, [][]byte{[]byte(`{"text":"second"}`)}) {
			t.Errorf("get after a second put: %q, want the second value", got)
		}
	})

	t.Run("values come back whole", func(t *testing.T) {
		c := newCache(t)
		large := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
		binary := make([]byte, 256)
		for i := range binary {
			binary[i] = byte(i)
		}
		for i, v := range [][]byte{large, binary, {'x'}} {
			k := key(fmt.Sprint("whole ", i))
			in := slices.Clone(v)
			put(t, c, k, in)
			in[0]++
			got := get(t, c, k)
			if !equal(got, [][]byte{v}) {
				t.Fatalf("value %d: got %d bytes back, want its %d bytes", i, len(got[0]), len(v))
			}
			got[0][0]++
			if again := get(t, c, k); !equal(again, [][]byte{v}) {
				t.Errorf("value %d: changed by a change to a slice put or got", i)
			}
		}
	})

	t.Run("an empty value is refused", func(t *testing.T) {
		c := newCache(t)
		k := key("empty")
		for _, v := range [][]byte{nil, {}} {
			if err := c.Put(ctx, k, v); !errors.Is(err, docinfer.ErrRequest) {
				t.Errorf("put of %q: %v, want ErrRequest", v, err)
			}
		}
		if got := get(t, c, k); got[0] != nil {
			t.Errorf("get after a refused put: %q, want a miss", got[0])
		}
	})

	t.Run("the same content under each capability is four keys", func(t *testing.T) {
		c := newCache(t)
		caps := []docinfer.Capability{
			docinfer.CapabilityVision, docinfer.CapabilityTranscription,
			docinfer.CapabilityExtraction, docinfer.CapabilityEmbeddings,
		}
		var keys []string
		var want [][]byte
		for _, cp := range caps {
			k := docinfer.Key(docinfer.Identity{Capability: cp, Model: "m"}, hash("shared"))
			v := []byte(`{"text":"` + string(cp) + `"}`)
			put(t, c, k, v)
			keys, want = append(keys, k), append(want, v)
		}
		if got := get(t, c, keys...); !equal(got, want) {
			t.Errorf("get of the four keys: %q, want %q", got, want)
		}
	})

	t.Run("an ended context fails get and put", func(t *testing.T) {
		c := newCache(t)
		k := key("ended")
		ended, cancel := context.WithCancel(ctx)
		cancel()
		if err := c.Put(ended, k, []byte(`{"text":"x"}`)); !errors.Is(err, context.Canceled) {
			t.Errorf("put under an ended context: %v, want context.Canceled", err)
		}
		if _, err := c.Get(ended, k); !errors.Is(err, context.Canceled) {
			t.Errorf("get under an ended context: %v, want context.Canceled", err)
		}
		if got := get(t, c, k); got[0] != nil {
			t.Errorf("get after a put under an ended context: %q, want a miss", got[0])
		}
	})

	t.Run("concurrent puts and gets", func(t *testing.T) {
		c := newCache(t)
		shared := key("shared")
		values := make([][]byte, 8)
		for i := range values {
			values[i] = bytes.Repeat([]byte{byte('a' + i)}, 1024*(i+1))
		}
		var wg sync.WaitGroup
		errs := make(chan error, 3*len(values))
		for i, v := range values {
			own := key(fmt.Sprint("own ", i))
			wg.Go(func() { errs <- c.Put(ctx, shared, v) })
			wg.Go(func() { errs <- c.Put(ctx, own, v) })
			wg.Go(func() {
				got, err := c.Get(ctx, shared, own)
				if err == nil && (!whole(got[0], values) || !whole(got[1], values)) {
					err = errors.New("a get during puts read a value not put whole")
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("concurrent puts and gets: %v", err)
			}
		}
		if got := get(t, c, shared); got[0] == nil || !whole(got[0], values) {
			t.Errorf("get after concurrent puts: %d bytes, want one value put whole", len(got[0]))
		}
		for i, v := range values {
			if got := get(t, c, key(fmt.Sprint("own ", i))); !equal(got, [][]byte{v}) {
				t.Errorf("get of key %d after concurrent puts: %d bytes, want its %d", i, len(got[0]), len(v))
			}
		}
	})
}

func key(name string) string {
	return docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "docinfertest"}, hash(name))
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func put(t *testing.T, c docinfer.Cache, k string, value []byte) {
	t.Helper()
	if err := c.Put(context.Background(), k, value); err != nil {
		t.Fatalf("put %s: %v", k, err)
	}
}

func get(t *testing.T, c docinfer.Cache, keys ...string) [][]byte {
	t.Helper()
	got, err := c.Get(context.Background(), keys...)
	if err != nil {
		t.Fatalf("get %q: %v", keys, err)
	}
	if len(got) != len(keys) {
		t.Fatalf("get of %d keys: %d values", len(keys), len(got))
	}
	return got
}

func equal(got, want [][]byte) bool {
	return slices.EqualFunc(got, want, func(g, w []byte) bool {
		return (g == nil) == (w == nil) && bytes.Equal(g, w)
	})
}

func whole(v []byte, values [][]byte) bool {
	return v == nil || slices.ContainsFunc(values, func(w []byte) bool { return bytes.Equal(v, w) })
}
