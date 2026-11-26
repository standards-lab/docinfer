package transcribe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"iter"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
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
)

const (
	wordsPerSecond = 8
	chunkSize      = 480
	batchBytes     = 512 << 10
	embedBatch     = 16
	dims           = 8
)

// Config is one run's settings: the docproc run directory and run, its latest ended run when RunID
// is empty; the audio endpoint, the model each request names, and the model it serves, which keys
// results; the transcription requests in flight; Transcription.MaxTokens, 0 sending none, and
// Transcription.Native, a JSON object, empty sending none; the draw, N inputs at most, skipping an
// input whose windows would take it past MaxWindows, 0 bounding none, shuffled by Seed; the run's
// Label; the JSON-lines file Out the Result is appended to and the directory Texts each window's
// texts go to, under Label, each empty writing none; Survey, which prints the draw to standard
// output and sends nothing; and Log, which takes the progress lines.
type Config struct {
	RunDir, RunID     string
	Endpoint          rig.Endpoint
	Model, Served     string
	Concurrency       int
	MaxTokens         int
	Native            string
	N, MaxWindows     int
	Seed              uint64
	Label, Out, Texts string
	Survey            bool
	Log               io.Writer
}

// WindowRecord is one window of a Result: its input, part, stage, and window index, its clip's
// SHA-256, its Outcome as docinfer held it, transcribed, incomplete, or refused, failed when a reply
// came and nothing was held, or unsent, the SHA-256 of docinfer's text, and the last reply's finish
// reason, tokens, content SHA-256 and length, attempts, latency, and HTTP error.
type WindowRecord struct {
	Input            string `json:"input"`
	Part             string `json:"part"`
	Stage            string `json:"stage"`
	Index            string `json:"index"`
	SHA256           string `json:"sha256"`
	Outcome          string `json:"outcome"`
	Reason           string `json:"reason,omitempty"`
	TextSHA256       string `json:"text_sha256,omitempty"`
	Finish           string `json:"finish_reason,omitempty"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	ReplySHA256      string `json:"reply_sha256,omitempty"`
	ReplyRunes       int    `json:"reply_runes"`
	Attempts         int    `json:"attempts"`
	LatencyMS        int64  `json:"latency_ms"`
	HTTPError        string `json:"http_error,omitempty"`
}

// Result is one run's JSON line: its settings, the draw's inputs and windows, its wall time,
// docinfer's transcription counts and tokens, the completion tokens summed and greatest over the
// replies captured, the HTTP requests sent, each transcript, a run error, and each window's record.
type Result struct {
	Label            string                        `json:"label"`
	MaxTokens        int                           `json:"max_tokens"`
	Native           string                        `json:"native,omitempty"`
	Seed             uint64                        `json:"seed"`
	N                int                           `json:"n"`
	MaxWindowsFlag   int                           `json:"max_windows"`
	Concurrency      int                           `json:"concurrency"`
	Served           string                        `json:"served"`
	BaseURL          string                        `json:"base_url"`
	Inputs           int                           `json:"inputs"`
	Windows          int                           `json:"windows"`
	DistinctWindows  int                           `json:"distinct_windows"`
	Start            time.Time                     `json:"start"`
	WallSeconds      float64                       `json:"wall_seconds"`
	Counts           *docinfer.TranscriptionCounts `json:"counts,omitempty"`
	Tokens           *docinfer.Usage               `json:"tokens,omitempty"`
	CompletionTokens int                           `json:"completion_tokens"`
	MaxCompletion    int                           `json:"max_completion_tokens"`
	HTTPRequests     int                           `json:"http_requests"`
	Transcripts      []Transcript                  `json:"transcripts"`
	RunError         string                        `json:"run_error,omitempty"`
	WindowRecords    []WindowRecord                `json:"window_records"`
}

// Transcript is one transcript docinfer yielded: its input, part, windows, the indexes of those
// that failed, its outcome, and its time.
type Transcript struct {
	Input   string `json:"input"`
	Part    string `json:"part"`
	Windows int    `json:"windows"`
	Failed  []int  `json:"failed,omitempty"`
	Outcome string `json:"outcome"`
	TookMS  int64  `json:"took_ms"`
}

type audioInput struct {
	origin  string
	events  []dispatch.Event
	windows int
}

func draw(record iter.Seq2[schema.Event, error], n, maxWindows int, seed uint64) ([]audioInput, []audioInput, error) {
	by := map[string]*audioInput{}
	for e, err := range record {
		if err != nil {
			return nil, nil, err
		}
		if e.Kind != schema.OutputEvent || e.Output == nil {
			continue
		}
		window := e.Output.Kind == dispatch.AudioOutput
		plan := e.Output.Kind == dispatch.RecordOutput && e.Output.MediaType == dispatch.MediaAudioPlan
		if !window && !plan {
			continue
		}
		a := by[e.Input]
		if a == nil {
			a = &audioInput{origin: e.Input}
			by[e.Input] = a
		}
		a.events = append(a.events, e.Event)
		if window {
			a.windows++
		}
	}
	all := make([]audioInput, 0, len(by))
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range by {
			if !yield(k) {
				return
			}
		}
	}) {
		all = append(all, *by[k])
	}
	shuffled := slices.Clone(all)
	rng := rand.New(rand.NewPCG(seed, seed))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	var out []audioInput
	windows := 0
	for _, a := range shuffled {
		if len(out) == n {
			break
		}
		if maxWindows > 0 && windows+a.windows > maxWindows {
			continue
		}
		out = append(out, a)
		windows += a.windows
	}
	return all, out, nil
}

// Run draws c's audio inputs from the docproc run, runs docinfer's chat-mode transcription over
// them against c's endpoint, with embeddings sent to an in-process fake, prints the Result less its
// transcripts and window records, appends it whole to c.Out, and writes each window's texts under
// c.Texts. A run error is returned after the Result is written.
func Run(ctx context.Context, c Config) error {
	if c.RunDir == "" {
		return errors.New("no docproc run directory: set -rundir")
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
	all, sample, err := draw(src.Record(c.RunID), c.N, c.MaxWindows, c.Seed)
	if err != nil {
		return err
	}
	total := 0
	for _, a := range all {
		total += a.windows
	}
	windows := 0
	distinct := map[string]bool{}
	for _, a := range sample {
		windows += a.windows
		for _, ev := range a.events {
			if ev.Output.Kind == dispatch.AudioOutput {
				distinct[ev.Output.SHA256] = true
			}
		}
	}
	_, _ = fmt.Fprintf(c.Log, "transcribe-eval: %d audio inputs, %d windows; drew %d inputs, %d windows (%d distinct), seed %d\n",
		len(all), total, len(sample), windows, len(distinct), c.Seed)
	if c.Survey {
		for _, a := range sample {
			fmt.Printf("%d\t%d\t%s\n", a.windows, len(a.events)-a.windows, a.origin)
		}
		return nil
	}

	if err := c.Endpoint.FromEnv("AUDIO"); err != nil {
		return err
	}
	emb, stop, err := rig.Embeddings(dims)
	if err != nil {
		return err
	}
	defer stop()

	captured := &capture{by: map[string]*reply{}}
	tc, err := c.Endpoint.Client(func(next http.RoundTripper) http.RoundTripper { return &recording{next: next, cap: captured} },
		ai.Options{TranscriptionMode: ai.TranscribeChat})
	if err != nil {
		return err
	}
	bounds, err := docinfer.NewBounds(1, c.Concurrency, 1, 1)
	if err != nil {
		return err
	}
	cache := docinfer.NewMemoryCache()
	events := 0
	for _, a := range sample {
		events += len(a.events)
	}
	p, err := docinfer.New(docinfer.Config{
		Transcription: &docinfer.Transcription{Client: tc, Model: c.Model, Served: c.Served, MaxTokens: c.MaxTokens, Native: nativeOf(c.Native)},
		Embeddings:    docinfer.Embeddings{Client: emb, Model: "fake", Served: "fake"},
		Chunker: chunk.Splitter{Bound: chunkSize, Runes: 8192, Model: "fake",
			Count: func(_ context.Context, s string) (int, error) { return len(strings.Fields(s)), nil }},
		Dimensions: dims,
		Cache:      cache,
		Bounds:     bounds,
		Pending:    max(events, c.Concurrency),
		EmbedBatch: embedBatch,
		BatchBytes: batchBytes,
		Join:       join.Request{WordsPerSecond: wordsPerSecond},
	})
	if err != nil {
		return err
	}

	r := Result{Label: c.Label, MaxTokens: c.MaxTokens, Native: c.Native, Seed: c.Seed, N: c.N, MaxWindowsFlag: c.MaxWindows, Concurrency: c.Concurrency,
		Served: c.Served, BaseURL: c.Endpoint.BaseURL, Inputs: len(sample), Windows: windows, DistinctWindows: len(distinct),
		Start: time.Now().UTC(), Transcripts: []Transcript{}}
	inputs := func(yield func(docinfer.Input, error) bool) {
		for _, a := range sample {
			for _, ev := range a.events {
				if !yield(docinfer.Input{Event: ev, Origin: a.origin}, nil) {
					return
				}
			}
		}
	}
	start := time.Now()
	for res, err := range p.Run(ctx, src.Content(), inputs, docinfer.Options{Final: true}) {
		if err != nil {
			r.RunError = err.Error()
			break
		}
		if res.Done != nil {
			r.Counts, r.Tokens = res.Done.Transcription, res.Done.Tokens.Transcription
		}
		if inf := res.Inference; inf != nil && inf.Capability == docinfer.CapabilityTranscription {
			r.Transcripts = append(r.Transcripts, Transcript{Input: inf.Origin, Part: inf.Part, Windows: len(inf.ContentSHA256),
				Failed: inf.Failed, Outcome: string(inf.Outcome), TookMS: inf.TookMS})
			_, _ = fmt.Fprintf(c.Log, "transcribe-eval: %s: %s %s: %d windows, %s, %.0f s in\n",
				c.Label, inf.Origin, inf.Part, len(inf.ContentSHA256), inf.Outcome, time.Since(start).Seconds())
		}
	}
	r.WallSeconds = time.Since(start).Seconds()

	var dir string
	if c.Texts != "" {
		dir = filepath.Join(c.Texts, c.Label)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	id := *p.Profile().Transcription
	r.HTTPRequests = captured.requests()
	written := map[string]bool{}
	for _, a := range sample {
		for _, ev := range a.events {
			if ev.Output.Kind != dispatch.AudioOutput {
				continue
			}
			sha := ev.Output.SHA256
			w := WindowRecord{Input: a.origin, Part: ev.Part.ID, Stage: ev.Stage.Name, Index: ev.Output.Meta[dispatch.MetaWindow],
				SHA256: sha, Outcome: "unsent"}
			var text string
			vals, err := cache.Get(ctx, docinfer.Key(id, sha))
			if err != nil {
				return err
			}
			if b := vals[0]; b != nil {
				var h docinfer.Held
				if err := json.Unmarshal(b, &h); err != nil {
					return fmt.Errorf("held %s: %w", sha, err)
				}
				switch {
				case h.Refused != "":
					w.Outcome, w.Reason = "refused", h.Refused
				case h.Incomplete != "":
					w.Outcome, w.Reason = "incomplete", h.Incomplete
				default:
					w.Outcome, text = "transcribed", h.Text
					w.TextSHA256 = sha256Hex(h.Text)
				}
			}
			rep := captured.get(sha)
			if rep != nil {
				if w.Outcome == "unsent" {
					w.Outcome = "failed"
				}
				w.Finish, w.PromptTokens, w.CompletionTokens = rep.finish, rep.prompt, rep.completion
				w.Attempts, w.LatencyMS, w.HTTPError = rep.attempts, rep.latency.Milliseconds(), rep.err
				if rep.ok {
					w.ReplySHA256, w.ReplyRunes = sha256Hex(rep.content), len([]rune(rep.content))
				}
			}
			r.WindowRecords = append(r.WindowRecords, w)
			if dir != "" && !written[sha] {
				written[sha] = true
				if err := os.WriteFile(filepath.Join(dir, sha+".txt"), []byte(text), 0o644); err != nil {
					return err
				}
				if rep != nil && rep.ok {
					if err := os.WriteFile(filepath.Join(dir, sha+".reply.txt"), []byte(rep.content), 0o644); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, rep := range captured.all() {
		r.CompletionTokens += rep.completion
		r.MaxCompletion = max(r.MaxCompletion, rep.completion)
	}

	summary := r
	summary.WindowRecords, summary.Transcripts = nil, nil
	if s, err := json.Marshal(summary); err == nil {
		fmt.Println(string(s))
	}
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

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type reply struct {
	attempts   int
	ok         bool
	content    string
	finish     string
	prompt     int
	completion int
	latency    time.Duration
	err        string
}

type capture struct {
	mu   sync.Mutex
	by   map[string]*reply
	reqs int
}

func (c *capture) put(sha string, f func(*reply)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.by[sha]
	if r == nil {
		r = &reply{}
		c.by[sha] = r
	}
	f(r)
}

func (c *capture) get(sha string) *reply {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.by[sha]; r != nil {
		cp := *r
		return &cp
	}
	return nil
}

func (c *capture) all() []reply {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]reply, 0, len(c.by))
	for _, r := range c.by {
		out = append(out, *r)
	}
	return out
}

func (c *capture) requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reqs
}

type recording struct {
	next http.RoundTripper
	cap  *capture
}

func (t *recording) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
		req.ContentLength = int64(len(b))
	}
	sha := clipSHA(body)
	t.cap.mu.Lock()
	t.cap.reqs++
	t.cap.mu.Unlock()
	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		if sha != "" {
			t.cap.put(sha, func(r *reply) {
				r.attempts++
				r.ok, r.latency, r.err = false, time.Since(start), "transport: "+err.Error()
			})
		}
		return nil, err
	}
	b, rerr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(b))
	if sha == "" {
		return resp, rerr
	}
	t.cap.put(sha, func(r *reply) {
		r.attempts++
		r.latency = time.Since(start)
		*r = reply{attempts: r.attempts, latency: r.latency}
		switch {
		case rerr != nil:
			r.err = "read: " + rerr.Error()
		case resp.StatusCode/100 != 2:
			r.err = "http " + resp.Status
		default:
			var wire struct {
				Choices []struct {
					Message struct {
						Content *string `json:"content"`
					} `json:"message"`
					Finish string `json:"finish_reason"`
				} `json:"choices"`
				Usage struct {
					Prompt     int `json:"prompt_tokens"`
					Completion int `json:"completion_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(b, &wire); err != nil {
				r.err = "decode: " + err.Error()
				return
			}
			r.ok = true
			r.prompt, r.completion = wire.Usage.Prompt, wire.Usage.Completion
			if len(wire.Choices) > 0 {
				r.finish = wire.Choices[0].Finish
				if s := wire.Choices[0].Message.Content; s != nil {
					r.content = *s
				}
			}
		}
	})
	return resp, rerr
}

func clipSHA(body []byte) string {
	var wire struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return ""
	}
	for _, m := range wire.Messages {
		var parts []struct {
			Type  string `json:"type"`
			Audio *struct {
				Data string `json:"data"`
			} `json:"input_audio"`
		}
		if json.Unmarshal(m.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			if p.Audio == nil {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(p.Audio.Data)
			if err != nil {
				return ""
			}
			h := sha256.Sum256(data)
			return hex.EncodeToString(h[:])
		}
	}
	return ""
}

func nativeOf(s string) jsontext.Value {
	if s == "" {
		return nil
	}
	return jsontext.Value(s)
}
