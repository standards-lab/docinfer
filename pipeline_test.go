package docinfer_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/protocol"
)

func newClient(t *testing.T, baseURL string, mode ai.TranscriptionMode) *ai.Client {
	t.Helper()
	c, err := ai.New(baseURL, http.DefaultClient, completions.New(completions.Options{}), ai.Options{TranscriptionMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newBounds(t *testing.T, vision, transcription, extraction, embeddings int) *docinfer.Bounds {
	t.Helper()
	b, err := docinfer.NewBounds(vision, transcription, extraction, embeddings)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validConfig(t *testing.T) docinfer.Config {
	t.Helper()
	c := newClient(t, "http://localhost/v1", ai.TranscribeRoute)
	return docinfer.Config{
		Vision: &docinfer.Vision{
			Client: c, Model: "vision", Served: "org/vision-1", Prompt: "Describe the image.", MaxTokens: 2,
			Reasoning: protocol.ReasoningEffortLow,
		},
		Transcription: &docinfer.Transcription{Client: c, Model: "audio", Served: "org/transcription-1", Language: "en"},
		Extraction: &docinfer.Extraction{
			Client: c, Model: "agent", Served: "org/extraction-1", MaxTokens: 2, Reasoning: protocol.ReasoningEffortMedium,
		},
		Embeddings: docinfer.Embeddings{Client: c, Model: "embedding", Served: "org/embedding-1"},
		Chunker:    chunk.Splitter{Bound: 2, Count: runes, Model: "org/embedding-1"},
		Dimensions: 2,
		Cache:      docinfer.NewMemoryCache(),
		Bounds:     newBounds(t, 1, 1, 1, 1),
		Pending:    2,
		EmbedBatch: 2,
		BatchBytes: 2,
		Join:       join.Request{WordsPerSecond: 2},
	}
}

func TestNewAcceptsLeastValues(t *testing.T) {
	cfg := validConfig(t)
	cfg.Vision.MaxTokens, cfg.Extraction.MaxTokens = 1, 1
	cfg.Chunker.Bound, cfg.Dimensions, cfg.Pending, cfg.EmbedBatch, cfg.BatchBytes = 1, 1, 1, 1, 1
	cfg.Join.WordsPerSecond = 1
	if _, err := docinfer.New(cfg); err != nil {
		t.Fatalf("New with every count 1: %v", err)
	}
	cfg.Vision, cfg.Transcription, cfg.Extraction = nil, nil, nil
	if _, err := docinfer.New(cfg); err != nil {
		t.Fatalf("New with no vision, transcription, or extraction: %v", err)
	}
}

func TestNewPanicsNamingTheField(t *testing.T) {
	cases := []struct {
		field string
		unset func(*docinfer.Config)
	}{
		{"Cache", func(c *docinfer.Config) { c.Cache = nil }},
		{"Bounds", func(c *docinfer.Config) { c.Bounds = nil }},
		{"Embeddings.Client", func(c *docinfer.Config) { c.Embeddings.Client = nil }},
		{"Vision.Client", func(c *docinfer.Config) { c.Vision.Client = nil }},
		{"Transcription.Client", func(c *docinfer.Config) { c.Transcription.Client = nil }},
		{"Extraction.Client", func(c *docinfer.Config) { c.Extraction.Client = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			cfg := validConfig(t)
			tc.unset(&cfg)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("New with a nil %s didn't panic", tc.field)
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, "nil "+tc.field) {
					t.Errorf("New with a nil %s panicked with %q, which doesn't name it", tc.field, msg)
				}
			}()
			_, _ = docinfer.New(cfg)
		})
	}
}

func TestNewRefusesNamingTheField(t *testing.T) {
	counts := []struct {
		field string
		set   func(*docinfer.Config, int)
	}{
		{"Chunker.Bound", func(c *docinfer.Config, n int) { c.Chunker.Bound = n }},
		{"Dimensions", func(c *docinfer.Config, n int) { c.Dimensions = n }},
		{"Pending", func(c *docinfer.Config, n int) { c.Pending = n }},
		{"EmbedBatch", func(c *docinfer.Config, n int) { c.EmbedBatch = n }},
		{"BatchBytes", func(c *docinfer.Config, n int) { c.BatchBytes = n }},
		{"Join.WordsPerSecond", func(c *docinfer.Config, n int) { c.Join.WordsPerSecond = n }},
		{"Vision.MaxTokens", func(c *docinfer.Config, n int) { c.Vision.MaxTokens = n }},
		{"Extraction.MaxTokens", func(c *docinfer.Config, n int) { c.Extraction.MaxTokens = n }},
	}
	for _, tc := range counts {
		for _, n := range []int{0, -1} {
			t.Run(fmt.Sprintf("%s=%d", tc.field, n), func(t *testing.T) {
				cfg := validConfig(t)
				tc.set(&cfg, n)
				refused(t, cfg, tc.field)
			})
		}
	}
	models := []struct {
		field string
		clear func(*docinfer.Config)
	}{
		{"Embeddings.Model", func(c *docinfer.Config) { c.Embeddings.Model = "" }},
		{"Vision.Model", func(c *docinfer.Config) { c.Vision.Model = "" }},
		{"Transcription.Model", func(c *docinfer.Config) { c.Transcription.Model = "" }},
		{"Extraction.Model", func(c *docinfer.Config) { c.Extraction.Model = "" }},
		{"Embeddings.Served", func(c *docinfer.Config) { c.Embeddings.Served = "" }},
		{"Vision.Served", func(c *docinfer.Config) { c.Vision.Served = "" }},
		{"Transcription.Served", func(c *docinfer.Config) { c.Transcription.Served = "" }},
		{"Extraction.Served", func(c *docinfer.Config) { c.Extraction.Served = "" }},
		{"Chunker.Model", func(c *docinfer.Config) { c.Chunker.Model = "" }},
		{"Chunker.Count", func(c *docinfer.Config) { c.Chunker.Count = nil }},
	}
	for _, tc := range models {
		t.Run(tc.field+"=empty", func(t *testing.T) {
			cfg := validConfig(t)
			tc.clear(&cfg)
			refused(t, cfg, tc.field)
		})
	}
}

func refused(t *testing.T, cfg docinfer.Config, field string) {
	t.Helper()
	p, err := docinfer.New(cfg)
	if !errors.Is(err, docinfer.ErrRequest) {
		t.Fatalf("New with a bad %s: %v, want ErrRequest", field, err)
	}
	if p != nil {
		t.Errorf("New with a bad %s returned a pipeline beside its error", field)
	}
	if !strings.Contains(err.Error(), field) {
		t.Errorf("New with a bad %s: %q doesn't name it", field, err)
	}
}

func TestNewBoundsRefuses(t *testing.T) {
	for i, capability := range []string{"vision", "transcription", "extraction", "embeddings"} {
		for _, n := range []int{0, -1} {
			t.Run(fmt.Sprintf("%s=%d", capability, n), func(t *testing.T) {
				bounds := []int{1, 1, 1, 1}
				bounds[i] = n
				b, err := docinfer.NewBounds(bounds[0], bounds[1], bounds[2], bounds[3])
				if !errors.Is(err, docinfer.ErrRequest) {
					t.Fatalf("NewBounds%v: %v, want ErrRequest", bounds, err)
				}
				if b != nil {
					t.Errorf("NewBounds%v returned Bounds beside its error", bounds)
				}
				if !strings.Contains(err.Error(), capability) {
					t.Errorf("NewBounds%v: %q doesn't name %s", bounds, err, capability)
				}
			})
		}
	}
}
