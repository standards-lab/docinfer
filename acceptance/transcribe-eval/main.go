// Command transcribe-eval runs docinfer's chat-mode transcription, at one Transcription.MaxTokens,
// over a seeded draw of a docproc run's audio inputs, and records per window what the model
// replied, as package internal/transcribe tells. Embeddings go to an in-process fake, so only the
// audio row is loaded.
//
// Usage:
//
//	transcribe-eval -rundir DIR [-run ID] [-base-url URL] [flags]
//
// The endpoint is -base-url, or AI_AUDIO_BASE_URL, such as http://gb10-2:3000/audio/v1, and each
// request carries AI_AUDIO_TOKEN as its bearer token when it is set. The draw keeps its first -n
// inputs, skipping any whose windows would take it past -max-windows; -survey prints the draw and
// sends nothing. A run appends one JSON line to -out: its counts and wall time, and per window its
// input, part, index, outcome as docinfer held it, completion tokens, finish reason, and text
// SHA-256s; with -texts, each window's texts go under <texts>/<label>/.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/standards-lab/docinfer/acceptance/internal/transcribe"
)

func main() {
	var c transcribe.Config
	flag.StringVar(&c.RunDir, "rundir", "", "the docproc run directory, required")
	flag.StringVar(&c.RunID, "run", "", "the docproc run's id; the latest run that ended when not given")
	flag.StringVar(&c.Endpoint.BaseURL, "base-url", "", "the audio row's API root; AI_AUDIO_BASE_URL when not given")
	flag.StringVar(&c.Model, "model", "audio", "the model each request names")
	flag.StringVar(&c.Served, "served", "google/gemma-4-E4B-it", "the model the row serves, which keys results")
	flag.IntVar(&c.Concurrency, "concurrency", 32, "transcription requests in flight, the Bounds transcription value")
	flag.IntVar(&c.MaxTokens, "max-tokens", 512, "Transcription.MaxTokens; 0 sends none")
	flag.StringVar(&c.Native, "native", "", "Transcription.Native, a JSON object (empty: none)")
	flag.IntVar(&c.N, "n", 50, "audio inputs drawn, at most")
	flag.IntVar(&c.MaxWindows, "max-windows", 700, "skip a drawn input whose windows would take the sample past this (0: no bound)")
	flag.Uint64Var(&c.Seed, "seed", 1, "the draw's seed")
	flag.StringVar(&c.Label, "label", "", "the run's label (default max<max-tokens>)")
	flag.StringVar(&c.Out, "out", "", "a JSON-lines file the result is appended to")
	flag.StringVar(&c.Texts, "texts", "", "a directory each window's texts go to, under <label>/")
	flag.DurationVar(&c.Endpoint.HeaderTimeout, "header-timeout", 300*time.Second, "the transport's ResponseHeaderTimeout")
	flag.BoolVar(&c.Survey, "survey", false, "print the drawn audio inputs and the draw's size, send nothing")
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: transcribe-eval -rundir DIR [-run ID] [-base-url URL] [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if c.Label == "" {
		c.Label = fmt.Sprintf("max%d", c.MaxTokens)
	}
	c.Log = os.Stderr
	if err := transcribe.Run(context.Background(), c); err != nil {
		fmt.Fprintln(os.Stderr, "transcribe-eval:", err)
		os.Exit(1)
	}
}
