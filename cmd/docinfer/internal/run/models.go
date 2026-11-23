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
	}
	caps := []capability{
		{name: docinfer.CapabilityVision},
		{name: docinfer.CapabilityTranscription},
		{name: docinfer.CapabilityEmbeddings, client: cfg.Embeddings.Client, model: cfg.Embeddings.Model},
	}
	if v := cfg.Vision; v != nil {
		caps[0].client, caps[0].model = v.Client, v.Model
	}
	if t := cfg.Transcription; t != nil {
		caps[1].client, caps[1].model = t.Client, t.Model
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
		check modelCheck
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
			continue
		}
		if l.err != nil {
			row.state = stateUnreachable
			errs = append(errs, fmt.Errorf("%s: endpoint %q is unreachable: %w", c.name, endpoint, l.err))
			check.rows = append(check.rows, row)
			continue
		}
		status, found := statusOf(l.models, c.model)
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

func statusOf(models []ai.Model, id string) (status string, found bool) {
	for _, m := range models {
		if m.ID != id {
			continue
		}
		var entry struct {
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		}
		if jsonv2.Unmarshal(m.Native, &entry) != nil {
			return "", true
		}
		return entry.Status.Value, true
	}
	return "", false
}
