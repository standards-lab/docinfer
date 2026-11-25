package app

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-ai"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/report"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/run"
)

func defineDomain(g *graph.Graph, n *Nodes) {
	n.RunConfig = g.Define("run config", func(s *graph.Scope) (run.Config, error) {
		cfg := s.Use(n.Config)
		e := cfg.Embeddings
		rc := run.Config{
			Embeddings:  docinfer.Embeddings{Client: s.Use(n.EmbeddingsClient), Model: e.Model, Served: e.ServedModel},
			Endpoints:   map[docinfer.Capability]run.Endpoint{docinfer.CapabilityEmbeddings: endpoint(cfg, e.Endpoint)},
			Concurrency: map[docinfer.Capability]int{docinfer.CapabilityEmbeddings: *e.Concurrency},
			Chunker:     chunk.Splitter{Bound: *e.ChunkSize, Runes: e.chunkRunes(), Count: counter(s.Use(n.TokenizerClient), e.Model)},
			Dimensions:  *e.Dimensions,
			EmbedBatch:  *e.Batch,
			Pending:     *cfg.Run.Pending,
			BatchBytes:  *cfg.Run.BatchBytes,
			Join:        join.Request{WordsPerSecond: startWordsPerSecond},
		}
		rc.Build.Command, rc.Build.Library = versions()
		if v := cfg.Vision; v != nil {
			rc.Vision = &docinfer.Vision{
				Client:    s.Use(n.VisionClient),
				Model:     v.Model,
				Served:    v.ServedModel,
				Prompt:    v.Prompt,
				MaxTokens: *v.MaxTokens,
				Reasoning: v.Reasoning,
				Native:    native(v.Native),
			}
			rc.Endpoints[docinfer.CapabilityVision] = endpoint(cfg, v.Endpoint)
			rc.Concurrency[docinfer.CapabilityVision] = *v.Concurrency
		}
		if t := cfg.Transcription; t != nil {
			rc.Transcription = &docinfer.Transcription{
				Client:    s.Use(n.TranscriptionClient),
				Model:     t.Model,
				Served:    t.ServedModel,
				Prompt:    t.Prompt,
				Language:  t.Language,
				MaxTokens: *t.MaxTokens,
				Native:    native(t.Native),
			}
			rc.Endpoints[docinfer.CapabilityTranscription] = endpoint(cfg, t.Endpoint)
			rc.Concurrency[docinfer.CapabilityTranscription] = *t.Concurrency
			rc.Join.WordsPerSecond = *t.WordsPerSecond
		}
		return rc, nil
	})
}

func native(v json.RawMessage) jsontext.Value {
	var members map[string]jsontext.Value
	if jsonv2.Unmarshal(v, &members) == nil && len(members) == 0 {
		return nil
	}
	return jsontext.Value(v)
}

func counter(client *ai.Client, model string) chunk.Counter {
	return func(ctx context.Context, text string) (int, error) {
		tokens, err := client.Tokenize(ctx, &ai.TokenizeRequest{Model: model, Text: text, InputType: ai.InputDocument})
		if err != nil {
			return 0, err
		}
		return tokens.Count, nil
	}
}

func endpoint(cfg Config, name string) run.Endpoint {
	return run.Endpoint{Name: name, BaseURL: cfg.Endpoints[name].BaseURL, Format: cfg.Endpoints[name].Format}
}

func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(run.Commands(n.RunConfig)...)
	root.Add(report.Commands()...)
}
