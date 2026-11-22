package report

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/join"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/outdir"
)

const toRoot = "../../.."

type model struct {
	Manifest outdir.Manifest
	Ended    bool
	Inputs   []*input
	Contents []*content
}

func (m *model) Span() schema.Span { return schema.Span{Start: m.Manifest.Start, End: m.Manifest.End} }

type input struct {
	Path        string
	SHA256      string
	Error       string
	Content     *content
	Images      int
	Transcripts int
	Chunks      int
	Failures    int
}

type content struct {
	SHA256      string
	Paths       []string
	Docproc     string
	Images      []*image
	Transcripts []*transcript
	Sources     []*source
	seen        map[string]bool
	sources     map[string]*source
}

type text struct {
	SHA256  string
	Body    string
	Missing bool
	Damaged string
}

type image struct {
	Part    string
	SHA256  string
	Href    string
	Outcome docinfer.Outcome
	Reason  string
	Held    bool
	Text    *text
}

type transcript struct {
	Part    string
	Outcome docinfer.Outcome
	Held    bool
	Windows []window
	Failed  []int
	Seams   []join.Seam
	Text    *text
}

type window struct {
	Index  int
	SHA256 string
	Href   string
	Failed bool
}

type source struct {
	Part       dispatch.Part
	Kind       string
	SHA256     string
	Incomplete string
	Output     *dispatch.Output
	Href       string
	Chunks     []outdir.Line
	indexes    map[int]bool
}

type builder struct {
	ctx     context.Context
	d       *outdir.Dir
	docproc string
	inputs  map[string]*input
	content map[string]*content
	texts   map[string]*text
	m       *model
}

func build(ctx context.Context, d *outdir.Dir, id string) (*model, error) {
	man, err := d.Manifest(id)
	if err != nil {
		return nil, err
	}
	var lines []outdir.Line
	for l, err := range d.Lines(id) {
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	var infs []outdir.Inference
	for i, err := range d.Inferences(id) {
		if err != nil {
			return nil, err
		}
		infs = append(infs, i)
	}
	m := &model{Manifest: man, Ended: !man.End.IsZero()}
	b := &builder{
		ctx:     ctx,
		d:       d,
		docproc: path.Join(toRoot, man.Docproc.Dir),
		inputs:  map[string]*input{},
		content: map[string]*content{},
		texts:   map[string]*text{},
		m:       m,
	}
	for _, in := range man.Inputs {
		b.input(in.Path, in.SHA256, in.Error)
	}
	for _, l := range lines {
		if l.Part.Depth == 0 && l.Part.ID == "/" {
			b.input(l.Input, l.Part.SHA256, "")
		}
	}
	for _, inf := range infs {
		if err := b.inference(inf); err != nil {
			return nil, err
		}
	}
	for _, l := range lines {
		b.line(l)
	}
	for _, p := range slices.Sorted(maps.Keys(b.inputs)) {
		m.Inputs = append(m.Inputs, b.inputs[p])
	}
	for _, sha := range slices.Sorted(maps.Keys(b.content)) {
		c := b.content[sha]
		slices.Sort(c.Paths)
		for _, s := range c.Sources {
			slices.SortStableFunc(s.Chunks, func(a, b outdir.Line) int { return cmp.Compare(a.Index, b.Index) })
		}
		m.Contents = append(m.Contents, c)
	}
	return m, nil
}

func (b *builder) input(p, sha, errText string) {
	in := b.inputs[p]
	if in == nil {
		in = &input{Path: p}
		b.inputs[p] = in
	}
	if errText != "" {
		in.Error = errText
	}
	if in.SHA256 != "" || dispatch.ContentFile(sha) == "" {
		return
	}
	in.SHA256 = sha
	c := b.content[sha]
	if c == nil {
		c = &content{
			SHA256:  sha,
			Docproc: href(path.Join(b.docproc, "runs", b.m.Manifest.Docproc.Run, "report", sha+".html")),
			seen:    map[string]bool{},
			sources: map[string]*source{},
		}
		b.content[sha] = c
	}
	c.Paths = append(c.Paths, p)
	in.Content = c
}

func (b *builder) named(p string) *input {
	if p == "" {
		return nil
	}
	if b.inputs[p] == nil {
		b.input(p, "", "")
	}
	return b.inputs[p]
}

func (b *builder) inference(inf outdir.Inference) error {
	in := b.named(inf.Input)
	if in == nil {
		return nil
	}
	switch inf.Capability {
	case docinfer.CapabilityVision:
		in.Images++
		if inf.Outcome == docinfer.OutcomeRefused || inf.Outcome == docinfer.OutcomeFailed {
			in.Failures++
		}
	case docinfer.CapabilityTranscription:
		in.Transcripts++
		in.Failures += len(inf.Failed)
	}
	c := in.Content
	if c == nil {
		return nil
	}
	key := string(inf.Capability) + "\x00" + inf.Part + "\x00" + strings.Join(inf.ContentSHA256, "\x00")
	if c.seen[key] {
		return nil
	}
	c.seen[key] = true
	t, err := b.text(inf.TextSHA256)
	if err != nil {
		return err
	}
	switch inf.Capability {
	case docinfer.CapabilityVision:
		img := &image{Part: inf.Part, Outcome: inf.Outcome, Reason: inf.Reason, Held: inf.Held, Text: t}
		if len(inf.ContentSHA256) > 0 {
			img.SHA256 = inf.ContentSHA256[0]
			img.Href = b.contentHref(img.SHA256)
		}
		c.Images = append(c.Images, img)
	case docinfer.CapabilityTranscription:
		tr := &transcript{Part: inf.Part, Outcome: inf.Outcome, Held: inf.Held, Failed: inf.Failed, Seams: inf.Seams, Text: t}
		for i, sha := range inf.ContentSHA256 {
			tr.Windows = append(tr.Windows, window{Index: i, SHA256: sha, Href: b.contentHref(sha), Failed: slices.Contains(inf.Failed, i)})
		}
		c.Transcripts = append(c.Transcripts, tr)
	}
	return nil
}

func (b *builder) line(l outdir.Line) {
	in := b.named(l.Input)
	if in == nil {
		return
	}
	in.Chunks++
	if l.Error != "" {
		in.Failures++
	}
	c := in.Content
	if c == nil {
		return
	}
	textSHA := l.Source.SHA256
	if textSHA == "" && l.Source.Output != nil {
		textSHA = l.Source.Output.SHA256
	}
	key := l.Part.ID + "\x00" + l.Part.SHA256 + "\x00" + l.Source.Kind + "\x00" + textSHA
	s := c.sources[key]
	if s == nil {
		s = &source{Part: l.Part, Kind: l.Source.Kind, SHA256: textSHA, Incomplete: l.Source.Incomplete, Output: l.Source.Output, indexes: map[int]bool{}}
		if s.Output != nil {
			s.Href = b.contentHref(s.Output.SHA256)
		}
		c.sources[key] = s
		c.Sources = append(c.Sources, s)
	}
	if s.indexes[l.Index] {
		return
	}
	s.indexes[l.Index] = true
	s.Chunks = append(s.Chunks, l)
}

func (b *builder) contentHref(sha string) string {
	file := rundir.ContentPath(sha)
	if file == "" {
		return ""
	}
	return href(path.Join(b.docproc, filepath.ToSlash(file)))
}

func (b *builder) text(sha string) (*text, error) {
	if sha == "" {
		return nil, nil
	}
	if t, ok := b.texts[sha]; ok {
		return t, nil
	}
	t := &text{SHA256: sha}
	b.texts[sha] = t
	r, err := b.d.Texts().Open(b.ctx, sha)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, dispatch.ErrRequest) {
		t.Missing = true
		return t, nil
	}
	if err != nil {
		return nil, fmt.Errorf("report: %w", err)
	}
	body, err := io.ReadAll(r)
	if err := errors.Join(err, r.Close()); err != nil {
		return nil, fmt.Errorf("report: text %s: %w", sha, err)
	}
	if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != sha {
		t.Damaged = hex.EncodeToString(sum[:])
	}
	t.Body = strings.ToValidUTF8(string(body), "�")
	return t, nil
}

func href(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}
