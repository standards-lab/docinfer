package docinfer

import (
	"slices"
	"strings"
	"time"
)

const documentSeparator = "\n\n"

type documentText struct {
	pos  int
	sha  string
	text string
}

type rootPart struct {
	id     string
	origin string
	seen   bool
}

func (r *run) noteRoot(in Input) {
	if r.root.seen || in.Event.Part.Depth != 0 || in.Event.Part.ID == "" {
		return
	}
	r.root = rootPart{id: in.Event.Part.ID, origin: in.Origin, seen: true}
}

func (r *run) startDocument() bool {
	if r.opts.Document == nil || r.documentStarted || len(r.documentTexts) == 0 {
		return false
	}
	r.documentStarted = true
	r.active++
	texts := slices.Clone(r.documentTexts)
	slices.SortStableFunc(texts, func(a, b documentText) int { return a.pos - b.pos })
	root := r.root
	if !root.seen {
		root.id = "/"
	}
	r.spawn(func() { r.document(texts, root) })
	return true
}

func (r *run) document(texts []documentText, root rootPart) {
	start := time.Now()
	parts := make([]string, len(texts))
	shas := make([]string, len(texts))
	for i, t := range texts {
		parts[i], shas[i] = t.text, t.sha
	}
	text := strings.Join(parts, documentSeparator)
	sha := sha256Hex([]byte(text))
	t, ok := r.extractText(*r.opts.Document, r.documentID, text, "the document text "+sha+" of part "+root.id)
	if !ok {
		return
	}
	inf := t.inference(root.id, root.origin, shas, start)
	inf.Scope = ScopeDocument
	r.send(extractEvent{inf: inf, requests: t.requests, usage: t.usage, held: t.held})
}
