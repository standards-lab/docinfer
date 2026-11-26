package rig

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// Endpoint is the row a measurement sends to: its API root, such as
// http://gb10-2:3000/vision/v1, the bearer token each request carries, empty sending none, and
// the transport's ResponseHeaderTimeout.
type Endpoint struct {
	BaseURL       string
	Token         string
	HeaderTimeout time.Duration
}

// FromEnv fills e's unset BaseURL and Token from go-ai's tier variables for role, such as
// AI_VISION_BASE_URL and AI_VISION_TOKEN, and refuses an Endpoint left with no BaseURL.
func (e *Endpoint) FromEnv(role string) error {
	if e.BaseURL == "" {
		e.BaseURL = os.Getenv("AI_" + role + "_BASE_URL")
	}
	if e.Token == "" {
		e.Token = os.Getenv("AI_" + role + "_TOKEN")
	}
	if e.BaseURL == "" {
		return fmt.Errorf("no endpoint: set -base-url or AI_%s_BASE_URL", role)
	}
	return nil
}

// Client returns a completions-format client of e, its transport a clone of the default one with
// e's HeaderTimeout, wrapped by wrap when wrap is not nil, retrying 3 attempts from 500ms to 30s,
// as cmd/docinfer starts its endpoints.
func (e Endpoint) Client(wrap func(http.RoundTripper) http.RoundTripper, opts ai.Options) (*ai.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = e.HeaderTimeout
	transport.MaxIdleConnsPerHost = 64
	var rt http.RoundTripper = transport
	if wrap != nil {
		rt = wrap(transport)
	}
	opts.Retry = ai.RetryPolicy{MaxAttempts: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
	if e.Token != "" {
		token := e.Token
		opts.Token = func(context.Context) (string, error) { return token, nil }
	}
	return ai.New(e.BaseURL, &http.Client{Transport: rt}, completions.New(completions.Options{}), opts)
}

// Embeddings serves an OpenAI-compatible embeddings route on loopback, answering every input with
// the same vector of dims ones, and returns a client of it and the function that stops it.
func Embeddings(dims int) (*ai.Client, func(), error) {
	vec := make([]byte, 4*dims)
	for i := range dims {
		binary.LittleEndian.PutUint32(vec[4*i:], math.Float32bits(1))
	}
	enc := base64.StdEncoding.EncodeToString(vec)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type datum struct {
			Index     int    `json:"index"`
			Embedding string `json:"embedding"`
		}
		data := make([]datum, len(req.Input))
		for i := range data {
			data[i] = datum{i, enc}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{}})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	c, err := ai.New("http://"+ln.Addr().String()+"/v1", &http.Client{}, completions.New(completions.Options{}), ai.Options{})
	if err != nil {
		return nil, nil, errors.Join(err, srv.Close())
	}
	return c, func() { _ = srv.Close() }, nil
}

// Append marshals v and appends it to the file at path as one JSON line, creating the file.
func Append(path string, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}
