package app

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
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
		return newClient(s, n, v.Endpoint, ai.Options{})
	})
	n.TranscriptionClient = g.Define("transcription client", func(s *graph.Scope) (*ai.Client, error) {
		t := s.Use(n.Config).Transcription
		if t == nil {
			return nil, nil
		}
		return newClient(s, n, t.Endpoint, ai.Options{TranscriptionMode: *t.Mode})
	})
	n.EmbeddingsClient = g.Define("embeddings client", func(s *graph.Scope) (*ai.Client, error) {
		e := s.Use(n.Config).Embeddings
		return newClient(s, n, e.Endpoint, ai.Options{Embedding: ai.EmbeddingProfile{InputTypes: e.InputTypes}})
	})
	n.TokenizerClient = g.Define("tokenizer client", func(s *graph.Scope) (*ai.Client, error) {
		e := s.Use(n.Config).Embeddings
		return newClient(s, n, e.Tokenizer, ai.Options{Embedding: ai.EmbeddingProfile{InputTypes: e.InputTypes}})
	})
}

func newLifecycleConfig(*graph.Scope) (lifecycle.Config, error) {
	var cfg lifecycle.Config
	err := cfg.Finalize(envPrefix)
	return cfg, err
}

func newClient(s *graph.Scope, n *Nodes, endpoint string, capability ai.Options) (*ai.Client, error) {
	e := s.Use(n.Config).Endpoints[endpoint]
	wireFormat, err := e.wireFormat()
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", endpoint, err)
	}
	opts := e.options()
	opts.TranscriptionMode = capability.TranscriptionMode
	opts.Embedding = capability.Embedding
	client, err := ai.New(e.BaseURL, s.Use(n.HTTPClients)[endpoint], wireFormat, opts)
	if err != nil {
		return nil, fmt.Errorf("endpoint %q: %w", endpoint, err)
	}
	return client, nil
}

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
// its requests name there, the most requests it keeps in flight, its concurrency, starting at 4,
// and served_model, the model the deployment serves under model, which keys the capability's
// results. The run takes the served model from the endpoint's model listing, its entry's root,
// as vLLM lists it, and served_model is for an endpoint whose listing gives none; it has no
// starting value, and a served_model the listing's root disagrees with stops the run.
type Capability struct {
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	ServedModel string `json:"served_model"`
	Concurrency *int   `json:"concurrency"`
}

// Vision is the vision capability: what each image is asked, prompt, the cap on its reply,
// max_tokens, starting at 2048, the reasoning effort, any go-ai's protocol.ReasoningEffort
// accepts, starting at none, native, a JSON object of members the endpoint defines, sent in every
// request, starting at Gemma 4's card sampling, {"temperature":1.0,"top_p":0.95,"top_k":64}; a
// configured native replaces it whole, and {} sends none; and max_pixels, the most pixels an image
// is sent at, a larger PNG or JPEG downscaled to it with its aspect ratio kept, starting at
// 2,580,480, the vision row's Gemma 4 budget at 1,120 soft tokens (1,120 × 9 patches × 16² px),
// 0 sending every image as it is.
type Vision struct {
	Capability
	Prompt    string                   `json:"prompt"`
	MaxTokens *int                     `json:"max_tokens"`
	Reasoning protocol.ReasoningEffort `json:"reasoning"`
	Native    json.RawMessage          `json:"native"`
	MaxPixels *int                     `json:"max_pixels"`
}

// Transcription is the transcription capability: its prompt and language, empty sending none;
// its mode, route or chat, starting at route, which its own client transcribes in, chat needing
// its endpoint's format to be completions; max_tokens, the most tokens a chat-mode reply takes,
// starting at 512 in chat mode and at 0, sending none, in route mode, which refuses any other;
// native, a JSON object of members the endpoint defines, sent in every chat-mode request,
// starting at {"temperature":0} in chat mode and at none in route mode, which refuses any other; a
// configured native replaces it whole, and {} sends none; and the words a second of speech holds
// at most, words_per_second, starting at 8.
type Transcription struct {
	Capability
	Prompt         string                `json:"prompt"`
	Language       string                `json:"language"`
	Mode           *ai.TranscriptionMode `json:"mode"`
	MaxTokens      *int                  `json:"max_tokens"`
	Native         json.RawMessage       `json:"native"`
	WordsPerSecond *int                  `json:"words_per_second"`
}

// Embeddings is the embeddings capability: the embedding space's dimensions, which have no
// starting value; input_types, the model's input-type conventions, go-ai's EmbeddingProfile's
// input_types, such as multilingual-e5's {"document":{"prefix":"passage: "},"query":{"prefix":
// "query: "}}, required and with no starting value, {} declaring none; tokenizer, the endpoint,
// by its key in endpoints, whose tokenize route counts a chunk's tokens, required: through a
// gateway the embeddings' own endpoint, straight to vLLM one whose base_url is the server's root,
// such as http://framework-2:8003; the texts one request carries, batch, starting at 16; the
// most tokens a chunk holds as the model's tokenizer counts it, its document prefix included,
// chunk_size, starting at 480; and the most characters, Unicode code points, a chunk holds, its
// document prefix included, chunk_runes, starting at 8192, the length vLLM refuses an input over
// at a max_model_len of 512, 16 characters a token, before tokenizing it. The chunker's own bound
// is chunk_runes less the document prefix's characters, which must leave it at least 1. The
// tokenizer must front the same deployment as the embeddings' endpoint, which no model listing
// checks: the chunker's identity names the embeddings' served model, and a vLLM server's root has
// no model listing.
type Embeddings struct {
	Capability
	Dimensions *int                                `json:"dimensions"`
	InputTypes map[ai.InputType]ai.InputConvention `json:"input_types"`
	Tokenizer  string                              `json:"tokenizer"`
	Batch      *int                                `json:"batch"`
	ChunkSize  *int                                `json:"chunk_size"`
	ChunkRunes *int                                `json:"chunk_runes"`
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
	startVisionNative          = `{"temperature":1.0,"top_p":0.95,"top_k":64}`
	startVisionMaxPixels       = 2580480
	startTranscriptionTokens   = 512
	startTranscriptionNative   = `{"temperature":0}`
	startWordsPerSecond        = 8
	startEmbeddingsBatch       = 16
	startChunkSize             = 480
	startChunkRunes            = 8192
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
	set(&c.ServedModel, src.ServedModel)
	set(&c.Concurrency, src.Concurrency)
}

func (v *Vision) merge(src *Vision) {
	v.Capability.merge(&src.Capability)
	set(&v.Prompt, src.Prompt)
	set(&v.MaxTokens, src.MaxTokens)
	set(&v.Reasoning, src.Reasoning)
	if src.Native != nil {
		v.Native = src.Native
	}
	set(&v.MaxPixels, src.MaxPixels)
}

func (t *Transcription) merge(src *Transcription) {
	t.Capability.merge(&src.Capability)
	set(&t.Prompt, src.Prompt)
	set(&t.Language, src.Language)
	set(&t.Mode, src.Mode)
	set(&t.MaxTokens, src.MaxTokens)
	if src.Native != nil {
		t.Native = src.Native
	}
	set(&t.WordsPerSecond, src.WordsPerSecond)
}

func (e *Embeddings) merge(src *Embeddings) {
	e.Capability.merge(&src.Capability)
	set(&e.Dimensions, src.Dimensions)
	if src.InputTypes != nil {
		if e.InputTypes == nil {
			e.InputTypes = map[ai.InputType]ai.InputConvention{}
		}
		maps.Copy(e.InputTypes, src.InputTypes)
	}
	set(&e.Tokenizer, src.Tokenizer)
	set(&e.Batch, src.Batch)
	set(&e.ChunkSize, src.ChunkSize)
	set(&e.ChunkRunes, src.ChunkRunes)
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
// response_header_timeout not below zero, each capability's endpoint among endpoints, each count at
// least 1, chunk_runes over the document prefix's characters, chat transcription on the completions
// format, transcription's max_tokens not below zero and 0 in route mode, vision's max_pixels not
// below zero, the embeddings' input_types set and their tokenizer among endpoints, vision's and
// transcription's native a JSON object with unique member names, transcription's none or {} in
// route mode, and no two endpoint names composing the same overrides. What ai.New refuses, such as
// a base URL or a retry policy, the client nodes report, named for their endpoint. An empty prefix
// reads no override.
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
		setFromEnv(&v.Native, config.EnvName(prefix, "vision", "native"), parseRaw),
		config.SetFromEnv(&v.MaxPixels, config.EnvName(prefix, "vision", "max_pixels"), parseCount),
	)
	if !reflect.ValueOf(v).IsZero() {
		o.Vision = &v
	}

	var t Transcription
	errs = append(errs, capabilityOverrides(prefix, "transcription", &t.Capability),
		setFromEnv(&t.Prompt, config.EnvName(prefix, "transcription", "prompt"), parseString),
		setFromEnv(&t.Language, config.EnvName(prefix, "transcription", "language"), parseString),
		config.SetFromEnv(&t.Mode, config.EnvName(prefix, "transcription", "mode"), parseMode),
		config.SetFromEnv(&t.MaxTokens, config.EnvName(prefix, "transcription", "max_tokens"), parseCount),
		setFromEnv(&t.Native, config.EnvName(prefix, "transcription", "native"), parseRaw),
		config.SetFromEnv(&t.WordsPerSecond, config.EnvName(prefix, "transcription", "words_per_second"), parseCount),
	)
	if !reflect.ValueOf(t).IsZero() {
		o.Transcription = &t
	}

	errs = append(errs, capabilityOverrides(prefix, "embeddings", &o.Embeddings.Capability),
		config.SetFromEnv(&o.Embeddings.Dimensions, config.EnvName(prefix, "embeddings", "dimensions"), parseCount),
		setFromEnv(&o.Embeddings.InputTypes, config.EnvName(prefix, "embeddings", "input_types"), parseInputTypes),
		setFromEnv(&o.Embeddings.Tokenizer, config.EnvName(prefix, "embeddings", "tokenizer"), parseString),
		config.SetFromEnv(&o.Embeddings.Batch, config.EnvName(prefix, "embeddings", "batch"), parseCount),
		config.SetFromEnv(&o.Embeddings.ChunkSize, config.EnvName(prefix, "embeddings", "chunk_size"), parseCount),
		config.SetFromEnv(&o.Embeddings.ChunkRunes, config.EnvName(prefix, "embeddings", "chunk_runes"), parseCount),
		config.SetFromEnv(&o.Run.Pending, config.EnvName(prefix, "run", "pending"), parseCount),
		config.SetFromEnv(&o.Run.BatchBytes, config.EnvName(prefix, "run", "batch_bytes"), parseCount),
	)
	return o, errors.Join(errs...)
}

func capabilityOverrides(prefix, name string, c *Capability) error {
	return errors.Join(
		setFromEnv(&c.Endpoint, config.EnvName(prefix, name, "endpoint"), parseString),
		setFromEnv(&c.Model, config.EnvName(prefix, name, "model"), parseString),
		setFromEnv(&c.ServedModel, config.EnvName(prefix, name, "served_model"), parseString),
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

func parseInputTypes(s string) (map[ai.InputType]ai.InputConvention, error) {
	var m map[ai.InputType]ai.InputConvention
	if err := jsonv2.Unmarshal([]byte(s), &m, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("not a JSON object of input-type conventions: %w", err)
	}
	if m == nil {
		return nil, fmt.Errorf("%q is no JSON object of input-type conventions", s)
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
		startAt(&v.MaxPixels, startVisionMaxPixels)
		if v.Prompt == "" {
			v.Prompt = startVisionPrompt
		}
		if v.Reasoning == "" {
			v.Reasoning = startVisionReasoning
		}
		if v.Native == nil {
			v.Native = json.RawMessage(startVisionNative)
		}
	}
	if t := c.Transcription; t != nil {
		startAt(&t.Concurrency, startConcurrency)
		startAt(&t.Mode, ai.TranscribeRoute)
		if *t.Mode == ai.TranscribeChat {
			startAt(&t.MaxTokens, startTranscriptionTokens)
			if t.Native == nil {
				t.Native = json.RawMessage(startTranscriptionNative)
			}
		}
		startAt(&t.MaxTokens, 0)
		startAt(&t.WordsPerSecond, startWordsPerSecond)
	}
	startAt(&c.Embeddings.Concurrency, startConcurrency)
	startAt(&c.Embeddings.Batch, startEmbeddingsBatch)
	startAt(&c.Embeddings.ChunkSize, startChunkSize)
	startAt(&c.Embeddings.ChunkRunes, startChunkRunes)
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
			atLeastOne("vision", "max_tokens", v.MaxTokens),
			checkNative("vision", v.Native))
		if n := *v.MaxPixels; n < 0 {
			errs = append(errs, fmt.Errorf("vision: max_pixels %d is below zero", n))
		}
	}
	if t := c.Transcription; t != nil {
		errs = append(errs, c.checkCapability("transcription", t.Capability),
			atLeastOne("transcription", "words_per_second", t.WordsPerSecond))
		if e := c.Endpoints[t.Endpoint]; *t.Mode == ai.TranscribeChat && e != nil && e.Format != formatCompletions {
			errs = append(errs, fmt.Errorf("transcription: mode chat needs the %s format, and endpoint %q's is %q",
				formatCompletions, t.Endpoint, e.Format))
		}
		switch n := *t.MaxTokens; {
		case n < 0:
			errs = append(errs, fmt.Errorf("transcription: max_tokens %d is below zero", n))
		case n > 0 && *t.Mode == ai.TranscribeRoute:
			errs = append(errs, fmt.Errorf("transcription: max_tokens %d bounds a chat-mode reply, and mode is route, "+
				"which has no field for it: set max_tokens to 0, or mode to chat", n))
		}
		if err := checkNative("transcription", t.Native); err != nil {
			errs = append(errs, err)
		} else if *t.Mode == ai.TranscribeRoute && len(native(t.Native)) > 0 {
			errs = append(errs, fmt.Errorf("transcription: native %s is sent in a chat-mode request, and mode is route, "+
				"which sends no JSON body: set native to {}, or mode to chat", t.Native))
		}
	}
	errs = append(errs, c.checkCapability("embeddings", c.Embeddings.Capability),
		atLeastOne("embeddings", "dimensions", c.Embeddings.Dimensions),
		c.checkEmbeddings(),
		atLeastOne("embeddings", "batch", c.Embeddings.Batch),
		atLeastOne("embeddings", "chunk_size", c.Embeddings.ChunkSize),
		c.checkChunkRunes(),
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

func (c *Config) checkEmbeddings() error {
	var errs []error
	if c.Embeddings.InputTypes == nil {
		errs = append(errs, errors.New(`embeddings: no input_types: declare the model's input-type conventions, such as {"document":{"prefix":"passage: "},"query":{"prefix":"query: "}}, or {} for none`))
	}
	switch t := c.Embeddings.Tokenizer; {
	case t == "":
		errs = append(errs, errors.New("embeddings: no tokenizer: name the endpoint whose tokenize route counts the model's tokens"))
	case c.Endpoints[t] == nil:
		errs = append(errs, fmt.Errorf("embeddings: tokenizer %q is not one of endpoints", t))
	}
	return errors.Join(errs...)
}

func (c *Config) checkChunkRunes() error {
	if err := atLeastOne("embeddings", "chunk_runes", c.Embeddings.ChunkRunes); err != nil {
		return err
	}
	if prefix := c.Embeddings.prefixRunes(); *c.Embeddings.ChunkRunes <= prefix {
		return fmt.Errorf("embeddings: chunk_runes %d leaves no character beside the document prefix's %d",
			*c.Embeddings.ChunkRunes, prefix)
	}
	return nil
}

func (e *Embeddings) prefixRunes() int {
	return utf8.RuneCountInString(e.InputTypes[ai.InputDocument].Prefix)
}

func (e *Embeddings) chunkRunes() int { return *e.ChunkRunes - e.prefixRunes() }

func checkNative(section string, native json.RawMessage) error {
	if native == nil {
		return nil
	}
	var members map[string]jsontext.Value
	if err := jsonv2.Unmarshal(native, &members); err != nil || members == nil {
		return fmt.Errorf("%s: native %s is not a JSON object with unique member names", section, native)
	}
	return nil
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
