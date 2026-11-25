package docinfer_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

const countedText = "One two three four five.\n\nSix seven eight nine ten eleven twelve.\n\n" +
	"Thirteen fourteen fifteen. Sixteen seventeen eighteen nineteen twenty twenty-one twenty-two."

type tokenizeRequests struct {
	mu    sync.Mutex
	seen  []aitest.TokenizeRequest
	reply func(aitest.TokenizeRequest) aitest.Reply
}

func (r *tokenizeRequests) route(_ context.Context, req aitest.TokenizeRequest) aitest.Reply {
	r.mu.Lock()
	r.seen = append(r.seen, req)
	r.mu.Unlock()
	return r.reply(req)
}

func (r *tokenizeRequests) all() []aitest.TokenizeRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]aitest.TokenizeRequest(nil), r.seen...)
}

func wordTokens(prompt string) int { return 2 + len(strings.Fields(prompt)) }

func countByWords(req aitest.TokenizeRequest) aitest.Reply {
	return aitest.TokenizeReply{Count: wordTokens(req.Prompt)}
}

func tokenizeConfig(t *testing.T, e *aitest.Endpoint, bound int) docinfer.Config {
	t.Helper()
	client := profiledClient(t, e.URL(), conventions(passage, queryPrefix))
	return runConfig(t, e, func(c *docinfer.Config) {
		c.Embeddings.Client = client
		c.Chunker.Bound = bound
		c.Chunker.Count = func(ctx context.Context, text string) (int, error) {
			tokens, err := client.Tokenize(ctx, &ai.TokenizeRequest{Model: embeddingModel, Text: text, InputType: ai.InputDocument})
			if err != nil {
				return 0, err
			}
			return tokens.Count, nil
		}
	})
}

func TestRunChunksByTheHostsCount(t *testing.T) {
	var embeds embedRequests
	counts := tokenizeRequests{reply: countByWords}
	e := aitest.Serve(t, aitest.Routes{Embeddings: embeds.route, Tokenize: counts.route})
	const bound = 10
	cfg := tokenizeConfig(t, e, bound)
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content,
		seq(outputInput(t, content, childPart, dispatch.TextOutput, countedText)), docinfer.Options{}))
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	asked := counts.all()
	if len(asked) == 0 {
		t.Fatal("Run asked the tokenize route for no count")
	}
	for _, req := range asked {
		if req.Model != embeddingModel || !req.AddSpecialTokens || !strings.HasPrefix(req.Prompt, "passage: ") {
			t.Errorf("a tokenize request %+v, want the embeddings model, special tokens, and the document prefix", req)
		}
	}
	chunks := o.chunks()
	if len(chunks) < 2 {
		t.Fatalf("%d chunks, want the text cut by its count", len(chunks))
	}
	embedded := map[string]bool{}
	for _, req := range embeds.all() {
		for _, in := range req.Input {
			embedded[in] = true
		}
	}
	for _, c := range chunks {
		if n := wordTokens("passage: " + c.chunk.Text); n > bound {
			t.Errorf("chunk %q measures %d tokens, over the bound %d", c.chunk.Text, n, bound)
		}
		if c.chunk.Vector == "" || !embedded["passage: "+c.chunk.Text] {
			t.Errorf("chunk %q wasn't embedded", c.chunk.Text)
		}
		if countedText[c.chunk.Span.Start:c.chunk.Span.End] != c.chunk.Text {
			t.Errorf("chunk %q isn't its span's text", c.chunk.Text)
		}
	}
}

func TestRunChunkCountFailure(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		final     bool
		permanent bool
	}{
		{"transient", http.StatusServiceUnavailable, false, false},
		{"permanent", http.StatusUnauthorized, false, true},
		{"refused", http.StatusBadRequest, false, true},
		{"failed on the last delivery", http.StatusInternalServerError, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var embeds embedRequests
			counts := tokenizeRequests{reply: func(aitest.TokenizeRequest) aitest.Reply {
				return aitest.ErrorReply{Status: tc.status, Message: "no count"}
			}}
			e := aitest.Serve(t, aitest.Routes{Embeddings: embeds.route, Tokenize: counts.route})
			cfg := tokenizeConfig(t, e, 10)
			content := dispatch.NewMemoryContent()
			o := collect(t, newPipeline(t, cfg).Run(t.Context(), content,
				seq(outputInput(t, content, childPart, dispatch.TextOutput, countedText)), docinfer.Options{Final: tc.final}))
			apiErr, ok := errors.AsType[*ai.APIError](o.err)
			if !ok || apiErr.Status != tc.status || !strings.Contains(o.err.Error(), "chunk part") {
				t.Fatalf("Run: %v, want the count's %d, wrapped as the part's chunking", o.err, tc.status)
			}
			if got := docinfer.Permanent(o.err); got != tc.permanent {
				t.Errorf("Permanent = %t, want %t", got, tc.permanent)
			}
			if len(o.chunks()) != 0 || len(embeds.all()) != 0 {
				t.Errorf("%d chunks yielded and %d embeddings asked, want none", len(o.chunks()), len(embeds.all()))
			}
		})
	}
}

func TestRunBoundBelowACharacter(t *testing.T) {
	counts := tokenizeRequests{reply: countByWords}
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll, Tokenize: counts.route})
	cfg := tokenizeConfig(t, e, 3)
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content,
		seq(outputInput(t, content, childPart, dispatch.TextOutput, countedText)), docinfer.Options{}))
	if !errors.Is(o.err, chunk.ErrRequest) || !docinfer.Permanent(o.err) {
		t.Errorf("Run with every character over the bound: %v, Permanent %t; want chunk.ErrRequest, permanent", o.err, docinfer.Permanent(o.err))
	}
}
