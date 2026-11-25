package docinfer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

const spansText = countedText + "\n\nUn café crème, s'il vous plaît. Merci beaucoup, à bientôt."

type countingCounter struct{ calls atomic.Int64 }

func (c *countingCounter) count(_ context.Context, text string) (int, error) {
	c.calls.Add(1)
	return wordTokens(text), nil
}

type spansRun struct {
	cache   docinfer.Cache
	content *dispatch.MemoryContent
	text    string
}

func (s spansRun) run(t *testing.T, splitter chunk.Splitter) (chunks []docinfer.Chunk, counts int64, embeds int) {
	t.Helper()
	var requests embedRequests
	counter := &countingCounter{}
	e := aitest.Serve(t, aitest.Routes{Embeddings: requests.route})
	cfg := runConfig(t, e, func(c *docinfer.Config) {
		c.Cache = s.cache
		c.Chunker = splitter
		c.Chunker.Count = counter.count
	})
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), s.content,
		seq(outputInput(t, s.content, childPart, dispatch.TextOutput, s.text)), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	for _, p := range o.chunks() {
		chunks = append(chunks, p.chunk)
	}
	slices.SortFunc(chunks, func(a, b docinfer.Chunk) int { return a.Index - b.Index })
	return chunks, counter.calls.Load(), len(requests.all())
}

func chunkIdentity(splitter chunk.Splitter) docinfer.Identity {
	return docinfer.Identity{Capability: docinfer.CapabilityChunk, Model: splitter.Model, Settings: splitter.Settings()}
}

func chunkKey(splitter chunk.Splitter, text string) string {
	return docinfer.Key(chunkIdentity(splitter), sha(text))
}

func heldValue(t *testing.T, c docinfer.Cache, key string) []byte {
	t.Helper()
	values, err := c.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	return values[0]
}

func TestRunHoldsChunkSpans(t *testing.T) {
	caches := map[string]func(t *testing.T) docinfer.Cache{
		"MemoryCache": func(*testing.T) docinfer.Cache { return docinfer.NewMemoryCache() },
		"FileCache":   func(t *testing.T) docinfer.Cache { return newFileCache(t, t.TempDir()) },
	}
	for name, newCache := range caches {
		t.Run(name, func(t *testing.T) {
			s := spansRun{cache: newCache(t), content: dispatch.NewMemoryContent(), text: spansText}
			splitter := chunk.Splitter{Bound: 10, Model: embeddingServed}

			first, counts, embeds := s.run(t, splitter)
			if counts == 0 || embeds == 0 || len(first) < 2 {
				t.Fatalf("the first run: %d counts, %d embedding requests, %d chunks; want the text counted, cut, and embedded",
					counts, embeds, len(first))
			}
			held := heldValue(t, s.cache, chunkKey(splitter, spansText))
			if held == nil {
				t.Fatal("the first run held no spans under the chunker's identity and the text's SHA-256")
			}

			again, counts, embeds := s.run(t, splitter)
			if counts != 0 || embeds != 0 {
				t.Errorf("the rerun: %d counts and %d embedding requests, want none", counts, embeds)
			}
			if !slices.Equal(again, first) {
				t.Errorf("the rerun's chunks %+v, want the first run's %+v", again, first)
			}

			for what, other := range map[string]chunk.Splitter{
				"another bound": {Bound: 11, Model: embeddingServed},
				"another model": {Bound: 10, Model: "org/other-tokenizer"},
			} {
				if _, counts, _ := s.run(t, other); counts == 0 {
					t.Errorf("a run with %s counted nothing, want the held spans missed", what)
				}
			}

			mid := strings.Index(spansText, "é") + 1
			for what, corrupt := range map[string]string{
				"out of range": `{"spans":[[0,100000]]}`,
				"unordered":    `{"spans":[[30,40],[0,10]]}`,
				"overlapping":  `{"spans":[[0,20],[10,30]]}`,
				"empty span":   `{"spans":[[5,5]]}`,
				"negative":     `{"spans":[[-1,5]]}`,
				"mid-rune":     fmt.Sprintf(`{"spans":[[0,%d]]}`, mid),
				"no spans":     `{"text":"a"}`,
				"not a Held":   `{"spans":[[0,5]],"extra":1}`,
			} {
				if err := s.cache.Put(t.Context(), chunkKey(splitter, spansText), []byte(corrupt)); err != nil {
					t.Fatal(err)
				}
				got, counts, _ := s.run(t, splitter)
				if counts == 0 || !slices.Equal(got, first) {
					t.Errorf("held spans %s: %d counts and chunks %+v, want them recomputed as %+v", what, counts, got, first)
				}
				if v := heldValue(t, s.cache, chunkKey(splitter, spansText)); !bytes.Equal(v, held) {
					t.Errorf("held spans %s: the cache holds %s after the run, want the recomputed %s", what, v, held)
				}
			}
		})
	}
}

func TestRunHoldsNoChunks(t *testing.T) {
	s := spansRun{cache: docinfer.NewMemoryCache(), content: dispatch.NewMemoryContent(), text: " \n\t "}
	splitter := chunk.Splitter{Bound: 10, Model: embeddingServed}
	if chunks, _, _ := s.run(t, splitter); len(chunks) != 0 {
		t.Fatalf("a blank text: %d chunks, want none", len(chunks))
	}
	if v := heldValue(t, s.cache, chunkKey(splitter, s.text)); string(v) != `{"spans":[]}` {
		t.Errorf("a blank text's held spans: %q, want {\"spans\":[]}", v)
	}
	if chunks, counts, embeds := s.run(t, splitter); len(chunks) != 0 || counts != 0 || embeds != 0 {
		t.Errorf("the rerun of a blank text: %d chunks, %d counts, %d embedding requests, want none", len(chunks), counts, embeds)
	}
}

func TestRunHoldsNoSpansOnACountFailure(t *testing.T) {
	cache := docinfer.NewMemoryCache()
	content := dispatch.NewMemoryContent()
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	splitter := chunk.Splitter{Bound: 10, Model: embeddingServed, Count: func(context.Context, string) (int, error) {
		return 0, errors.New("no count")
	}}
	cfg := runConfig(t, e, func(c *docinfer.Config) { c.Cache, c.Chunker = cache, splitter })
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content,
		seq(outputInput(t, content, childPart, dispatch.TextOutput, spansText)), docinfer.Options{}))
	if o.err == nil {
		t.Fatal("Run with a failing count: no error")
	}
	if v := heldValue(t, cache, chunkKey(splitter, spansText)); v != nil {
		t.Errorf("a failed count held %s, want nothing", v)
	}
}
