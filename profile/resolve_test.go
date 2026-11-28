package profile_test

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"
)

func newClient(t *testing.T, e *aitest.Endpoint, opts ai.Options) *ai.Client {
	t.Helper()
	c, err := ai.New(e.URL(), http.DefaultClient, completions.New(completions.Options{}), opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func listing(entries ...string) func(context.Context, aitest.ModelsRequest) aitest.Reply {
	return func(context.Context, aitest.ModelsRequest) aitest.Reply {
		r := aitest.ModelsReply{}
		for _, e := range entries {
			r.Entries = append(r.Entries, jsontext.Value(e))
		}
		return r
	}
}

func embeddingsOnly(t *testing.T, servedModel string) profile.Settings {
	t.Helper()
	s := decode(t, settingsJSON)
	s.Vision, s.Transcription, s.Extraction = nil, nil, nil
	s.Embeddings.ServedModel = servedModel
	return s
}

func resolveEmbeddings(t *testing.T, routes aitest.Routes, servedModel string) (profile.Model, error) {
	t.Helper()
	e := aitest.Serve(t, routes)
	c := newClient(t, e, ai.Options{})
	models, err := profile.Resolve(t.Context(), embeddingsOnly(t, servedModel), profile.Clients{Embeddings: c, Tokenizer: c})
	if len(models) != 1 || models[0].Capability != docinfer.CapabilityEmbeddings || models[0].Endpoint != "embed" || models[0].Model != "e5" {
		t.Fatalf("Resolve: models %+v, want the embeddings' alone, its endpoint and model", models)
	}
	return models[0], err
}

func TestResolveTakesTheListingsRoot(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(
		`{"id":"other","root":"org/other"}`,
		`{"id":"e5","object":"model","root":"intfloat/multilingual-e5-large","status":{"value":"loaded"}}`,
	)}, "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if m.Served != "intfloat/multilingual-e5-large" || m.State != profile.Listed || m.Status != "loaded" || m.Listing != nil {
		t.Errorf("model %+v, want the listing's root, listed and loaded", m)
	}
}

func TestResolveTakesARootThatAgreesWithServedModel(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(`{"id":"e5","root":"org/e5"}`)}, "org/e5")
	if err != nil || m.Served != "org/e5" {
		t.Errorf("Resolve: %+v, %v, want org/e5", m, err)
	}
}

func TestResolveTakesServedModelWhenTheListingGivesNoRoot(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(`{"id":"e5","object":"model"}`)}, "org/e5")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if m.Served != "org/e5" || m.State != profile.Listed || m.Status != "" {
		t.Errorf("model %+v, want served_model, listed", m)
	}
}

func TestResolveTakesServedModelWhenTheEndpointGivesNoListing(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{}, "org/e5")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, ok := errors.AsType[*ai.APIError](m.Listing); m.Served != "org/e5" || m.State != profile.Unchecked || !ok {
		t.Errorf("model %+v, want served_model, unchecked, with the listing's *ai.APIError", m)
	}
}

func refusedNaming(t *testing.T, err error, want ...string) {
	t.Helper()
	if !errors.Is(err, docinfer.ErrRequest) {
		t.Fatalf("Resolve: %v, want ErrRequest", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("Resolve: %v, want it naming %s", err, w)
		}
	}
}

func TestResolveRefusesARootThatDisagreesWithServedModel(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(`{"id":"e5","root":"org/e5-large"}`)}, "org/e5-small")
	refusedNaming(t, err, "embeddings", `endpoint "embed"`, `model "e5"`, `"org/e5-large"`, `"org/e5-small"`)
	if m.Served != "" || m.State != profile.Listed {
		t.Errorf("model %+v, want listed and unresolved", m)
	}
}

func TestResolveRefusesAModelWithNeitherRootNorServedModel(t *testing.T) {
	t.Run("listed without a root", func(t *testing.T) {
		m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(`{"id":"e5"}`)}, "")
		refusedNaming(t, err, "embeddings", `endpoint "embed"`, `model "e5"`, "served_model")
		if m.Served != "" {
			t.Errorf("model %+v, want unresolved", m)
		}
	})
	t.Run("no listing", func(t *testing.T) {
		m, err := resolveEmbeddings(t, aitest.Routes{}, "")
		refusedNaming(t, err, "embeddings", `endpoint "embed"`, `model "e5"`, "served_model")
		if m.Served != "" || m.State != profile.Unchecked {
			t.Errorf("model %+v, want unchecked and unresolved", m)
		}
	})
}

func TestResolveRefusesAnUnlistedModel(t *testing.T) {
	m, err := resolveEmbeddings(t, aitest.Routes{Models: listing(`{"id":"other","root":"org/other"}`)}, "org/e5")
	refusedNaming(t, err, "embeddings", `endpoint "embed"`, `model "e5"`)
	if m.Served != "" || m.State != profile.Unlisted {
		t.Errorf("model %+v, want unlisted and unresolved", m)
	}
}

func TestResolveReportsAnUnreachableEndpoint(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listing(`{"id":"e5","root":"org/e5"}`)})
	c := newClient(t, e, ai.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	models, err := profile.Resolve(ctx, embeddingsOnly(t, ""), profile.Clients{Embeddings: c, Tokenizer: c})
	if !errors.Is(err, context.Canceled) || errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), `endpoint "embed"`) {
		t.Errorf("Resolve under an ended context: %v, want context.Canceled naming the endpoint, not ErrRequest", err)
	}
	if m := models[0]; m.State != profile.Unreachable || m.Served != "" || !errors.Is(m.Listing, context.Canceled) {
		t.Errorf("model %+v, want unreachable and unresolved", m)
	}
}

func TestResolveListsEachSetCapabilityInOrder(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listing(
		`{"id":"vision","root":"org/vision-1"}`,
		`{"id":"whisper","root":"org/whisper-1"}`,
		`{"id":"agent","root":"org/agent-1"}`,
		`{"id":"e5","root":"org/e5-1"}`,
	)})
	c := newClient(t, e, ai.Options{})
	models, err := profile.Resolve(t.Context(), decode(t, settingsJSON),
		profile.Clients{Vision: c, Transcription: c, Extraction: c, Embeddings: c, Tokenizer: c})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []profile.Model{
		{Capability: docinfer.CapabilityVision, Endpoint: "gpu", Model: "vision", State: profile.Listed, Served: "org/vision-1"},
		{Capability: docinfer.CapabilityTranscription, Endpoint: "audio", Model: "whisper", State: profile.Listed, Served: "org/whisper-1"},
		{Capability: docinfer.CapabilityExtraction, Endpoint: "gpu", Model: "agent", State: profile.Listed, Served: "org/agent-1"},
		{Capability: docinfer.CapabilityEmbeddings, Endpoint: "embed", Model: "e5", State: profile.Listed, Served: "org/e5-1"},
	}
	if len(models) != len(want) {
		t.Fatalf("Resolve: %d models, want %d", len(models), len(want))
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("model %d: %+v, want %+v", i, models[i], want[i])
		}
	}
	if n := len(e.Requests()); n != len(want) {
		t.Errorf("%d requests, want a listing per capability", n)
	}
}

func TestResolvePanicsNamingTheClient(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{})
	c := newClient(t, e, ai.Options{})
	cases := []struct {
		field string
		unset func(*profile.Clients)
	}{
		{"Clients.Vision", func(cl *profile.Clients) { cl.Vision = nil }},
		{"Clients.Transcription", func(cl *profile.Clients) { cl.Transcription = nil }},
		{"Clients.Extraction", func(cl *profile.Clients) { cl.Extraction = nil }},
		{"Clients.Embeddings", func(cl *profile.Clients) { cl.Embeddings = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			clients := profile.Clients{Vision: c, Transcription: c, Extraction: c, Embeddings: c, Tokenizer: c}
			tc.unset(&clients)
			defer func() {
				if r := recover(); r == nil || !strings.Contains(r.(string), tc.field) {
					t.Errorf("Resolve with a nil %s: panic %v, want one naming it", tc.field, r)
				}
			}()
			_, _ = profile.Resolve(t.Context(), decode(t, settingsJSON), clients)
		})
	}
}
