package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/standards-lab/go-ai"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"
)

const (
	stateLoaded  = "loaded"
	stateSkipped = "skipped"
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
	models   []profile.Model
}

func checkModels(ctx context.Context, cfg Config) (modelCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	models, err := profile.Resolve(ctx, cfg.Settings, cfg.Clients)
	check := modelCheck{models: models}
	resolved := map[docinfer.Capability]profile.Model{}
	for _, m := range models {
		resolved[m.Capability] = m
	}
	for _, name := range []docinfer.Capability{
		docinfer.CapabilityVision,
		docinfer.CapabilityTranscription,
		docinfer.CapabilityExtraction,
		docinfer.CapabilityEmbeddings,
	} {
		m, ok := resolved[name]
		switch {
		case !ok && name == docinfer.CapabilityExtraction:
			continue
		case !ok:
			check.rows = append(check.rows, modelRow{capability: name, endpoint: "-", model: "-", state: stateSkipped})
			continue
		}
		row := modelRow{capability: name, endpoint: m.Endpoint, model: m.Model, state: string(m.State)}
		switch {
		case m.State == profile.Unchecked:
			apiErr, _ := errors.AsType[*ai.APIError](m.Listing)
			check.warnings = append(check.warnings,
				fmt.Sprintf("%s: endpoint %q gives no model listing (%v); its model is unchecked", name, m.Endpoint, apiErr))
		case m.State == profile.Listed && m.Status != "":
			row.state = m.Status
			if m.Status != stateLoaded {
				check.warnings = append(check.warnings,
					fmt.Sprintf("%s: endpoint %q lists model %q as %s", name, m.Endpoint, m.Model, m.Status))
			}
		}
		check.rows = append(check.rows, row)
	}
	return check, err
}
