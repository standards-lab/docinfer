package docinfer

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/docproc/dispatch"
)

const videoStage = "video"

type timeline struct {
	origin string
	part   string

	frames  []*frame
	pending int
	group   *group
	heard   []heardSpan
	heardIn bool
	textSHA string

	own     bool
	holds   []int
	started bool
}

type frame struct {
	tl   *timeline
	at   float64
	text string
	sha  string
}

type heardSpan struct {
	start, end float64
	text       string
}

type timelineLine struct {
	at   float64
	kind int
	text string
	sha  string
}

const (
	lineHeard = iota
	lineSeen
)

func (r *run) timelineOf(in Input) *timeline {
	if r.opts.Timeline == nil || in.Event.Stage.Name != videoStage {
		return nil
	}
	k := groupKey{in.Origin, in.Event.Part.ID, in.Event.Stage}
	if tl, ok := r.timelines[k]; ok {
		if tl.started {
			return nil
		}
		return tl
	}
	if r.timelines == nil {
		r.timelines = map[groupKey]*timeline{}
	}
	tl := &timeline{origin: in.Origin, part: in.Event.Part.ID}
	r.timelines[k] = tl
	r.timelineOrder = append(r.timelineOrder, tl)
	return tl
}

func metaSeconds(ev dispatch.Event, what, key string) (float64, error) {
	out := ev.Output
	v := out.Meta[key]
	s, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(s) || math.IsInf(s, 0) || s < 0 {
		return 0, fmt.Errorf("docinfer: run: %s %s of part %s: Meta %s %q: %w", what, out.SHA256, ev.Part.ID, key, v, errInputDefect)
	}
	return s, nil
}

func (r *run) admitFrame(in Input, pos int) error {
	tl := r.timelineOf(in)
	if tl == nil {
		return nil
	}
	at, err := metaSeconds(in.Event, "frame", dispatch.MetaTime)
	if err != nil {
		return err
	}
	f := &frame{tl: tl, at: at}
	tl.frames = append(tl.frames, f)
	if r.p.cfg.Vision != nil {
		tl.pending++
		if r.frames == nil {
			r.frames = map[int]*frame{}
		}
		r.frames[pos] = f
	}
	return nil
}

func (r *run) frameDescribed(e inferenceEvent) {
	f, ok := r.frames[e.pos]
	if !ok {
		return
	}
	delete(r.frames, e.pos)
	if e.inf.Text != "" {
		f.text, f.sha = e.inf.Text, e.inf.TextSHA256
	}
	f.tl.pending--
	r.checkTimeline(f.tl)
}

func (r *run) noteOwn(in Input, pos int) *timeline {
	if in.Event.Warning != nil {
		return nil
	}
	tl := r.timelineOf(in)
	if tl == nil {
		return nil
	}
	tl.own = true
	tl.holds = append(tl.holds, pos)
	if r.holds == nil {
		r.holds = map[int]bool{}
	}
	r.holds[pos] = false
	return tl
}

func (r *run) heardOf(g *group, textSHA string, words []string, seams []int) {
	tl := g.tl
	if tl == nil {
		return
	}
	tl.textSHA = textSHA
	for i := range g.n {
		from, to := 0, len(words)
		if i > 0 {
			from = seams[i-1]
		}
		if i < g.n-1 {
			to = seams[i]
		}
		if w := g.windows[i]; w != nil && to > from {
			tl.heard = append(tl.heard, heardSpan{start: w.start, end: w.end, text: strings.Join(words[from:to], " ")})
		}
	}
	tl.heardIn = true
	r.checkTimeline(tl)
}

func (r *run) checkTimeline(tl *timeline) {
	if tl.started || tl.pending > 0 || (tl.group != nil && !tl.heardIn) || (!tl.own && !r.inputsEnded) {
		return
	}
	tl.started = true
	text, shas := tl.text()
	if text == "" {
		r.releaseTimeline(tl)
		return
	}
	r.active++
	r.spawn(func() { r.extractTimeline(tl, text, shas) })
}

func (r *run) releaseTimeline(tl *timeline) {
	for _, pos := range tl.holds {
		settle := r.holds[pos]
		delete(r.holds, pos)
		if settle {
			r.settled = append(r.settled, pos)
		}
	}
	tl.holds = nil
}

func (tl *timeline) text() (string, []string) {
	var lines []timelineLine
	for _, h := range tl.heard {
		lines = append(lines, timelineLine{at: h.start, kind: lineHeard,
			text: "[" + clock(h.start) + "–" + clock(h.end) + "] heard: " + h.text, sha: tl.textSHA})
	}
	for _, f := range tl.frames {
		if f.text == "" {
			continue
		}
		lines = append(lines, timelineLine{at: f.at, kind: lineSeen,
			text: "[" + clock(f.at) + "] seen: " + strings.Join(strings.Fields(f.text), " "), sha: f.sha})
	}
	slices.SortStableFunc(lines, func(a, b timelineLine) int {
		return cmp.Or(cmp.Compare(math.Floor(a.at), math.Floor(b.at)), cmp.Compare(a.kind, b.kind), cmp.Compare(a.at, b.at))
	})
	texts := make([]string, len(lines))
	var shas []string
	for i, l := range lines {
		texts[i] = l.text
		if !slices.Contains(shas, l.sha) {
			shas = append(shas, l.sha)
		}
	}
	return strings.Join(texts, "\n"), shas
}

func clock(seconds float64) string {
	s := int(math.Floor(seconds))
	h, m := s/3600, s/60%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s%60)
	}
	return fmt.Sprintf("%d:%02d", m, s%60)
}

func (r *run) extractTimeline(tl *timeline, text string, shas []string) {
	start := time.Now()
	sha := sha256Hex([]byte(text))
	t, ok := r.extractText(*r.opts.Timeline, r.timelineID, text, "the timeline text "+sha+" of part "+tl.part)
	if !ok {
		return
	}
	inf := t.inference(tl.part, tl.origin, shas, start)
	inf.Scope = ScopeTimeline
	r.send(extractEvent{tl: tl, inf: inf, requests: t.requests, usage: t.usage, held: t.held})
}
