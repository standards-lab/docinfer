package run

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/standards-lab/go-ai"

	"github.com/standards-lab/docinfer"
)

const (
	stateLoaded      = "loaded"
	stateListed      = "listed"
	stateUnlisted    = "unlisted"
	stateUnchecked   = "unchecked"
	stateUnreachable = "unreachable"
	stateSkipped     = "skipped"
)

const modelsTimeout = 30 * time.Second

type modelRow struct {
	capability docinfer.Capability
	endpoint   string
	model      string
	state      string
}

type modelCheck struct {
	rows     []modelRow
	warnings []string
	served   map[docinfer.Capability]string
}

type listed struct {
	models []ai.Model
	err    error
}

func checkModels(ctx context.Context, cfg Config) (modelCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	type capability struct {
		name   docinfer.Capability
		client *ai.Client
		model  string
		served string
	}
	caps := []capability{
		{name: docinfer.CapabilityVision},
		{name: docinfer.CapabilityTranscription},
		{name: docinfer.CapabilityEmbeddings, client: cfg.Embeddings.Client, model: cfg.Embeddings.Model, served: cfg.Embeddings.Served},
	}
	if v := cfg.Vision; v != nil {
		caps[0].client, caps[0].model, caps[0].served = v.Client, v.Model, v.Served
	}
	if t := cfg.Transcription; t != nil {
		caps[1].client, caps[1].model, caps[1].served = t.Client, t.Model, t.Served
	}

	lists := make([]listed, len(caps))
	var wg sync.WaitGroup
	for i, c := range caps {
		if c.client == nil {
			continue
		}
		wg.Go(func() {
			models, err := c.client.Models(ctx)
			lists[i] = listed{models: models, err: err}
		})
	}
	wg.Wait()

	var (
		check = modelCheck{served: map[docinfer.Capability]string{}}
		errs  []error
	)
	for i, c := range caps {
		if c.client == nil {
			check.rows = append(check.rows, modelRow{capability: c.name, endpoint: "-", model: "-", state: stateSkipped})
			continue
		}
		endpoint := cfg.Endpoints[c.name].Name
		row := modelRow{capability: c.name, endpoint: endpoint, model: c.model}
		l := lists[i]
		if apiErr, ok := errors.AsType[*ai.APIError](l.err); ok {
			row.state = stateUnchecked
			check.warnings = append(check.warnings,
				fmt.Sprintf("%s: endpoint %q gives no model listing (%v); its model is unchecked", c.name, endpoint, apiErr))
			check.rows = append(check.rows, row)
			served, err := resolveServed(c.name, endpoint, c.model, "", c.served)
			check.served[c.name] = served
			errs = append(errs, err)
			continue
		}
		if l.err != nil {
			row.state = stateUnreachable
			errs = append(errs, fmt.Errorf("%s: endpoint %q is unreachable: %w", c.name, endpoint, l.err))
			check.rows = append(check.rows, row)
			continue
		}
		status, root, found := entryOf(l.models, c.model)
		if found {
			served, err := resolveServed(c.name, endpoint, c.model, root, c.served)
			check.served[c.name] = served
			errs = append(errs, err)
		}
		switch {
		case !found:
			row.state = stateUnlisted
			errs = append(errs, fmt.Errorf("%s: endpoint %q doesn't list model %q", c.name, endpoint, c.model))
		case status == "":
			row.state = stateListed
		default:
			row.state = status
			if status != stateLoaded {
				check.warnings = append(check.warnings,
					fmt.Sprintf("%s: endpoint %q lists model %q as %s", c.name, endpoint, c.model, status))
			}
		}
		check.rows = append(check.rows, row)
	}
	return check, errors.Join(errs...)
}

func resolveServed(name docinfer.Capability, endpoint, model, root, configured string) (string, error) {
	switch {
	case root != "" && configured != "" && root != configured:
		return "", fmt.Errorf("%s: endpoint %q lists model %q with root %q, and served_model is %q: set %s.served_model to %q, or leave it unset to take the listing's",
			name, endpoint, model, root, configured, name, root)
	case root != "":
		return root, nil
	case configured != "":
		return configured, nil
	}
	return "", fmt.Errorf("%s: endpoint %q lists no root for model %q, and served_model is unset: set %s.served_model to the model the deployment serves under %q",
		name, endpoint, model, name, model)
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

func (cfg Config) withServed(served map[docinfer.Capability]string) Config {
	if v := cfg.Vision; v != nil {
		resolved := *v
		resolved.Served = served[docinfer.CapabilityVision]
		cfg.Vision = &resolved
	}
	if t := cfg.Transcription; t != nil {
		resolved := *t
		resolved.Served = served[docinfer.CapabilityTranscription]
		cfg.Transcription = &resolved
	}
	cfg.Embeddings.Served = served[docinfer.CapabilityEmbeddings]
	cfg.Chunker.Model = cfg.Embeddings.Served
	return cfg
}
