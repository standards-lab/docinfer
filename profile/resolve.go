package profile

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"sync"

	"github.com/standards-lab/go-ai"

	"github.com/standards-lab/docinfer"
)

// Clients is the go-ai client its host built for each capability, against the endpoint the
// capability's settings name. A capability Settings sets needs its client; Embeddings and
// Tokenizer are required. Tokenizer is the endpoint the embeddings' tokenizer names, whose
// Tokenize counts a chunk's tokens; it must front the same deployment as Embeddings. The host
// declares the embeddings' input-type conventions on Embeddings and Tokenizer, and transcription's
// mode on Transcription.
type Clients struct {
	Vision        *ai.Client
	Transcription *ai.Client
	Extraction    *ai.Client
	Embeddings    *ai.Client
	Tokenizer     *ai.Client
}

// State is what a capability's endpoint's model listing says of its model.
type State string

const (
	// Listed is a model the endpoint's listing holds.
	Listed State = "listed"
	// Unlisted is a model the endpoint's listing doesn't hold.
	Unlisted State = "unlisted"
	// Unchecked is a model whose endpoint gives no listing, answering its models route with an
	// *ai.APIError.
	Unchecked State = "unchecked"
	// Unreachable is a model whose endpoint's listing failed otherwise, such as in transport.
	Unreachable State = "unreachable"
)

// Model is a capability's model as [Resolve] resolved it: the capability, its endpoint and model as
// its settings name them, the listing's [State] and the error of an Unchecked or Unreachable
// listing, the listing entry's status, such as llama.cpp's router's "loaded", empty when it gives
// none, and Served, the model the deployment serves under Model, empty when unresolved.
type Model struct {
	Capability docinfer.Capability
	Endpoint   string
	Model      string
	State      State
	Listing    error
	Status     string
	Served     string
}

// Resolve lists each capability s sets through its client's Models, at once, and returns its
// [Model]s, in the order vision, transcription, extraction, embeddings, and the joined error of
// those it couldn't resolve. A capability's served model is its listing entry's root, else its
// served_model. Resolve refuses, matching docinfer.ErrRequest, a root that disagrees with a set
// served_model, a model with neither, and a model the listing doesn't hold, each naming the
// capability, the endpoint, and the model; an endpoint that gives no listing resolves to its
// served_model alone. A listing that fails otherwise is returned wrapped. It panics, naming the
// field, on a nil client of a capability s sets. It sets no deadline: ctx and each client's
// transport bound the listings.
func Resolve(ctx context.Context, s Settings, c Clients) ([]Model, error) {
	type capability struct {
		name     docinfer.Capability
		settings Capability
		client   *ai.Client
		field    string
	}
	var caps []capability
	if v := s.Vision; v != nil {
		caps = append(caps, capability{docinfer.CapabilityVision, v.Capability, c.Vision, "Vision"})
	}
	if t := s.Transcription; t != nil {
		caps = append(caps, capability{docinfer.CapabilityTranscription, t.Capability, c.Transcription, "Transcription"})
	}
	if x := s.Extraction; x != nil {
		caps = append(caps, capability{docinfer.CapabilityExtraction, x.Capability, c.Extraction, "Extraction"})
	}
	caps = append(caps, capability{docinfer.CapabilityEmbeddings, s.Embeddings.Capability, c.Embeddings, "Embeddings"})
	for _, cp := range caps {
		if cp.client == nil {
			panic("profile: Resolve: nil Clients." + cp.field)
		}
	}

	type listed struct {
		models []ai.Model
		err    error
	}
	lists := make([]listed, len(caps))
	var wg sync.WaitGroup
	for i, cp := range caps {
		wg.Go(func() {
			models, err := cp.client.Models(ctx)
			lists[i] = listed{models, err}
		})
	}
	wg.Wait()

	models := make([]Model, len(caps))
	var errs []error
	for i, cp := range caps {
		m := Model{Capability: cp.name, Endpoint: cp.settings.Endpoint, Model: cp.settings.Model}
		l := lists[i]
		var err error
		switch _, apiErr := errors.AsType[*ai.APIError](l.err); {
		case apiErr:
			m.State, m.Listing = Unchecked, l.err
			m.Served, err = served(m, false, "", cp.settings.ServedModel)
		case l.err != nil:
			m.State, m.Listing = Unreachable, l.err
			err = fmt.Errorf("profile: %s: endpoint %q is unreachable: %w", m.Capability, m.Endpoint, l.err)
		default:
			var root string
			var found bool
			m.Status, root, found = entryOf(l.models, m.Model)
			if !found {
				m.State = Unlisted
				err = refuse("%s: endpoint %q doesn't list model %q", m.Capability, m.Endpoint, m.Model)
				break
			}
			m.State = Listed
			m.Served, err = served(m, true, root, cp.settings.ServedModel)
		}
		models[i] = m
		errs = append(errs, err)
	}
	return models, errors.Join(errs...)
}

func served(m Model, listed bool, root, configured string) (string, error) {
	switch {
	case root != "" && configured != "" && root != configured:
		return "", refuse("%s: endpoint %q lists model %q with root %q, and served_model is %q: set %s.served_model to %q, or leave it unset to take the listing's",
			m.Capability, m.Endpoint, m.Model, root, configured, m.Capability, root)
	case root != "":
		return root, nil
	case configured != "":
		return configured, nil
	case listed:
		return "", refuse("%s: endpoint %q lists no root for model %q, and served_model is unset: set %s.served_model to the model the deployment serves under %q",
			m.Capability, m.Endpoint, m.Model, m.Capability, m.Model)
	}
	return "", refuse("%s: endpoint %q gives no model listing for model %q, and served_model is unset: set %s.served_model to the model the deployment serves under %q",
		m.Capability, m.Endpoint, m.Model, m.Capability, m.Model)
}

func entryOf(models []ai.Model, id string) (status, root string, found bool) {
	for _, m := range models {
		if m.ID != id {
			continue
		}
		var entry struct {
			Root   string `json:"root"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		}
		if jsonv2.Unmarshal(m.Native, &entry) != nil {
			return "", "", true
		}
		return entry.Status.Value, entry.Root, true
	}
	return "", "", false
}
