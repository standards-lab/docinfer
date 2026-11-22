package app

import (
	"fmt"
	"net/http"
	"os"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai"
)

const envPrefix = "DOCINFER"

var configVariable = config.EnvName(envPrefix, "config")

func defineInfrastructure(g *graph.Graph, n *Nodes, configDir *string) {
	n.LifecycleConfig = g.Define("lifecycle config", newLifecycleConfig)
	n.Config = g.Define("config", func(*graph.Scope) (Config, error) {
		dir := *configDir
		if dir == "" {
			dir = os.Getenv(configVariable)
		}
		if dir == "" {
			return Config{}, fmt.Errorf("no configuration directory: set --config or %s", configVariable)
		}
		return loadConfig(dir)
	})
	n.HTTPClients = g.Define("http clients", func(s *graph.Scope) (map[string]*http.Client, error) {
		clients := map[string]*http.Client{}
		for name, e := range s.Use(n.Config).Endpoints {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.ResponseHeaderTimeout = e.ResponseHeaderTimeout.Duration()
			clients[name] = &http.Client{Transport: transport}
		}
		return clients, nil
	})
	n.VisionClient = g.Define("vision client", func(s *graph.Scope) (*ai.Client, error) {
		v := s.Use(n.Config).Vision
		if v == nil {
			return nil, nil
		}
		return newClient(s, n, v.Endpoint, ai.TranscribeRoute)
	})
	n.TranscriptionClient = g.Define("transcription client", func(s *graph.Scope) (*ai.Client, error) {
		t := s.Use(n.Config).Transcription
		if t == nil {
			return nil, nil
		}
		return newClient(s, n, t.Endpoint, *t.Mode)
	})
	n.EmbeddingsClient = g.Define("embeddings client", func(s *graph.Scope) (*ai.Client, error) {
		return newClient(s, n, s.Use(n.Config).Embeddings.Endpoint, ai.TranscribeRoute)
	})
}

func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

func newClient(s *graph.Scope, n *Nodes, endpoint string, mode ai.TranscriptionMode) (*ai.Client, error) {
	e := s.Use(n.Config).Endpoints[endpoint]
	wireFormat, err := e.wireFormat()
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", endpoint, err)
	}
	opts := e.options()
	opts.TranscriptionMode = mode
	client, err := ai.New(e.BaseURL, s.Use(n.HTTPClients)[endpoint], wireFormat, opts)
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", endpoint, err)
	}
	return client, nil
}
