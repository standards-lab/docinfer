package chunk

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Version is the version of the splitting. Any change to the chunks a text and bound give must
// change it, since docinfer reports it in its Profile through [Splitter.Settings]; the golden
// corpus, testdata/chunks-v<Version>.json, is named for it and fails on such a change.
const Version = "1"

// ErrRequest is the cause of a refused request: a Splitter's Bound below 1.
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

// Splitter cuts texts into chunks of at most Bound runes. Bound has no default: it must be at
// least 1.
type Splitter struct {
	Bound int
}

// settings is the JSON of a Splitter's Settings, its members in this order.
type settings struct {
	Version string `json:"version"`
	Bound   int    `json:"bound"`
}

// Settings returns the splitting's version and the Splitter's Bound as one JSON object,
// {"version":…,"bound":…}, in that member order, for docinfer's Profile to report. It keys
// nothing.
func (s Splitter) Settings() string {
	b, err := json.Marshal(settings{Version: Version, Bound: s.Bound})
	if err != nil {
		panic(err) // a string and an int always marshal
	}
	return string(b)
}

// The levels a text is cut at, coarsest first.
const (
	headings = iota
	paragraphs
	sentences
	hard
)

// Split cuts text into chunks of at most s.Bound runes, along its structure, as the package says,
// in the text's order. An empty or white-space-only text yields none. A Bound below 1 is an error
// wrapping ErrRequest.
func (s Splitter) Split(text string) ([]Chunk, error) {
	if s.Bound < 1 {
		return nil, fmt.Errorf("%w: bound %d is below 1", ErrRequest, s.Bound)
	}
	var out []Chunk
	split(text, 0, len(text), s.Bound, headings, &out)
	return out, nil
}

// split appends the chunks of text's [s, e) bytes, cut at level and finer, to out.
func split(text string, s, e, bound, level int, out *[]Chunk) {
	s, e = trim(text, s, e)
	if s == e {
		return
	}
	if utf8.RuneCountInString(text[s:e]) <= bound {
		*out = append(*out, Chunk{Text: text[s:e], Span: Span{Start: s, End: e}})
		return
	}
	if level == hard {
		cut(text, s, e, bound, out)
		return
	}
	// The units tile [s, e) between the level's cuts; runs of whole units that fit the bound are
	// packed into one chunk, and a unit that doesn't fit alone is cut at the next level.
	cuts := append(boundaries(text, s, e, level), e)
	start, end, runes := -1, -1, 0
	from := s
	for _, to := range cuts {
		n := utf8.RuneCountInString(text[from:to])
		switch {
		case start >= 0 && runes+n <= bound:
			end, runes = to, runes+n
		default:
			if start >= 0 {
				split(text, start, end, bound, level+1, out)
				start = -1
			}
			if n <= bound {
				start, end, runes = from, to, n
			} else {
				split(text, from, to, bound, level+1, out)
			}
		}
		from = to
	}
	if start >= 0 {
		split(text, start, end, bound, level+1, out)
	}
}

// boundaries returns the offsets inside (s, e) where level cuts text, in order.
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

// lines calls f with the start and end, its line break excluded, of each line of text's [s, e).
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

// headingCuts returns the start of each ATX heading line in (s, e) outside a fenced code block.
func headingCuts(text string, s, e int) []int {
	var cuts []int
	fenced := false
	lines(text, s, e, func(ls, le int) {
		line := strings.TrimLeft(text[ls:le], " ")
		if le-ls-len(line) > 3 {
			return // indented four or more: code, not a heading or fence
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

// heading reports whether line, its indentation removed, opens an ATX heading: one to six "#"
// and then white space or the line's end.
func heading(line string) bool {
	n := len(line) - len(strings.TrimLeft(line, "#"))
	if n < 1 || n > 6 {
		return false
	}
	return n == len(line) || line[n] == ' ' || line[n] == '\t' || line[n] == '\r'
}

// paragraphCuts returns the start of each line in (s, e) that follows one or more blank lines
// after a line that isn't blank: the start of each paragraph but the first.
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

// closers may follow a sentence's terminal punctuation before the white space that ends it.
const closers = `"')]}’”»`

// sentenceCuts returns the offsets in (s, e) where a sentence or a line ends: after ".", "!", or
// "?" and any closers, where white space follows; after "。", "！", or "？"; and after each line
// break.
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

// cut appends text's [s, e) bytes to out as chunks of bound runes, the last the rest, each
// trimmed, none empty.
func cut(text string, s, e, bound int, out *[]Chunk) {
	for s < e {
		end, n := s, 0
		for end < e && n < bound {
			_, w := utf8.DecodeRuneInString(text[end:e])
			end += w
			n++
		}
		if cs, ce := trim(text, s, end); cs < ce {
			*out = append(*out, Chunk{Text: text[cs:ce], Span: Span{Start: cs, End: ce}})
		}
		s = end
	}
}

// trim returns [s, e) narrowed past the white space at both its ends.
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
