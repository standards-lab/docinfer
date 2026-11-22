package app

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/standards-lab/go-core/config"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

// Config is docinfer's configuration: its endpoints, by name, and the capabilities that send to
// them. Vision and Transcription are optional, and a capability left unset is skipped;
// Embeddings is required. Each setting is its JSON key's path, such as
// endpoints.<name>.response_header_timeout, and DOCINFER_ followed by that path, such as
// DOCINFER_ENDPOINTS_<NAME>_RESPONSE_HEADER_TIMEOUT, overrides it: a map's override is a JSON
// object, and an endpoint's only when a file names the endpoint. [Config.Finalize] gives each
// setting a file and its override leave unset its starting value, which the library leaves none.
type Config struct {
	Endpoints     map[string]*Endpoint `json:"endpoints"`
	Vision        *Vision              `json:"vision"`
	Transcription *Transcription       `json:"transcription"`
	Embeddings    Embeddings           `json:"embeddings"`
	Run           Run                  `json:"run"`
}

// Endpoint is one model server and how docinfer speaks to it, each capability that names it
// through a client of its own and the endpoint's one HTTP client:
//
//   - base_url: its API root, such as http://gb10:8080/v1, as go-ai's ai.New takes it
//   - format: the wire format, completions, messages, or responses
//   - profile: the format's options, a JSON object keyed by format name, as go-ai's tools/infer
//     decodes its profile, such as {"responses":{"refused_efforts":["none"]}}; optional
//   - query: parameters added to every request's URL; optional
//   - response_header_timeout: the transport's wait for a reply's header, starting at 180s; 0
//     waits for as long as the reply takes
//   - retry: go-ai's RetryPolicy, max_attempts starting at 3, base_delay at 500ms, and max_delay
//     at 30s
//   - token and headers: the bearer token and the fixed headers each request carries, which
//     belong in the secrets file; optional
type Endpoint struct {
	BaseURL               string            `json:"base_url"`
	Format                string            `json:"format"`
	Profile               json.RawMessage   `json:"profile"`
	Query                 map[string]string `json:"query"`
	ResponseHeaderTimeout *config.Duration  `json:"response_header_timeout"`
	Retry                 Retry             `json:"retry"`
	Token                 string            `json:"token"`
	Headers               map[string]string `json:"headers"`
}

// Retry is an endpoint's retry policy, go-ai's ai.RetryPolicy, which ai.New checks.
type Retry struct {
	MaxAttempts *int             `json:"max_attempts"`
	BaseDelay   *config.Duration `json:"base_delay"`
	MaxDelay    *config.Duration `json:"max_delay"`
}

// Capability is what every capability names: its endpoint, by its key in endpoints, the model
// its requests name there, and the most requests it keeps in flight, its concurrency, starting at
// 4.
type Capability struct {
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	Concurrency *int   `json:"concurrency"`
}

// Vision is the vision capability: what each image is asked, prompt, the cap on its reply,
// max_tokens, starting at 2048, and the reasoning effort, any go-ai's protocol.ReasoningEffort
// accepts, starting at none.
type Vision struct {
	Capability
	Prompt    string                   `json:"prompt"`
	MaxTokens *int                     `json:"max_tokens"`
	Reasoning protocol.ReasoningEffort `json:"reasoning"`
}

// Transcription is the transcription capability: its prompt and language, empty sending none;
// its mode, route or chat, starting at route, which its own client transcribes in, chat needing
// its endpoint's format to be completions; and the words a second of speech holds at most,
// words_per_second, starting at 8.
type Transcription struct {
	Capability
	Prompt         string                `json:"prompt"`
	Language       string                `json:"language"`
	Mode           *ai.TranscriptionMode `json:"mode"`
	WordsPerSecond *int                  `json:"words_per_second"`
}

// Embeddings is the embeddings capability: the embedding space's dimensions, which have no
// starting value; the texts one request carries, batch, starting at 16; and the most characters a
// chunk holds, chunk_size, starting at 2000.
type Embeddings struct {
	Capability
	Dimensions *int `json:"dimensions"`
	Batch      *int `json:"batch"`
	ChunkSize  *int `json:"chunk_size"`
}

// Run is the run's own settings: a job's inputs admitted and not yet finished, pending, starting
// at 16, and a batch's marshalled JSON's cap in bytes, batch_bytes, starting at 524288.
type Run struct {
	Pending    *int `json:"pending"`
	BatchBytes *int `json:"batch_bytes"`
}

const (
	startResponseHeaderTimeout = 180 * time.Second
	startRetryAttempts         = 3
	startRetryBaseDelay        = 500 * time.Millisecond
	startRetryMaxDelay         = 30 * time.Second
	startConcurrency           = 4
	startVisionPrompt          = "Transcribe the text in this picture. If it has no text, describe it in one sentence."
	startVisionMaxTokens       = 2048
	startVisionReasoning       = protocol.ReasoningEffortNone
	startWordsPerSecond        = 8
	startEmbeddingsBatch       = 16
	startChunkSize             = 2000
	startPending               = 16
	startBatchBytes            = 512 << 10
)

// Merge overlays src's set settings onto c, field by field, so a secrets file can carry an
// endpoint's token and headers alone. A map's members are added to c's, src's winning.
func (c *Config) Merge(src *Config) {
	for name, e := range src.Endpoints {
		if e == nil {
			continue
		}
		if c.Endpoints == nil {
			c.Endpoints = map[string]*Endpoint{}
		}
		if c.Endpoints[name] == nil {
			c.Endpoints[name] = &Endpoint{}
		}
		c.Endpoints[name].merge(e)
	}
	if src.Vision != nil {
		if c.Vision == nil {
			c.Vision = &Vision{}
		}
		c.Vision.merge(src.Vision)
	}
	if src.Transcription != nil {
		if c.Transcription == nil {
			c.Transcription = &Transcription{}
		}
		c.Transcription.merge(src.Transcription)
	}
	c.Embeddings.merge(&src.Embeddings)
	set(&c.Run.Pending, src.Run.Pending)
	set(&c.Run.BatchBytes, src.Run.BatchBytes)
}

func (e *Endpoint) merge(src *Endpoint) {
	set(&e.BaseURL, src.BaseURL)
	set(&e.Format, src.Format)
	if src.Profile != nil {
		e.Profile = src.Profile
	}
	e.Query = mergeMap(e.Query, src.Query)
	set(&e.ResponseHeaderTimeout, src.ResponseHeaderTimeout)
	set(&e.Retry.MaxAttempts, src.Retry.MaxAttempts)
	set(&e.Retry.BaseDelay, src.Retry.BaseDelay)
	set(&e.Retry.MaxDelay, src.Retry.MaxDelay)
	set(&e.Token, src.Token)
	e.Headers = mergeMap(e.Headers, src.Headers)
}

func (c *Capability) merge(src *Capability) {
	set(&c.Endpoint, src.Endpoint)
	set(&c.Model, src.Model)
	set(&c.Concurrency, src.Concurrency)
}

func (v *Vision) merge(src *Vision) {
	v.Capability.merge(&src.Capability)
	set(&v.Prompt, src.Prompt)
	set(&v.MaxTokens, src.MaxTokens)
	set(&v.Reasoning, src.Reasoning)
}

func (t *Transcription) merge(src *Transcription) {
	t.Capability.merge(&src.Capability)
	set(&t.Prompt, src.Prompt)
	set(&t.Language, src.Language)
	set(&t.Mode, src.Mode)
	set(&t.WordsPerSecond, src.WordsPerSecond)
}

func (e *Embeddings) merge(src *Embeddings) {
	e.Capability.merge(&src.Capability)
	set(&e.Dimensions, src.Dimensions)
	set(&e.Batch, src.Batch)
	set(&e.ChunkSize, src.ChunkSize)
}

func set[T comparable](dst *T, src T) {
	var zero T
	if src != zero {
		*dst = src
	}
}

func mergeMap(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]string{}
	}
	maps.Copy(dst, src)
	return dst
}

// Finalize applies the overrides prefix composes, gives each setting still unset its starting
// value, and checks what is docinfer's to check: each endpoint's format and profile and a
// response_header_timeout not below zero, each capability's endpoint among endpoints, each
// count at least 1, chat transcription on the completions format, and no two endpoint names
// composing the same overrides. What ai.New refuses, such as a base URL or a retry policy, the
// client nodes report, named for their endpoint. An empty prefix reads no override.
func (c *Config) Finalize(prefix string) error {
	overrides, err := c.overrides(prefix)
	c.Merge(overrides)
	c.start()
	return errors.Join(err, c.validate())
}

func (c *Config) overrides(prefix string) (*Config, error) {
	o := &Config{Endpoints: map[string]*Endpoint{}}
	var errs []error
	owners := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(c.Endpoints)) {
		path := func(key ...string) string {
			return config.EnvName(prefix, slices.Concat([]string{"endpoints", name}, key)...)
		}
		if stem := path(); stem != "" {
			if other, ok := owners[stem]; ok {
				errs = append(errs, fmt.Errorf("endpoints %q and %q compose the same overrides, %s_*", other, name, stem))
				continue
			}
			owners[stem] = name
		}
		e := &Endpoint{}
		errs = append(errs,
			setFromEnv(&e.BaseURL, path("base_url"), parseString),
			setFromEnv(&e.Format, path("format"), parseString),
			setFromEnv(&e.Profile, path("profile"), parseRaw),
			setFromEnv(&e.Query, path("query"), parseMap),
			config.SetDurationFromEnv(&e.ResponseHeaderTimeout, path("response_header_timeout")),
			config.SetFromEnv(&e.Retry.MaxAttempts, path("retry", "max_attempts"), parseCount),
			config.SetDurationFromEnv(&e.Retry.BaseDelay, path("retry", "base_delay")),
			config.SetDurationFromEnv(&e.Retry.MaxDelay, path("retry", "max_delay")),
			setFromEnv(&e.Token, path("token"), parseString),
			setFromEnv(&e.Headers, path("headers"), parseMap),
		)
		o.Endpoints[name] = e
	}

	var v Vision
	errs = append(errs, capabilityOverrides(prefix, "vision", &v.Capability),
		setFromEnv(&v.Prompt, config.EnvName(prefix, "vision", "prompt"), parseString),
		config.SetFromEnv(&v.MaxTokens, config.EnvName(prefix, "vision", "max_tokens"), parseCount),
		setFromEnv(&v.Reasoning, config.EnvName(prefix, "vision", "reasoning"), parseEffort),
	)
	if v != (Vision{}) {
		o.Vision = &v
	}

	var t Transcription
	errs = append(errs, capabilityOverrides(prefix, "transcription", &t.Capability),
		setFromEnv(&t.Prompt, config.EnvName(prefix, "transcription", "prompt"), parseString),
		setFromEnv(&t.Language, config.EnvName(prefix, "transcription", "language"), parseString),
		config.SetFromEnv(&t.Mode, config.EnvName(prefix, "transcription", "mode"), parseMode),
		config.SetFromEnv(&t.WordsPerSecond, config.EnvName(prefix, "transcription", "words_per_second"), parseCount),
	)
	if t != (Transcription{}) {
		o.Transcription = &t
	}

	errs = append(errs, capabilityOverrides(prefix, "embeddings", &o.Embeddings.Capability),
		config.SetFromEnv(&o.Embeddings.Dimensions, config.EnvName(prefix, "embeddings", "dimensions"), parseCount),
		config.SetFromEnv(&o.Embeddings.Batch, config.EnvName(prefix, "embeddings", "batch"), parseCount),
		config.SetFromEnv(&o.Embeddings.ChunkSize, config.EnvName(prefix, "embeddings", "chunk_size"), parseCount),
		config.SetFromEnv(&o.Run.Pending, config.EnvName(prefix, "run", "pending"), parseCount),
		config.SetFromEnv(&o.Run.BatchBytes, config.EnvName(prefix, "run", "batch_bytes"), parseCount),
	)
	return o, errors.Join(errs...)
}

func capabilityOverrides(prefix, name string, c *Capability) error {
	return errors.Join(
		setFromEnv(&c.Endpoint, config.EnvName(prefix, name, "endpoint"), parseString),
		setFromEnv(&c.Model, config.EnvName(prefix, name, "model"), parseString),
		config.SetFromEnv(&c.Concurrency, config.EnvName(prefix, name, "concurrency"), parseCount),
	)
}

func setFromEnv[T any](dst *T, name string, parse func(string) (T, error)) error {
	var v *T
	err := config.SetFromEnv(&v, name, parse)
	if v != nil {
		*dst = *v
	}
	return err
}

func parseString(s string) (string, error) { return s, nil }

func parseRaw(s string) (json.RawMessage, error) { return json.RawMessage(s), nil }

func parseCount(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a whole number", s)
	}
	return n, nil
}

func parseMap(s string) (map[string]string, error) {
	var m map[string]string
	if err := jsonv2.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("not a JSON object of strings: %w", err)
	}
	return m, nil
}

func parseEffort(s string) (protocol.ReasoningEffort, error) {
	var e protocol.ReasoningEffort
	err := e.UnmarshalText([]byte(s))
	return e, err
}

func parseMode(s string) (ai.TranscriptionMode, error) {
	var m ai.TranscriptionMode
	err := m.UnmarshalText([]byte(s))
	return m, err
}

func (c *Config) start() {
	for _, e := range c.Endpoints {
		startAt(&e.ResponseHeaderTimeout, config.Duration(startResponseHeaderTimeout))
		startAt(&e.Retry.MaxAttempts, startRetryAttempts)
		startAt(&e.Retry.BaseDelay, config.Duration(startRetryBaseDelay))
		startAt(&e.Retry.MaxDelay, config.Duration(startRetryMaxDelay))
	}
	if v := c.Vision; v != nil {
		startAt(&v.Concurrency, startConcurrency)
		startAt(&v.MaxTokens, startVisionMaxTokens)
		if v.Prompt == "" {
			v.Prompt = startVisionPrompt
		}
		if v.Reasoning == "" {
			v.Reasoning = startVisionReasoning
		}
	}
	if t := c.Transcription; t != nil {
		startAt(&t.Concurrency, startConcurrency)
		startAt(&t.Mode, ai.TranscribeRoute)
		startAt(&t.WordsPerSecond, startWordsPerSecond)
	}
	startAt(&c.Embeddings.Concurrency, startConcurrency)
	startAt(&c.Embeddings.Batch, startEmbeddingsBatch)
	startAt(&c.Embeddings.ChunkSize, startChunkSize)
	startAt(&c.Run.Pending, startPending)
	startAt(&c.Run.BatchBytes, startBatchBytes)
}

func startAt[T any](p **T, v T) {
	if *p == nil {
		*p = &v
	}
}

func (c *Config) validate() error {
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(c.Endpoints)) {
		e := c.Endpoints[name]
		if _, err := e.wireFormat(); err != nil {
			errs = append(errs, fmt.Errorf("endpoints %q: %w", name, err))
		}
		if d := e.ResponseHeaderTimeout.Duration(); d < 0 {
			errs = append(errs, fmt.Errorf("endpoints %q: response_header_timeout %s is below zero", name, d))
		}
	}
	if v := c.Vision; v != nil {
		errs = append(errs, c.checkCapability("vision", v.Capability),
			atLeastOne("vision", "max_tokens", v.MaxTokens))
	}
	if t := c.Transcription; t != nil {
		errs = append(errs, c.checkCapability("transcription", t.Capability),
			atLeastOne("transcription", "words_per_second", t.WordsPerSecond))
		if e := c.Endpoints[t.Endpoint]; *t.Mode == ai.TranscribeChat && e != nil && e.Format != formatCompletions {
			errs = append(errs, fmt.Errorf("transcription: mode chat needs the %s format, and endpoint %q's is %q",
				formatCompletions, t.Endpoint, e.Format))
		}
	}
	errs = append(errs, c.checkCapability("embeddings", c.Embeddings.Capability),
		atLeastOne("embeddings", "dimensions", c.Embeddings.Dimensions),
		atLeastOne("embeddings", "batch", c.Embeddings.Batch),
		atLeastOne("embeddings", "chunk_size", c.Embeddings.ChunkSize),
		atLeastOne("run", "pending", c.Run.Pending),
		atLeastOne("run", "batch_bytes", c.Run.BatchBytes))
	return errors.Join(errs...)
}

func (c *Config) checkCapability(name string, capability Capability) error {
	var errs []error
	switch {
	case capability.Endpoint == "":
		errs = append(errs, fmt.Errorf("%s: no endpoint", name))
	case c.Endpoints[capability.Endpoint] == nil:
		errs = append(errs, fmt.Errorf("%s: endpoint %q is not one of endpoints", name, capability.Endpoint))
	}
	return errors.Join(append(errs, atLeastOne(name, "concurrency", capability.Concurrency))...)
}

func atLeastOne(section, key string, v *int) error {
	switch {
	case v == nil:
		return fmt.Errorf("%s: no %s", section, key)
	case *v < 1:
		return fmt.Errorf("%s: %s %d is below 1", section, key, *v)
	}
	return nil
}

const formatCompletions = "completions"

// Adapted from go-ai tools/infer (72f1b98): an endpoint's profile, read from its configuration.
type formatOptions struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

var formats = map[string]func(opts formatOptions) format.Format{
	formatCompletions: func(opts formatOptions) format.Format { return completions.New(opts.Completions) },
	"messages":        func(opts formatOptions) format.Format { return messages.New(opts.Messages) },
	"responses":       func(opts formatOptions) format.Format { return responses.New(opts.Responses) },
}

func (e *Endpoint) wireFormat() (format.Format, error) {
	build, ok := formats[e.Format]
	if !ok {
		return nil, fmt.Errorf("format %q is not completions, messages, or responses", e.Format)
	}
	var opts formatOptions
	if len(e.Profile) > 0 {
		if err := jsonv2.Unmarshal(e.Profile, &opts, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, fmt.Errorf("profile: %w", err)
		}
	}
	return build(opts), nil
}

func (e *Endpoint) options() ai.Options {
	opts := ai.Options{Retry: ai.RetryPolicy{
		MaxAttempts: *e.Retry.MaxAttempts,
		BaseDelay:   e.Retry.BaseDelay.Duration(),
		MaxDelay:    e.Retry.MaxDelay.Duration(),
	}}
	if len(e.Query) > 0 {
		opts.Query = url.Values{}
		for k, v := range e.Query {
			opts.Query.Set(k, v)
		}
	}
	if token := e.Token; token != "" {
		opts.Token = func(context.Context) (string, error) { return token, nil }
	}
	if len(e.Headers) > 0 {
		opts.Header = http.Header{}
		for k, v := range e.Headers {
			opts.Header.Add(k, v)
		}
	}
	return opts
}

func loadConfig(dir string) (Config, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return Config{}, err
	}
	if !info.IsDir() {
		return Config{}, fmt.Errorf("%s is no directory", dir)
	}
	c, err := config.Load[Config](config.Options{Dir: dir, EnvPrefix: envPrefix})
	if err != nil {
		return Config{}, err
	}
	return *c, nil
}
