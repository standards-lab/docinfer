package docinfer_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/dispatch/dispatchtest"
	"github.com/standards-lab/docproc/route"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format"
)

// The embedding rows of the failure table in doc.go, one test per row against aitest. Each run is
// one text of two chunks, sent as one call.

const (
	refusedChunk = "This chunk is refused."
	twoChunks    = refusedChunk + "\n\nThis chunk is embedded."
)

// runTwoChunks runs a pipeline over e on twoChunks, with opts, returning what it yielded and the
// pipeline's cache.
func runTwoChunks(t *testing.T, e *aitest.Endpoint, opts docinfer.Options) (outcome, docinfer.Cache) {
	t.Helper()
	cfg := runConfig(t, e, nil)
	if pieces, _ := cfg.Chunker.Split(twoChunks); len(pieces) != 2 || pieces[0].Text != refusedChunk {
		t.Fatalf("the fixture cuts into %d chunks, want 2, the first %q", len(pieces), refusedChunk)
	}
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, twoChunks)), opts))
	return o, cfg.Cache
}

// A refused status, 400, 413, 415, or 422, re-sends the call a text at a time: the text the
// endpoint refuses alone is its chunk's Error, held as a refusal, and the other is embedded.
func TestRunEmbeddingRefused(t *testing.T) {
	for _, status := range []int{400, 413, 415, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
				if len(req.Input) > 1 || req.Input[0] == refusedChunk {
					return aitest.ErrorReply{Status: status, Message: "content refused"}
				}
				return embedAll(ctx, req)
			}})
			o, cache := runTwoChunks(t, e, docinfer.Options{})
			if o.err != nil {
				t.Fatalf("Run: %v", o.err)
			}
			if n := len(e.Requests()); n != 3 {
				t.Errorf("%d requests, want the call and then each text alone, 3", n)
			}
			wantError := (&ai.APIError{Status: status, Message: "content refused"}).Error()
			for _, c := range o.chunks() {
				h := held(t, cache, c.chunk.Digest)
				switch c.chunk.Text {
				case refusedChunk:
					if c.chunk.Error != wantError || c.chunk.Vector != "" {
						t.Errorf("the refused chunk: error %q, vector %q; want error %q", c.chunk.Error, c.chunk.Vector, wantError)
					}
					if h == nil || h.Refused != wantError || h.Vector != "" {
						t.Errorf("the refused chunk is held as %+v, want its refusal", h)
					}
				default:
					if c.chunk.Vector == "" || h == nil || h.Vector != c.chunk.Vector {
						t.Errorf("the other chunk: vector %q, held %+v", c.chunk.Vector, h)
					}
				}
			}
			checkCounts(t, o, docinfer.Counts{
				Texts: 1, Embedded: 1, Rejected: 1, Requests: 3,
				Tokens: docinfer.Tokens{Embeddings: docinfer.Usage{Input: 3}},
			})
		})
	}
}

// A held refusal is read back as its chunk's Error, counted rejected and held, and nothing is
// asked.
func TestRunHeldRefusal(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		if slices.Contains(req.Input, refusedChunk) {
			return aitest.ErrorReply{Status: 422, Message: "content refused"}
		}
		return embedAll(ctx, req)
	}})
	cfg := runConfig(t, e, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	in := outputInput(t, content, childPart, dispatch.TextOutput, twoChunks)
	if o := collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{})); o.err != nil {
		t.Fatal(o.err)
	}
	requests := len(e.Requests())
	o := collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if n := len(e.Requests()); n != requests {
		t.Errorf("the second run sent %d requests, want none", n-requests)
	}
	for _, c := range o.chunks() {
		if c.chunk.Text == refusedChunk && !strings.Contains(c.chunk.Error, "content refused") {
			t.Errorf("the held refusal read back as error %q", c.chunk.Error)
		}
	}
	checkCounts(t, o, docinfer.Counts{Texts: 1, Embedded: 1, Rejected: 1, Held: 2})
}

// A permanent status, 401, 403, 404, 501, 505, or any other 4xx but 408 and 429, stops the job,
// final or not: the call isn't split, nothing is held, and Permanent reports the error.
func TestRunEmbeddingPermanent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 405, 409, 418, 451, 501, 505} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{Embeddings: func(context.Context, aitest.EmbeddingRequest) aitest.Reply {
					return aitest.ErrorReply{Status: status, Message: "no"}
				}})
				o, cache := runTwoChunks(t, e, docinfer.Options{Final: final})
				apiErr, ok := errors.AsType[*ai.APIError](o.err)
				if !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if !docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is false", o.err)
				}
				stopped(t, o, e, cache)
			})
		}
	}
}

// A transient status, 408, 429, or any 5xx but 500, 501, and 505, stops the job, final or not,
// its *ai.APIError's RetryAfter carried for the redelivery.
func TestRunEmbeddingTransient(t *testing.T) {
	for _, status := range []int{408, 429, 502, 503, 504, 507, 599} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				e := aitest.Serve(t, aitest.Routes{Embeddings: func(context.Context, aitest.EmbeddingRequest) aitest.Reply {
					return aitest.ErrorReply{Status: status, Message: "later", RetryAfter: "7"}
				}})
				o, cache := runTwoChunks(t, e, docinfer.Options{Final: final})
				apiErr, ok := errors.AsType[*ai.APIError](o.err)
				if !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if apiErr.RetryAfter != 7*time.Second {
					t.Errorf("RetryAfter %v, want 7s", apiErr.RetryAfter)
				}
				if docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is true", o.err)
				}
				stopped(t, o, e, cache)
			})
		}
	}
}

// A transport failure, a *url.Error with no reply, stops the job, final or not, and isn't
// permanent.
func TestRunEmbeddingTransportFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String() + "/v1"
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("final=%t", final), func(t *testing.T) {
			cfg := runConfig(t, aitest.Serve(t, aitest.Routes{}), func(c *docinfer.Config) {
				c.Embeddings.Client = newClient(t, closed, ai.TranscribeRoute)
			})
			content := dispatch.NewMemoryContent()
			o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(outputInput(t, content, childPart, dispatch.TextOutput, twoChunks)), docinfer.Options{Final: final}))
			if _, ok := errors.AsType[*url.Error](o.err); !ok {
				t.Fatalf("Run against a closed port: %v, want a *url.Error", o.err)
			}
			if docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is true", o.err)
			}
			if o.counts != nil || len(o.batches) > 0 {
				t.Error("a stopped run yielded batches or counts")
			}
		})
	}
}

// A 500, or a 2xx reply that doesn't decode, stops the job; on the last delivery the call's
// chunks are recorded failed instead, never held, and the job goes on.
func TestRunEmbeddingFailed(t *testing.T) {
	replies := map[string]aitest.Reply{
		"500":         aitest.ErrorReply{Status: http.StatusInternalServerError, Message: "broke"},
		"undecodable": aitest.RawReply{Body: []byte(`{"data":`)},
		"one vector":  aitest.EmbeddingsReply{Vectors: [][]float32{{1, 2}}},
	}
	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Embeddings: func(context.Context, aitest.EmbeddingRequest) aitest.Reply { return reply }})

			o, cache := runTwoChunks(t, e, docinfer.Options{})
			if o.err == nil || docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want an error not permanent", o.err)
			}
			stopped(t, o, e, cache)

			o, cache = runTwoChunks(t, e, docinfer.Options{Final: true})
			if o.err != nil {
				t.Fatalf("the final Run: %v, want the job to go on", o.err)
			}
			chunks := o.chunks()
			for _, c := range chunks {
				if c.chunk.Error == "" || c.chunk.Vector != "" {
					t.Errorf("chunk %q: error %q, vector %q; want it recorded failed", c.chunk.Text, c.chunk.Error, c.chunk.Vector)
				}
				if h := held(t, cache, c.chunk.Digest); h != nil {
					t.Errorf("chunk %q failed and was held: %+v", c.chunk.Text, h)
				}
			}
			checkCounts(t, o, docinfer.Counts{Texts: 1, Rejected: 2, Requests: 1})
		})
	}
}

// stopped fails the test unless the run stopped after the endpoint's one request, unsplit,
// yielding nothing but its error and holding nothing.
func stopped(t *testing.T, o outcome, e *aitest.Endpoint, cache docinfer.Cache) {
	t.Helper()
	if o.counts != nil || len(o.batches) > 0 {
		t.Error("a stopped run yielded batches or counts")
	}
	if n := len(e.Requests()); n != 1 {
		t.Errorf("%d requests, want the one call, unsplit", n)
	}
	for _, text := range []string{refusedChunk, "This chunk is embedded."} {
		if h := held(t, cache, sha(text)); h != nil {
			t.Errorf("chunk %q held as %+v by a stopped run", text, h)
		}
	}
}

// Permanent reports exactly the permanent rows and the input's defects: a permanent status, a
// request the format can't send, which Embed never makes but generation does, a mismatched input,
// a dispatch's refused request, made by a real dispatch: an input of neither or both of Path and
// Reader, an input SHA-256 that isn't lowercase hex, or a resume cursor that names no message; an
// image output without a MediaType, a window of another media type, and a refused request, and
// nothing else: not a refused, transient, or failed status, nor a failed reply, a transport
// failure, or a content miss.
func TestPermanent(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unsupported", fmt.Errorf("wrapped: %w", &format.UnsupportedError{Format: "completions", Feature: "content part"}), true},
		{"mismatched input", fmt.Errorf("dispatch: %w", dispatch.ErrSHA256), true},
		{"input of neither Path nor Reader", dispatchError(t, false, dispatch.Input{}), true},
		{"input of both Path and Reader", dispatchError(t, false, dispatch.Input{Path: "a.txt", Reader: strings.NewReader("text")}), true},
		{"input SHA-256 not lowercase hex", dispatchError(t, false, dispatch.Input{Reader: strings.NewReader("text"), SHA256: strings.Repeat("A", 64)}), true},
		{"resume cursor of no message", dispatchError(t, true, dispatch.Input{Reader: strings.NewReader(twoMessages), Name: "store.txt", After: 99}), true},
		{"refused request", fmt.Errorf("docinfer: %w", docinfer.ErrRequest), true},
		{"image without a media type", noMediaTypeError(t), true},
		{"window of another media type", windowDefectError(t), true},
		{"refused status", &ai.APIError{Status: 422}, false},
		{"transient status", &ai.APIError{Status: 503}, false},
		{"failed status", &ai.APIError{Status: 500}, false},
		{"transport failure", &url.Error{Op: "Post", URL: "http://localhost", Err: errors.New("refused")}, false},
		{"missing content", &docinfer.MissingError{Err: errors.New("gone")}, false},
		{"other", errors.New("other"), false},
		{"nil", nil, false},
	}
	for _, status := range []int{401, 403, 404, 405, 409, 418, 451, 501, 505} {
		cases = append(cases, struct {
			name string
			err  error
			want bool
		}{fmt.Sprint(status), fmt.Errorf("wrapped: %w", &ai.APIError{Status: status}), true})
	}
	for _, tc := range cases {
		if got := docinfer.Permanent(tc.err); got != tc.want {
			t.Errorf("Permanent(%s) = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// twoMessages is a store of two messages, 1 and 2, to the fixture stage, which a part named
// store.txt routes to.
const twoMessages = "message 1\nmessage 2"

// dispatchError dispatches in through a dispatcher of docproc's fixture stage over memory stores,
// first dispatching twoMessages whole when prepared, so in may resume it, and returns the error that
// ended the dispatch of in, failing the test unless it matches dispatch.ErrRequest or if the first
// dispatch failed.
func dispatchError(t *testing.T, prepared bool, in dispatch.Input) error {
	t.Helper()
	d, err := dispatch.New(dispatch.Config{
		Stages:  dispatch.Stages{route.Text: dispatchtest.NewStage(nil)},
		Index:   dispatch.NewMemoryIndex(0),
		Content: dispatch.NewMemoryContent(),
		Dir:     t.TempDir(),
		Puts:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared {
		for _, err := range d.Dispatch(t.Context(), dispatch.Input{Reader: strings.NewReader(twoMessages), Name: "store.txt"}) {
			if err != nil {
				t.Fatalf("dispatch of the messages: %v", err)
			}
		}
	}
	for _, err := range d.Dispatch(t.Context(), in) {
		if err != nil {
			if !errors.Is(err, dispatch.ErrRequest) {
				t.Fatalf("dispatch of %+v: %v, want an error matching dispatch.ErrRequest", in, err)
			}
			return err
		}
	}
	t.Fatalf("dispatch of %+v ended without an error", in)
	return nil
}
