package chunk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version is the version of the splitting. Any change to the chunks a text, bound, and count give
// must change it, since docinfer reports it in its Profile through [Splitter.Settings]; the golden
// corpus, testdata/chunks-v<Version>.json, is named for it and fails on such a change.
const Version = "2"

// ErrRequest is the cause of a refused request: a Splitter's Bound or Runes below 1 or nil Count,
// or a Bound below the tokens one character of the text measures.
var ErrRequest = errors.New("chunk: invalid request")

// Chunk is one chunk of a text: its Text, the text's bytes at its Span. Its JSON is
// docinfer.Chunk's, which embeds it, versioned by docinfer.Version.
type Chunk struct {
	Text string `json:"text"`
	Span Span   `json:"span"`
}

// Span is a chunk's place in its text: the text's [Start, End) bytes.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Counter is the host's measure of a text: the tokens the embedding model takes for it, as the
// embedding of that text would take them, its special tokens and any document prefix included. It
// measures any text, the empty one too, and its error stops the text's splitting.
type Counter func(ctx context.Context, text string) (int, error)

// Splitter cuts texts into chunks of at most Bound tokens as Count measures them and at most Runes
// characters. Nothing has a default: Bound and Runes must be at least 1 and Count set.
type Splitter struct {
	Bound int     // the most tokens a chunk measures
	Runes int     // the most characters, Unicode code points, a chunk holds
	Count Counter // the host's measure
	Model string  // the served model whose tokenizer Count measures by, which Settings reports
}

type settings struct {
	Version string `json:"version"`
	Bound   int    `json:"bound"`
	Runes   int    `json:"runes"`
	Model   string `json:"model"`
}

// Settings returns the splitting's version and the Splitter's Bound, Runes, and Model as one JSON
// object, {"version":…,"bound":…,"runes":…,"model":…}, in that member order: the chunker's
// identity, which docinfer's Profile reports.
func (s Splitter) Settings() string {
	b, err := json.Marshal(settings{Version: Version, Bound: s.Bound, Runes: s.Runes, Model: s.Model})
	if err != nil {
		panic(err)
	}
	return string(b)
}

const (
	headings = iota
	paragraphs
	sentences
	hard
)

// Split cuts text into chunks of at most s.Bound tokens as s.Count measures them and at most
// s.Runes characters, along its structure, as the package says, in the text's order, passing ctx
// to each count. An empty or white-space-only text yields none. A Bound or Runes below 1, a nil
// Count, or a character of the text that alone measures over the Bound is an error wrapping
// ErrRequest; a count's error is returned wrapped, with no chunk.
func (s Splitter) Split(ctx context.Context, text string) ([]Chunk, error) {
	if s.Bound < 1 {
		return nil, fmt.Errorf("%w: bound %d is below 1", ErrRequest, s.Bound)
	}
	if s.Runes < 1 {
		return nil, fmt.Errorf("%w: runes %d is below 1", ErrRequest, s.Runes)
	}
	if s.Count == nil {
		return nil, fmt.Errorf("%w: no count", ErrRequest)
	}
	sp := &splitter{ctx: ctx, text: text, bound: s.Bound, runes: s.Runes, count: s.Count, empty: -1}
	if err := sp.split(0, len(text), headings, -1); err != nil {
		return nil, err
	}
	return sp.out, nil
}

type splitter struct {
	ctx   context.Context
	text  string
	bound int
	runes int
	count Counter
	empty int
	out   []Chunk
}

type unit struct{ start, end, n int }

func (p *splitter) long(s, e int) bool {
	return utf8.RuneCountInString(p.text[s:e]) > p.runes
}

func (u unit) over(bound int) bool { return u.n < 0 || u.n > bound }

func (p *splitter) measure(s, e int) (int, error) {
	n, err := p.count(p.ctx, p.text[s:e])
	if err != nil {
		return 0, fmt.Errorf("chunk: count bytes [%d, %d): %w", s, e, err)
	}
	return n, nil
}

func (p *splitter) overhead() (int, error) {
	if p.empty < 0 {
		n, err := p.count(p.ctx, "")
		if err != nil {
			return 0, fmt.Errorf("chunk: count the empty text: %w", err)
		}
		p.empty = max(n, 0)
	}
	return p.empty, nil
}

func (p *splitter) emit(s, e int) {
	p.out = append(p.out, Chunk{Text: p.text[s:e], Span: Span{Start: s, End: e}})
}

func (p *splitter) split(s, e, level, n int) error {
	s, e = trim(p.text, s, e)
	if s == e {
		return nil
	}
	if !p.long(s, e) {
		if n < 0 {
			var err error
			if n, err = p.measure(s, e); err != nil {
				return err
			}
		}
		if n <= p.bound {
			p.emit(s, e)
			return nil
		}
	}
	if level == hard {
		return p.cut(s, e, n)
	}
	var units []unit
	from := s
	for _, to := range append(boundaries(p.text, s, e, level), e) {
		if us, ue := trim(p.text, from, to); us < ue {
			units = append(units, unit{start: us, end: ue})
		}
		from = to
	}
	if len(units) == 1 {
		return p.split(s, e, level+1, n)
	}
	for i := range units {
		units[i].n = -1
		if p.long(units[i].start, units[i].end) {
			continue
		}
		m, err := p.measure(units[i].start, units[i].end)
		if err != nil {
			return err
		}
		units[i].n = m
	}
	return p.pack(units, level)
}

func (p *splitter) pack(units []unit, level int) error {
	for i := 0; i < len(units); {
		if units[i].over(p.bound) {
			if err := p.split(units[i].start, units[i].end, level+1, units[i].n); err != nil {
				return err
			}
			i++
			continue
		}
		j, estimate := i, units[i].n
		length := utf8.RuneCountInString(p.text[units[i].start:units[i].end])
		for j+1 < len(units) && !units[j+1].over(p.bound) {
			more := utf8.RuneCountInString(p.text[units[j].end:units[j+1].end])
			if length+more > p.runes {
				break
			}
			o, err := p.overhead()
			if err != nil {
				return err
			}
			if estimate+units[j+1].n-o > p.bound {
				break
			}
			j, estimate, length = j+1, estimate+units[j+1].n-o, length+more
		}
		for ; j > i; j-- {
			m, err := p.measure(units[i].start, units[j].end)
			if err != nil {
				return err
			}
			if m <= p.bound {
				break
			}
		}
		p.emit(units[i].start, units[j].end)
		i = j + 1
	}
	return nil
}

func boundaries(text string, s, e, level int) []int {
	switch level {
	case headings:
		return headingCuts(text, s, e)
	case paragraphs:
		return paragraphCuts(text, s, e)
	default:
		return sentenceCuts(text, s, e)
	}
}

func lines(text string, s, e int, f func(ls, le int)) {
	for ls := s; ls < e; {
		le := strings.IndexByte(text[ls:e], '\n')
		if le < 0 {
			f(ls, e)
			return
		}
		f(ls, ls+le)
		ls += le + 1
	}
}

func headingCuts(text string, s, e int) []int {
	var cuts []int
	fenced := false
	lines(text, s, e, func(ls, le int) {
		line := strings.TrimLeft(text[ls:le], " ")
		if indentedCode := le-ls-len(line) > 3; indentedCode {
			return
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			fenced = !fenced
			return
		}
		if !fenced && ls > s && heading(line) {
			cuts = append(cuts, ls)
		}
	})
	return cuts
}

func heading(line string) bool {
	n := len(line) - len(strings.TrimLeft(line, "#"))
	if n < 1 || n > 6 {
		return false
	}
	return n == len(line) || line[n] == ' ' || line[n] == '\t' || line[n] == '\r'
}

func paragraphCuts(text string, s, e int) []int {
	var cuts []int
	seen, gap := false, false
	lines(text, s, e, func(ls, le int) {
		if strings.TrimSpace(text[ls:le]) == "" {
			gap = seen
			return
		}
		if gap && ls > s {
			cuts = append(cuts, ls)
		}
		seen, gap = true, false
	})
	return cuts
}

const closers = `"')]}’”»`

func sentenceCuts(text string, s, e int) []int {
	var cuts []int
	add := func(at int) {
		if at > s && at < e && (len(cuts) == 0 || cuts[len(cuts)-1] < at) {
			cuts = append(cuts, at)
		}
	}
	for i := s; i < e; {
		r, w := utf8.DecodeRuneInString(text[i:e])
		i += w
		switch r {
		case '\n', '。', '！', '？':
			add(i)
		case '.', '!', '?':
			j := i
			for j < e {
				c, cw := utf8.DecodeRuneInString(text[j:e])
				if !strings.ContainsRune(closers, c) {
					break
				}
				j += cw
			}
			if c, _ := utf8.DecodeRuneInString(text[j:e]); j < e && unicode.IsSpace(c) {
				add(j)
			}
		}
	}
	return cuts
}

func (p *splitter) cut(s, e, n int) error {
	var at []int
	for i := range p.text[s:e] {
		at = append(at, s+i)
	}
	at = append(at, e)
	last := len(at) - 1
	a := 0
	for {
		lo, hi, nLo, nHi := a, last, 0, n
		if a+p.runes < last {
			hi = a + p.runes
			m, err := p.measure(trim(p.text, at[a], at[hi]))
			if err != nil {
				return err
			}
			if m <= p.bound {
				lo, nLo = hi, m
			} else {
				nHi = m
			}
		}
		for step := 0; hi-lo > 1 && hi-lo > (lo-a)/32; step++ {
			var mid int
			switch {
			case step%2 == 0:
				mid = lo + (hi-lo)*(p.bound-nLo)/(nHi-nLo)
			case lo > a:
				mid = lo + min((hi-lo)/2, (lo-a)/8+1)
			default:
				mid = lo + (hi-lo)/2
			}
			mid = min(max(mid, lo+1), hi-1)
			ts, te := trim(p.text, at[a], at[mid])
			m, err := p.measure(ts, te)
			if err != nil {
				return err
			}
			if m <= p.bound {
				lo, nLo = mid, m
			} else {
				hi, nHi = mid, m
			}
		}
		if lo == a {
			return fmt.Errorf("%w: the character at byte %d measures %d tokens, over bound %d", ErrRequest, at[a], nHi, p.bound)
		}
		p.emit(trim(p.text, at[a], at[lo]))
		rs, _ := trim(p.text, at[lo], e)
		if rs == e {
			return nil
		}
		for a = lo; at[a] < rs; {
			a++
		}
		if n = -1; !p.long(rs, e) {
			var err error
			if n, err = p.measure(rs, e); err != nil {
				return err
			}
			if n <= p.bound {
				p.emit(rs, e)
				return nil
			}
		}
	}
}

func trim(text string, s, e int) (int, int) {
	for s < e {
		r, w := utf8.DecodeRuneInString(text[s:e])
		if !unicode.IsSpace(r) {
			break
		}
		s += w
	}
	for e > s {
		r, w := utf8.DecodeLastRuneInString(text[s:e])
		if !unicode.IsSpace(r) {
			break
		}
		e -= w
	}
	return s, e
}
