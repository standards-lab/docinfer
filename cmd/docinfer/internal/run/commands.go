package run

import (
	"context"
	"fmt"
	"io"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/report"
)

// Config is what the run command runs under, in the library's shapes: each capability the
// configuration sets, with the client the composition root built for it, and the pipeline's
// settings, each at the configuration's value or its starting value. Each capability's Served is
// the configuration's served_model, empty when it sets none, and the Chunker's Model is empty:
// the run resolves each against the model listing before it reads anything.
type Config struct {
	// Vision describes images; nil skips them.
	Vision *docinfer.Vision
	// Transcription transcribes audio windows; nil skips them.
	Transcription *docinfer.Transcription
	// Embeddings embeds chunks.
	Embeddings docinfer.Embeddings
	// Endpoints is each set capability's endpoint.
	Endpoints map[docinfer.Capability]Endpoint
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
	// Build is the command's and the library's versions, which each run's manifest records.
	Build outdir.Build
}

// Endpoint is a capability's endpoint, as the manifest records it: its name, its key in the
// configuration's endpoints, its base URL, and its format.
type Endpoint struct {
	Name    string
	BaseURL string
	Format  string
}

// Commands returns the run command, over the configuration cfg's node holds, which it declares.
func Commands(cfg *graph.Node[Config]) []*cli.Command {
	return []*cli.Command{command(cfg)}
}

const exitStatus = `Exit status:
  0  the run ended: every line of docproc's run was fed through, though some of its images,
     windows, or chunks were refused or failed; each is recorded, and the last line counts
     them as failures; with --report, its pages were rendered too
  1  the run stopped, or never began: an unlisted model or an unreachable endpoint, a docproc
     directory with no run that ended, a --run that isn't one of its runs or hasn't ended, an
     --out that can't be written or is of another schema, a failure of a record or the held
     results, or a cancellation; a run that began ends its manifest with the error and prints
     no last line. With --report, a run that stopped renders no pages; a run that ended and
     whose pages couldn't be rendered exits 1 too, its manifest and records as the run left
     them, and docinfer report renders them again
  2  a usage error, such as no docproc directory or more than one, no --out, or a --run that
     names no run
`

func command(cfg *graph.Node[Config]) *cli.Command {
	var (
		out, id string
		render  bool
	)
	cmd := &cli.Command{
		Name:     "run",
		Summary:  "Infer over a docproc run directory's latest run that ended, or --run's",
		Synopsis: "<docproc-dir>",
		Footer:   func(w io.Writer) { _, _ = io.WriteString(w, exitStatus) },
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
			c := inv.Get(cfg)
			check, err := checkModels(ctx, c)
			if werr := writeModels(inv.Stdout, check); werr != nil {
				return werr
			}
			if err != nil {
				return err
			}
			run, err := infer(ctx, inv.Stdout, c.withServed(check.served), target{docproc: inv.Args[0], out: out, run: id})
			if err != nil || !render {
				return err
			}
			if err := report.Make(ctx, inv.Stdout, out, run); err != nil {
				return fmt.Errorf("the run's report: %w; docinfer report %s --run %s renders its pages again", err, out, run)
			}
			return nil
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&out, "out", "", "the `dir`ectory the run writes in, made when it doesn't exist")
	flags.StringVar(&id, "run", "", "the `id` of the docproc run to read; the latest run that ended when not given")
	flags.BoolVar(&render, "report", false, "once the last line is printed, render the run's report and print its index's path, as docinfer report does; never after a run that stopped or was cancelled")
	cmd.Require("out")
	return cmd.Use(cfg)
}
