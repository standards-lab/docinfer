package docinfer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/docinfertest"
)

func TestCache(t *testing.T) {
	t.Run("MemoryCache", func(t *testing.T) {
		docinfertest.Cache(t, func(*testing.T) docinfer.Cache { return docinfer.NewMemoryCache() })
	})
	t.Run("FileCache", func(t *testing.T) {
		docinfertest.Cache(t, func(t *testing.T) docinfer.Cache { return newFileCache(t, t.TempDir()) })
	})
}

func newFileCache(t *testing.T, dir string) *docinfer.FileCache {
	t.Helper()
	c, err := docinfer.NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewFileCacheRefusesNoDirectory(t *testing.T) {
	if _, err := docinfer.NewFileCache(""); !errors.Is(err, docinfer.ErrRequest) {
		t.Errorf("NewFileCache(\"\"): %v, want ErrRequest", err)
	}
}

func TestFileCacheLayout(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "made")
	c := newFileCache(t, dir)
	key := docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "m", Settings: `{"request":"1"}`}, sha("image"))
	value := []byte(`{"text":"a cat"}`)
	if err := c.Put(ctx, key, value); err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimPrefix(key, "vision/")
	path := filepath.Join(dir, "vision", hash[:2], hash+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the value's file: %v", err)
	}
	if !bytes.Equal(b, value) {
		t.Errorf("%s holds %q, want %q", path, b, value)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%s holds %d files, want the value's alone", filepath.Dir(path), len(entries))
	}
	got, err := newFileCache(t, dir).Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0], value) {
		t.Errorf("a new FileCache over the directory reads %q, want %q", got[0], value)
	}
}

func TestFileCacheRefusesForeignKeys(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	c := newFileCache(t, dir)
	hash := strings.TrimPrefix(docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityEmbeddings}, sha("x")), "embeddings/")
	for _, key := range []string{
		"", hash, "embeddings/", "/" + hash, "Embeddings/" + hash, "embeddings/" + strings.ToUpper(hash),
		"embeddings/" + hash[:63], "embeddings/" + hash + "0", "../" + hash, "embeddings/../" + hash[3:],
		"embeddings/x/" + hash[2:],
	} {
		if err := c.Put(ctx, key, []byte("{}")); !errors.Is(err, docinfer.ErrRequest) {
			t.Errorf("put %q: %v, want ErrRequest", key, err)
		}
		if _, err := c.Get(ctx, key); !errors.Is(err, docinfer.ErrRequest) {
			t.Errorf("get %q: %v, want ErrRequest", key, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("refused puts left %d entries in the directory", len(entries))
	}
}

func TestHeldJSON(t *testing.T) {
	for _, tc := range []struct {
		held docinfer.Held
		want string
	}{
		{docinfer.Held{}, `{}`},
		{docinfer.Held{Text: "a cat"}, `{"text":"a cat"}`},
		{docinfer.Held{Vector: "AACAPw=="}, `{"vector":"AACAPw=="}`},
		{docinfer.Held{Text: "a ca", Incomplete: "max_output_tokens"}, `{"text":"a ca","incomplete":"max_output_tokens"}`},
		{docinfer.Held{Refused: "I can't help with that."}, `{"refused":"I can't help with that."}`},
	} {
		b, err := json.Marshal(tc.held)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.want {
			t.Errorf("%+v encodes as %s, want %s", tc.held, b, tc.want)
		}
	}
}
