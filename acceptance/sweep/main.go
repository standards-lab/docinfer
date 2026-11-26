// Command sweep measures docinfer's vision row at one concurrency over a seeded draw of a docproc
// run's distinct images, with cmd/docinfer's vision prompt, reasoning effort, and chunking, and
// embeddings sent to an in-process fake, so only the vision row is loaded.
//
// Usage:
//
//	sweep -rundir DIR [-run ID] [-base-url URL] [flags]
//
// The endpoint is -base-url, or AI_VISION_BASE_URL, such as http://gb10-2:3000/vision/v1, and
// each request carries AI_VISION_TOKEN as its bearer token when it is set. The draw is the run's
// distinct image outputs sorted by SHA-256 and shuffled by a PCG seeded with -seed, its first -n
// kept, then, with -only, those whose SHA-256 the file lists. The result, its counts, throughput,
// latency quantiles, and the SHA-256s it held incomplete, is printed and appended as one JSON line
// to -out. With -texts, each image's reply goes to <texts>/<sha>.<max-tokens>.txt and a line
// describing it, its outcome, size, and time, to <texts>/index.jsonl.
package main

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"iter"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/acceptance/internal/rig"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	visionPrompt    = "Transcribe the text in this picture. If it has no text, describe it in one sentence."
	visionReasoning = protocol.ReasoningEffortNone
	chunkSize       = 480
	batchBytes      = 512 << 10
	embedBatch      = 16
	dims            = 8
)

type config struct {
	RunDir, RunID string
	Endpoint      rig.Endpoint
	Model, Served string
	Concurrency   int
	N             int
	Seed          uint64
	MaxTokens     int
	MaxPixels     int
	Native        string
	Only          string
	Label         string
	Out, Texts    string
}

type textEntry struct {
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	Output    int    `json:"output_tokens"`
	TookMS    int64  `json:"took_ms"`
	MaxTokens int    `json:"max_tokens"`
}

type result struct {
	Concurrency    int             `json:"concurrency"`
	Seed           uint64          `json:"seed"`
	Images         int             `json:"images"`
	Done           int             `json:"done"`
	WallSeconds    float64         `json:"wall_seconds"`
	PerMinute      float64         `json:"images_per_minute"`
	LatencyP50     float64         `json:"latency_p50_s"`
	LatencyP95     float64         `json:"latency_p95_s"`
	LatencyMax     float64         `json:"latency_max_s"`
	Requests       int             `json:"http_requests"`
	HTTPErrors     int             `json:"http_errors"`
	Described      int             `json:"described"`
	Incomplete     int             `json:"incomplete"`
	Refused        int             `json:"refused"`
	Failed         int             `json:"failed"`
	RunError       string          `json:"run_error,omitempty"`
	Failures       []string        `json:"failures,omitempty"`
	OutputToks     int             `json:"output_tokens"`
	InputToks      int             `json:"input_tokens"`
	Start          time.Time       `json:"start"`
	Native         json.RawMessage `json:"native,omitempty"`
	MaxPixels      int             `json:"max_pixels,omitempty"`
	Label          string          `json:"label,omitempty"`
	IncompleteSHAs []string        `json:"incomplete_shas,omitempty"`
}

func main() {
	var c config
	flag.StringVar(&c.RunDir, "rundir", "", "the docproc run directory, required")
	flag.StringVar(&c.RunID, "run", "", "the docproc run's id; the latest run that ended when not given")
	flag.StringVar(&c.Endpoint.BaseURL, "base-url", "", "the vision row's API root; AI_VISION_BASE_URL when not given")
	flag.DurationVar(&c.Endpoint.HeaderTimeout, "header-timeout", 600*time.Second, "the transport's ResponseHeaderTimeout")
	flag.StringVar(&c.Model, "model", "vision", "the model each request names")
	flag.StringVar(&c.Served, "served", "nvidia/Gemma-4-26B-A4B-NVFP4", "the model the row serves, which keys results")
	flag.IntVar(&c.Concurrency, "concurrency", 4, "vision requests in flight, the Bounds vision value")
	flag.IntVar(&c.N, "n", 320, "distinct images drawn")
	flag.Uint64Var(&c.Seed, "seed", 1, "the draw's seed")
	flag.IntVar(&c.MaxTokens, "max-tokens", 2048, "Vision.MaxTokens")
	flag.IntVar(&c.MaxPixels, "max-pixels", 0, "Vision.MaxPixels; 0 sends every image as it is")
	flag.StringVar(&c.Native, "native", "", "Vision.Native, a JSON object merged into each request; empty sends none")
	flag.StringVar(&c.Only, "only", "", "a file of SHA-256s, one a line: send only the drawn images it lists")
	flag.StringVar(&c.Label, "label", "", "a label the result carries")
	flag.StringVar(&c.Out, "out", "", "a JSON-lines file the result is appended to")
	flag.StringVar(&c.Texts, "texts", "", "a directory each image's reply and an index are written to")
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: sweep -rundir DIR [-run ID] [-base-url URL] [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if err := run(context.Background(), c); err != nil {
		fmt.Fprintln(os.Stderr, "sweep:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, c config) error {
	if c.RunDir == "" {
		return errors.New("no docproc run directory: set -rundir")
	}
	if err := c.Endpoint.FromEnv("VISION"); err != nil {
		return err
	}
	src, err := rundir.Open(c.RunDir)
	if err != nil {
		return err
	}
	if c.RunID == "" {
		if c.RunID, err = src.Latest(); err != nil {
			return err
		}
	}
	images, distinct, err := draw(src.Record(c.RunID), c.N, c.Seed)
	if err != nil {
		return err
	}
	if c.Only != "" {
		b, err := os.ReadFile(c.Only)
		if err != nil {
			return err
		}
		want := map[string]bool{}
		for l := range strings.FieldsSeq(string(b)) {
			want[l] = true
		}
		images = slices.DeleteFunc(images, func(e dispatch.Event) bool { return !want[e.Output.SHA256] })
	}
	fmt.Fprintf(os.Stderr, "sweep: %d distinct images, drew %d with seed %d, concurrency %d\n", distinct, len(images), c.Seed, c.Concurrency)

	emb, stop, err := rig.Embeddings(dims)
	if err != nil {
		return err
	}
	defer stop()

	rec := &recorder{}
	vc, err := c.Endpoint.Client(func(next http.RoundTripper) http.RoundTripper { return &timed{next: next, rec: rec} }, ai.Options{})
	if err != nil {
		return err
	}
	var native jsontext.Value
	if c.Native != "" {
		native = jsontext.Value(c.Native)
	}
	bounds, err := docinfer.NewBounds(c.Concurrency, 1, 1, 1)
	if err != nil {
		return err
	}
	p, err := docinfer.New(docinfer.Config{
		Vision: &docinfer.Vision{Client: vc, Model: c.Model, Served: c.Served, Prompt: visionPrompt,
			MaxTokens: c.MaxTokens, Reasoning: visionReasoning, Native: native, MaxPixels: c.MaxPixels},
		Embeddings: docinfer.Embeddings{Client: emb, Model: "fake", Served: "fake"},
		Chunker: chunk.Splitter{Bound: chunkSize, Runes: 8192, Model: "fake",
			Count: func(_ context.Context, s string) (int, error) { return len(strings.Fields(s)), nil }},
		Dimensions: dims,
		Cache:      docinfer.NewMemoryCache(),
		Bounds:     bounds,
		Pending:    max(len(images), c.Concurrency),
		EmbedBatch: embedBatch,
		BatchBytes: batchBytes,
		Join:       join.Request{WordsPerSecond: 3},
	})
	if err != nil {
		return err
	}

	r := result{Concurrency: c.Concurrency, Seed: c.Seed, Images: len(images), Start: time.Now().UTC(), Label: c.Label,
		Native: json.RawMessage(native), MaxPixels: c.MaxPixels}
	inputs := func(yield func(docinfer.Input, error) bool) {
		for _, ev := range images {
			if !yield(docinfer.Input{Event: ev}, nil) {
				return
			}
		}
	}
	start := time.Now()
	for res, err := range p.Run(ctx, src.Content(), inputs, docinfer.Options{Final: true}) {
		if err != nil {
			r.RunError = err.Error()
			break
		}
		inf := res.Inference
		if inf == nil || inf.Capability != docinfer.CapabilityVision {
			continue
		}
		r.Done++
		if c.Texts != "" {
			if err := writeText(ctx, src, inf, c); err != nil {
				return err
			}
		}
		r.InputToks += inf.Usage.Input
		r.OutputToks += inf.Usage.Output
		switch inf.Outcome {
		case docinfer.OutcomeDescribed:
			r.Described++
		case docinfer.OutcomeIncomplete:
			r.Incomplete++
			r.IncompleteSHAs = append(r.IncompleteSHAs, inf.ContentSHA256[0])
		case docinfer.OutcomeRefused:
			r.Refused++
			r.note(fmt.Sprintf("refused %s: %s", inf.ContentSHA256[0], inf.Reason))
		case docinfer.OutcomeFailed:
			r.Failed++
			r.note(fmt.Sprintf("failed %s: %s", inf.ContentSHA256[0], inf.Reason))
		}
	}
	wall := time.Since(start)
	r.WallSeconds = wall.Seconds()
	r.PerMinute = float64(r.Done) / wall.Minutes()
	lat, errs := rec.read()
	r.Requests, r.HTTPErrors = len(lat), len(errs)
	for _, e := range errs {
		r.note(e)
	}
	if len(lat) > 0 {
		slices.Sort(lat)
		r.LatencyP50, r.LatencyP95, r.LatencyMax = quantile(lat, 0.5), quantile(lat, 0.95), lat[len(lat)-1]
	}
	if r.RunError != "" {
		r.Failed += r.Images - r.Done
	}

	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	fmt.Println(string(line))
	if c.Out != "" {
		if err := rig.Append(c.Out, r); err != nil {
			return err
		}
	}
	if r.RunError != "" {
		return errors.New(r.RunError)
	}
	return nil
}

func writeText(ctx context.Context, src *rundir.Dir, inf *docinfer.Inference, c config) error {
	sha := inf.ContentSHA256[0]
	e := textEntry{SHA256: sha, Outcome: string(inf.Outcome), Reason: inf.Reason, Output: inf.Usage.Output,
		TookMS: inf.TookMS, MaxTokens: c.MaxTokens}
	if rc, err := src.Content().Open(ctx, sha); err == nil {
		if cfg, format, err := image.DecodeConfig(rc); err == nil {
			e.Width, e.Height, e.MediaType = cfg.Width, cfg.Height, format
		}
		_ = rc.Close()
	}
	name := fmt.Sprintf("%s/%s.%d.txt", c.Texts, sha, c.MaxTokens)
	if err := os.WriteFile(name, []byte(inf.Text), 0o644); err != nil {
		return err
	}
	return rig.Append(c.Texts+"/index.jsonl", e)
}

func (r *result) note(s string) {
	if len(r.Failures) < 5 {
		r.Failures = append(r.Failures, s)
	}
}

func draw(record iter.Seq2[schema.Event, error], n int, seed uint64) ([]dispatch.Event, int, error) {
	seen := map[string]dispatch.Event{}
	for e, err := range record {
		if err != nil {
			return nil, 0, err
		}
		if e.Kind != schema.OutputEvent || e.Output == nil || e.Output.Kind != dispatch.ImageOutput {
			continue
		}
		if _, ok := seen[e.Output.SHA256]; !ok {
			seen[e.Output.SHA256] = e.Event
		}
	}
	shas := slices.Sorted(func(yield func(string) bool) {
		for s := range seen {
			if !yield(s) {
				return
			}
		}
	})
	rng := rand.New(rand.NewPCG(seed, seed))
	rng.Shuffle(len(shas), func(i, j int) { shas[i], shas[j] = shas[j], shas[i] })
	n = min(n, len(shas))
	out := make([]dispatch.Event, n)
	for i, s := range shas[:n] {
		out[i] = seen[s]
	}
	return out, len(shas), nil
}

func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

type recorder struct {
	mu   sync.Mutex
	lat  []float64
	errs []string
}

func (r *recorder) add(d time.Duration, failure string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lat = append(r.lat, d.Seconds())
	if failure != "" {
		r.errs = append(r.errs, failure)
	}
}

func (r *recorder) read() ([]float64, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lat), slices.Clone(r.errs)
}

type timed struct {
	next http.RoundTripper
	rec  *recorder
}

func (t *timed) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		t.rec.add(time.Since(start), "transport: "+err.Error())
		return nil, err
	}
	failure := ""
	if resp.StatusCode/100 != 2 {
		failure = "http " + resp.Status
	}
	resp.Body = &timedBody{ReadCloser: resp.Body, done: func() { t.rec.add(time.Since(start), failure) }}
	return resp, nil
}

type timedBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *timedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.done)
	}
	return n, err
}

func (b *timedBody) Close() error {
	b.once.Do(b.done)
	return b.ReadCloser.Close()
}
