package docinfer

import (
	"fmt"
	"time"

	"github.com/standards-lab/docinfer/chunk"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/go-ai"
)

// Config is a [Pipeline]'s configuration. Nothing in it has a default: [New] refuses a count or
// bound below 1 and an empty model or served model, and panics on a missing client, cache, or
// Bounds.
type Config struct {
	Vision        *Vision        // nil skips images, counted
	Transcription *Transcription // nil skips windows, counted
	Extraction    *Extraction    // nil: a run that asks for extraction fails, permanent
	Embeddings    Embeddings     // required
	Chunker       chunk.Splitter // its Bound and Runes at least 1, its Count set, its Model not empty
	// Dimensions is the embedding space's, at least 1: a vector of another length, made or held,
	// fails the job, permanent.
	Dimensions int
	Cache      Cache   // required
	Bounds     *Bounds // required: the requests in flight per capability, shared
	Pending    int     // at least 1: a job's inputs admitted and not yet finished
	EmbedBatch int     // at least 1: texts per Embed call
	BatchBytes int     // at least 1: a batch's marshalled JSON's cap
	Join       join.Request
}

// Pipeline infers over jobs' inputs with one configuration. It is built once by [New] and runs
// many jobs, and is safe for concurrent use.
type Pipeline struct {
	cfg     Config
	profile Profile
	claims  claims
	now     func() time.Time
}

// New returns a Pipeline of cfg. It panics, naming the field, on a nil Cache, Bounds, or
// Embeddings.Client, or a set Vision, Transcription, or Extraction whose Client is nil: each is a
// wiring defect. It refuses, matching [ErrRequest], a count or bound below 1 (Chunker.Bound,
// Chunker.Runes, Dimensions, Pending, EmbedBatch, BatchBytes, Join.WordsPerSecond, and each
// MaxTokens but Transcription's), a negative Transcription.MaxTokens, a nil Chunker.Count, an
// empty Model, Served, or Chunker.Model, and a Vision, Transcription, or Extraction Native that
// isn't a JSON object with unique member names. It copies the capabilities cfg points to, so a later change to
// them changes nothing.
func New(cfg Config) (*Pipeline, error) {
	if cfg.Cache == nil {
		panic("docinfer: New: nil Cache")
	}
	if cfg.Bounds == nil {
		panic("docinfer: New: nil Bounds")
	}
	if cfg.Embeddings.Client == nil {
		panic("docinfer: New: nil Embeddings.Client")
	}
	if cfg.Vision != nil && cfg.Vision.Client == nil {
		panic("docinfer: New: nil Vision.Client")
	}
	if cfg.Transcription != nil && cfg.Transcription.Client == nil {
		panic("docinfer: New: nil Transcription.Client")
	}
	if cfg.Extraction != nil && cfg.Extraction.Client == nil {
		panic("docinfer: New: nil Extraction.Client")
	}

	type count struct {
		field string
		n     int
	}
	counts := []count{
		{"Chunker.Bound", cfg.Chunker.Bound},
		{"Chunker.Runes", cfg.Chunker.Runes},
		{"Dimensions", cfg.Dimensions},
		{"Pending", cfg.Pending},
		{"EmbedBatch", cfg.EmbedBatch},
		{"BatchBytes", cfg.BatchBytes},
		{"Join.WordsPerSecond", cfg.Join.WordsPerSecond},
	}
	type model struct{ field, model string }
	models := []model{
		{"Embeddings.Model", cfg.Embeddings.Model}, {"Embeddings.Served", cfg.Embeddings.Served},
		{"Chunker.Model", cfg.Chunker.Model},
	}
	if v := cfg.Vision; v != nil {
		counts = append(counts, count{"Vision.MaxTokens", v.MaxTokens})
		models = append(models, model{"Vision.Model", v.Model}, model{"Vision.Served", v.Served})
		cfg.Vision = new(*v)
		cfg.Vision.Native = v.Native.Clone()
	}
	if t := cfg.Transcription; t != nil {
		models = append(models, model{"Transcription.Model", t.Model}, model{"Transcription.Served", t.Served})
		cfg.Transcription = new(*t)
		cfg.Transcription.Native = t.Native.Clone()
	}
	if x := cfg.Extraction; x != nil {
		counts = append(counts, count{"Extraction.MaxTokens", x.MaxTokens})
		models = append(models, model{"Extraction.Model", x.Model}, model{"Extraction.Served", x.Served})
		cfg.Extraction = new(*x)
		cfg.Extraction.Native = x.Native.Clone()
	}
	for _, c := range counts {
		if c.n < 1 {
			return nil, fmt.Errorf("docinfer: New: %w: %s %d is below 1", ErrRequest, c.field, c.n)
		}
	}
	if t := cfg.Transcription; t != nil && t.MaxTokens < 0 {
		return nil, fmt.Errorf("docinfer: New: %w: Transcription.MaxTokens %d is negative", ErrRequest, t.MaxTokens)
	}
	if cfg.Chunker.Count == nil {
		return nil, fmt.Errorf("docinfer: New: %w: Chunker.Count is nil", ErrRequest)
	}
	for _, m := range models {
		if m.model == "" {
			return nil, fmt.Errorf("docinfer: New: %w: %s is empty", ErrRequest, m.field)
		}
	}
	if v := cfg.Vision; v != nil {
		if err := probeNative("Vision.Native", v.Native); err != nil {
			return nil, err
		}
	}
	if t := cfg.Transcription; t != nil {
		if err := probeNative("Transcription.Native", t.Native); err != nil {
			return nil, err
		}
	}
	if x := cfg.Extraction; x != nil {
		if err := probeNative("Extraction.Native", x.Native); err != nil {
			return nil, err
		}
	}

	p := &Pipeline{cfg: cfg, now: time.Now}
	p.profile = Profile{
		Embeddings:     cfg.Embeddings.Identity(),
		EmbeddingQuery: cfg.Embeddings.query(),
		Dimensions:     cfg.Dimensions,
		Chunker:        cfg.Chunker.Settings(),
	}
	if cfg.Vision != nil {
		p.profile.Vision = new(cfg.Vision.Identity())
	}
	if cfg.Transcription != nil {
		p.profile.Transcription = new(cfg.Transcription.Identity())
	}
	if cfg.Extraction != nil {
		p.profile.Extraction = new(cfg.Extraction.profile())
	}
	return p, nil
}

// Profile returns the pipeline's resolved identity: each capability's [Identity], the query
// convention the embeddings' client declares, the embeddings' Dimensions, and the Chunker's
// Settings, known before any job runs. Each identity's Model is the capability's Served, the model
// the deployment serves, not the one its request names, so a consumer recording the embedding space
// records the model. An unset capability's identity is nil, and the extraction's settings are
// {"request","max_tokens", "effort","native"}, "native" there only when Native is set, less the
// name, prompt, and schema each run's [Extract] gives.
func (p *Pipeline) Profile() Profile {
	pr := p.profile
	for _, id := range []**Identity{&pr.Vision, &pr.Transcription, &pr.Extraction} {
		if *id != nil {
			*id = new(**id)
		}
	}
	if q := pr.EmbeddingQuery; q != nil {
		pr.EmbeddingQuery = &ai.InputConvention{Prefix: q.Prefix, Native: q.Native.Clone()}
	}
	return pr
}
