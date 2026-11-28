package app

import (
	"encoding/json/jsontext"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/report"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/run"
)

func defineDomain(g *graph.Graph, n *Nodes) {
	n.RunConfig = g.Define("run config", func(s *graph.Scope) (run.Config, error) {
		cfg := s.Use(n.Config)
		e := cfg.Embeddings
		rc := run.Config{
			Settings: profile.Settings{
				Embeddings: profile.Embeddings{
					Capability: capability(e.Capability),
					Dimensions: e.Dimensions,
					Tokenizer:  e.Tokenizer,
					Batch:      e.Batch,
					ChunkSize:  e.ChunkSize,
					ChunkRunes: e.ChunkRunes,
				},
				Pending:        cfg.Run.Pending,
				BatchBytes:     cfg.Run.BatchBytes,
				WordsPerSecond: new(startWordsPerSecond),
			},
			Clients: profile.Clients{
				Vision:        s.Use(n.VisionClient),
				Transcription: s.Use(n.TranscriptionClient),
				Extraction:    s.Use(n.ExtractionClient),
				Embeddings:    s.Use(n.EmbeddingsClient),
				Tokenizer:     s.Use(n.TokenizerClient),
			},
			Endpoints:   map[docinfer.Capability]run.Endpoint{docinfer.CapabilityEmbeddings: endpoint(cfg, e.Endpoint)},
			Concurrency: map[docinfer.Capability]int{docinfer.CapabilityEmbeddings: *e.Concurrency},
		}
		rc.Build.Command, rc.Build.Library = versions()
		if v := cfg.Vision; v != nil {
			rc.Settings.Vision = &profile.Vision{
				Capability: capability(v.Capability),
				Prompt:     v.Prompt,
				MaxTokens:  v.MaxTokens,
				Reasoning:  v.Reasoning,
				Native:     jsontext.Value(v.Native),
				MaxPixels:  v.MaxPixels,
				LayerWords: v.LayerWords,
			}
			rc.Endpoints[docinfer.CapabilityVision] = endpoint(cfg, v.Endpoint)
			rc.Concurrency[docinfer.CapabilityVision] = *v.Concurrency
		}
		if t := cfg.Transcription; t != nil {
			rc.Settings.Transcription = &profile.Transcription{
				Capability: capability(t.Capability),
				Prompt:     t.Prompt,
				Language:   t.Language,
				MaxTokens:  t.MaxTokens,
				Native:     jsontext.Value(t.Native),
			}
			rc.Settings.WordsPerSecond = t.WordsPerSecond
			rc.Endpoints[docinfer.CapabilityTranscription] = endpoint(cfg, t.Endpoint)
			rc.Concurrency[docinfer.CapabilityTranscription] = *t.Concurrency
		}
		if x := cfg.Extraction; x != nil {
			rc.Settings.Extraction = &profile.Extraction{
				Capability:  capability(x.Capability),
				MaxTokens:   x.MaxTokens,
				Reasoning:   x.Reasoning,
				Native:      jsontext.Value(x.Native),
				WindowRunes: x.WindowRunes,
			}
			rc.Endpoints[docinfer.CapabilityExtraction] = endpoint(cfg, x.Endpoint)
			rc.Concurrency[docinfer.CapabilityExtraction] = *x.Concurrency
		}
		return rc, nil
	})
}

func capability(c Capability) profile.Capability {
	return profile.Capability{Endpoint: c.Endpoint, Model: c.Model, ServedModel: c.ServedModel}
}

func endpoint(cfg Config, name string) run.Endpoint {
	return run.Endpoint{Name: name, BaseURL: cfg.Endpoints[name].BaseURL, Format: cfg.Endpoints[name].Format}
}

func mountDomain(root *cli.Command, n *Nodes) {
	root.Add(run.Commands(n.RunConfig)...)
	root.Add(report.Commands()...)
}
