package run

import (
	"context"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
)

// Config is what the run command runs under, in the library's shapes: each capability the
// configuration sets, with the client the composition root built for it, and the pipeline's
// settings, each at the configuration's value or its starting value.
type Config struct {
	// Vision describes images; nil skips them.
	Vision *docinfer.Vision
	// Transcription transcribes audio windows; nil skips them.
	Transcription *docinfer.Transcription
	// Embeddings embeds chunks.
	Embeddings docinfer.Embeddings
	// Endpoints names each set capability's endpoint, its key in the configuration's endpoints.
	Endpoints map[docinfer.Capability]string
	// Concurrency is each set capability's most requests in flight.
	Concurrency map[docinfer.Capability]int
	// Chunker cuts each text into the chunks Embeddings embeds.
	Chunker chunk.Splitter
	// Dimensions is the embedding space's.
	Dimensions int
	// EmbedBatch is the texts one embeddings request carries.
	EmbedBatch int
	// Pending is a job's inputs admitted and not yet finished.
	Pending int
	// BatchBytes is a batch's marshalled JSON's cap.
	BatchBytes int
	// Join is how window transcripts are merged.
	Join join.Request
}

// Commands returns the run command, over the configuration cfg's node holds, which it declares.
func Commands(cfg *graph.Node[Config]) []*cli.Command {
	return []*cli.Command{command(cfg)}
}

func command(cfg *graph.Node[Config]) *cli.Command {
	var out, id string
	cmd := &cli.Command{
		Name:     "run",
		Summary:  "Infer over a docproc run directory's latest run that ended, or --run's",
		Synopsis: "<docproc-dir>",
		Args:     cli.ExactArgs(1),
		Validate: func(inv *cli.Invocation) error {
			if out == "" {
				return cli.Usagef("--out names no directory")
			}
			if inv.Changed("run") && id == "" {
				return cli.Usagef("--run names no run")
			}
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			check, err := checkModels(ctx, inv.Get(cfg))
			if werr := writeModels(inv.Stdout, check); werr != nil {
				return werr
			}
			return err
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&out, "out", "", "the `dir`ectory the run writes in, made when it doesn't exist")
	flags.StringVar(&id, "run", "", "the `id` of the docproc run to read; the latest run that ended when not given")
	cmd.Require("out")
	return cmd.Use(cfg)
}
