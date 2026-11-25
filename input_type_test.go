package docinfer_test

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
)

func conventions(document, query *ai.InputConvention) ai.EmbeddingProfile {
	p := ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{}}
	if document != nil {
		p.InputTypes[ai.InputDocument] = *document
	}
	if query != nil {
		p.InputTypes[ai.InputQuery] = *query
	}
	return p
}

func profiledClient(t *testing.T, baseURL string, profile ai.EmbeddingProfile) *ai.Client {
	t.Helper()
	c, err := ai.New(baseURL, http.DefaultClient, completions.New(completions.Options{}), ai.Options{
		TranscriptionMode: ai.TranscribeRoute, Embedding: profile,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type embedRequests struct {
	mu   sync.Mutex
	seen []aitest.EmbeddingRequest
}

func (r *embedRequests) route(ctx context.Context, req aitest.EmbeddingRequest) aitest.Reply {
	r.mu.Lock()
	r.seen = append(r.seen, req)
	r.mu.Unlock()
	return embedAll(ctx, req)
}

func (r *embedRequests) all() []aitest.EmbeddingRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]aitest.EmbeddingRequest(nil), r.seen...)
}

var (
	passage     = &ai.InputConvention{Prefix: "passage: ", Native: jsontext.Value(`{"input_type":"search_document"}`)}
	queryPrefix = &ai.InputConvention{Prefix: "query: "}
)

func TestRunSendsDocuments(t *testing.T) {
	cases := []struct {
		name    string
		profile ai.EmbeddingProfile
		prefix  string
		native  string
	}{
		{"no profile", ai.EmbeddingProfile{}, "", ""},
		{"query convention alone", conventions(nil, queryPrefix), "", ""},
		{"document convention", conventions(passage, queryPrefix), "passage: ", `{"input_type":"search_document"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r embedRequests
			e := aitest.Serve(t, aitest.Routes{Embeddings: r.route})
			cfg := runConfig(t, e, func(c *docinfer.Config) {
				c.Embeddings.Client = profiledClient(t, e.URL(), tc.profile)
			})
			p := newPipeline(t, cfg)
			content := dispatch.NewMemoryContent()
			o := collect(t, p.Run(t.Context(), content,
				seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), docinfer.Options{}))
			if o.err != nil {
				t.Fatalf("Run: %v", o.err)
			}
			chunks := o.chunks()
			if len(chunks) == 0 {
				t.Fatal("Run yielded no chunks")
			}
			sent := map[string]bool{}
			for _, req := range r.all() {
				if string(req.Native) != tc.native {
					t.Errorf("an embeddings request's native members %s, want %q", req.Native, tc.native)
				}
				for _, in := range req.Input {
					if !strings.HasPrefix(in, tc.prefix) {
						t.Errorf("embedded %q, want it led by %q", in, tc.prefix)
					}
					sent[in] = true
				}
			}
			for _, c := range chunks {
				if !sent[tc.prefix+c.chunk.Text] {
					t.Errorf("chunk %q wasn't sent as %q", c.chunk.Text, tc.prefix+c.chunk.Text)
				}
				if strings.HasPrefix(c.chunk.Text, "passage: ") {
					t.Errorf("chunk %q carries the prefix, which only the request should", c.chunk.Text)
				}
			}
		})
	}
}

func TestRunReembedsOnTheDocumentConventionAlone(t *testing.T) {
	cache := docinfer.NewMemoryCache()
	embedded := func(profile ai.EmbeddingProfile) int {
		t.Helper()
		var r embedRequests
		e := aitest.Serve(t, aitest.Routes{Embeddings: r.route})
		cfg := runConfig(t, e, func(c *docinfer.Config) {
			c.Embeddings.Client = profiledClient(t, e.URL(), profile)
			c.Cache = cache
		})
		content := dispatch.NewMemoryContent()
		o := collect(t, newPipeline(t, cfg).Run(t.Context(), content,
			seq(outputInput(t, content, childPart, dispatch.TextOutput, plainText)), docinfer.Options{}))
		if o.err != nil {
			t.Fatalf("Run: %v", o.err)
		}
		return len(r.all())
	}
	if n := embedded(conventions(passage, queryPrefix)); n == 0 {
		t.Fatal("the first run sent no embeddings request")
	}
	if n := embedded(conventions(passage, &ai.InputConvention{Prefix: "search_query: "})); n != 0 {
		t.Errorf("a changed query convention sent %d embeddings requests, want the cache's vectors", n)
	}
	if n := embedded(conventions(&ai.InputConvention{Prefix: "search_document: "}, queryPrefix)); n == 0 {
		t.Error("a changed document convention sent no embeddings request, want the chunks embedded again")
	}
}

func TestEmbeddingsIdentityKeysTheDocumentConvention(t *testing.T) {
	identity := func(profile ai.EmbeddingProfile) docinfer.Identity {
		t.Helper()
		return docinfer.Embeddings{
			Client: profiledClient(t, "http://localhost/v1", profile), Model: "alias", Served: "org/e5",
		}.Identity()
	}
	bare := identity(ai.EmbeddingProfile{})
	if bare != (docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "org/e5"}) {
		t.Errorf("with no profile, Identity %+v, want settings \"\"", bare)
	}
	if got := identity(conventions(nil, queryPrefix)); got != bare {
		t.Errorf("with a query convention alone, Identity %+v, want %+v", got, bare)
	}
	prefixed := identity(conventions(&ai.InputConvention{Prefix: "passage: "}, nil))
	if want := `{"request":"1","input_type":"document","prefix":"passage: "}`; prefixed.Settings != want {
		t.Errorf("with a document prefix, settings %s, want %s", prefixed.Settings, want)
	}
	spaced := identity(conventions(&ai.InputConvention{Native: jsontext.Value(`{ "truncate": "END", "input_type": 1.50 }`)}, nil))
	if want := `{"request":"1","input_type":"document","prefix":"","native":{"input_type":1.50,"truncate":"END"}}`; spaced.Settings != want {
		t.Errorf("with a document native, settings %s, want %s", spaced.Settings, want)
	}

	content := sha("a chunk's text")
	keys := map[string]string{
		"no profile":        docinfer.Key(bare, content),
		"passage: ":         docinfer.Key(prefixed, content),
		"search_document:":  docinfer.Key(identity(conventions(&ai.InputConvention{Prefix: "search_document: "}, nil)), content),
		"native":            docinfer.Key(spaced, content),
		"prefix and native": docinfer.Key(identity(conventions(passage, nil)), content),
	}
	seen := map[string]string{}
	for name, key := range keys {
		if other, ok := seen[key]; ok {
			t.Errorf("%s and %s key the same", name, other)
		}
		seen[key] = name
	}
	for _, query := range []*ai.InputConvention{nil, queryPrefix, {Prefix: "search_query: ", Native: jsontext.Value(`{"input_type":"q"}`)}} {
		if got := docinfer.Key(identity(conventions(passage, query)), content); got != keys["prefix and native"] {
			t.Errorf("query convention %+v changed the key", query)
		}
	}
}

func TestProfileReportsTheQueryConvention(t *testing.T) {
	cfg := validConfig(t)
	native := jsontext.Value(`{"input_type":"search_query"}`)
	cfg.Embeddings.Client = profiledClient(t, "http://localhost/v1",
		conventions(passage, &ai.InputConvention{Prefix: "query: ", Native: native}))
	p, err := docinfer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	copy(native, `{"input_type":"changed_qry"}`)
	first := p.Profile()
	first.EmbeddingQuery.Prefix = "x"
	copy(first.EmbeddingQuery.Native, `{"input_type":"changed_qry"}`)
	got := p.Profile()
	if got.EmbeddingQuery == nil || got.EmbeddingQuery.Prefix != "query: " || string(got.EmbeddingQuery.Native) != `{"input_type":"search_query"}` {
		t.Errorf("Profile.EmbeddingQuery %+v, want the client's query convention", got.EmbeddingQuery)
	}
	if want := `{"request":"1","input_type":"document","prefix":"passage: ","native":{"input_type":"search_document"}}`; got.Embeddings.Settings != want {
		t.Errorf("Profile.Embeddings' settings %s, want %s", got.Embeddings.Settings, want)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"embedding_query":{"prefix":"query: ","native":{"input_type":"search_query"}}`; !strings.Contains(string(b), want) {
		t.Errorf("Profile's JSON %s, want it to hold %s", b, want)
	}

	for name, profile := range map[string]ai.EmbeddingProfile{
		"no profile":                  {},
		"a document convention alone": conventions(passage, nil),
	} {
		cfg := validConfig(t)
		cfg.Embeddings.Client = profiledClient(t, "http://localhost/v1", profile)
		p, err := docinfer.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		pr := p.Profile()
		if pr.EmbeddingQuery != nil {
			t.Errorf("%s: Profile.EmbeddingQuery %+v, want nil", name, pr.EmbeddingQuery)
		}
		b, err := json.Marshal(pr)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "embedding_query") {
			t.Errorf("%s: Profile's JSON %s holds embedding_query", name, b)
		}
	}
}
