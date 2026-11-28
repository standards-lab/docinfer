package run

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"os"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/report"
)

// Config is what the run command runs under: the settings of each capability the configuration
// sets, in package profile's shapes, with the client the composition root built for each, each
// capability's endpoint and concurrency, which its manifest records, and the build's versions.
// The run resolves each capability's served model through profile.Resolve before it reads
// anything, and builds its pipeline through profile.New.
type Config struct {
	// Settings is each capability's and the pipeline's, at the configuration's value or its
	// starting value.
	Settings profile.Settings
	// Clients is each set capability's client and the tokenizer's.
	Clients profile.Clients
	// Endpoints is each set capability's endpoint.
	Endpoints map[docinfer.Capability]Endpoint
	// Concurrency is each set capability's most requests in flight.
	Concurrency map[docinfer.Capability]int
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
  1  the run stopped, or never began: an --extract file that can't be read or decoded, or
     given with no extraction configured, an unlisted model or an unreachable endpoint, a
     docproc directory with no run that ended, a --run that isn't one of its runs or hasn't
     ended, an --out that can't be written or is of another schema, an extract docinfer
     refuses, a failure of a record or the held results, or a cancellation; a run that began
     ends its manifest with the error and prints no last line. With --report, a run that
     stopped renders no pages; a run that ended and whose pages couldn't be rendered exits 1
     too, its manifest and records as the run left them, and docinfer report renders them
     again
  2  a usage error, such as no docproc directory or more than one, no --out, a --run that
     names no run, or an --extract that names no file
`

func command(cfg *graph.Node[Config]) *cli.Command {
	var (
		out, id, extractFile string
		render               bool
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
			if inv.Changed("extract") && extractFile == "" {
				return cli.Usagef("--extract names no file")
			}
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			c := inv.Get(cfg)
			t := target{docproc: inv.Args[0], out: out, run: id}
			if extractFile != "" {
				x, err := readExtract(extractFile, c)
				if err != nil {
					return err
				}
				t.extract = x
			}
			check, err := checkModels(ctx, c)
			if werr := writeModels(inv.Stdout, check); werr != nil {
				return werr
			}
			if err != nil {
				return err
			}
			run, err := infer(ctx, inv.Stdout, c, check.models, t)
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
	flags.StringVar(&extractFile, "extract", "", "the `file` of the extract to run over each source text, a JSON object of its name, prompt, and schema, as docinfer.Extract decodes it; needs extraction configured")
	flags.BoolVar(&render, "report", false, "once the last line is printed, render the run's report and print its index's path, as docinfer report does; never after a run that stopped or was cancelled")
	cmd.Require("out")
	return cmd.Use(cfg)
}

func readExtract(path string, cfg Config) (*docinfer.Extract, error) {
	if cfg.Settings.Extraction == nil {
		return nil, fmt.Errorf("--extract %s: no extraction configured: set extraction's endpoint and model", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--extract: %w", err)
	}
	var x docinfer.Extract
	if err := jsonv2.Unmarshal(data, &x, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("--extract %s: %w", path, err)
	}
	return &x, nil
}
