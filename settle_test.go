package docinfer_test

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

// The settlement tests drive Pipeline.Run's Result.Settled through aitest over MemoryContent:
// each input settled once everything derived from it is out, which collect checks of every run
// loosely and settlesAfter strictly.

// settleEndpoint serves a description of each image, a streamed extraction of each text, each
// clip's transcript in route mode, and embedAll. replies answers a clip, or "image" a description,
// in place of its own.
func settleEndpoint(t *testing.T, said map[string]string, replies map[string]aitest.Reply) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if req.Stream {
				return aitest.ChatReply{Chunks: []string{`{"total":`, `1}`}, PromptTokens: 4, CompletionTokens: 2}
			}
			if r, ok := replies["image"]; ok {
				return r
			}
			return aitest.ChatReply{Text: description, PromptTokens: 7, CompletionTokens: 3}
		},
		Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
			clip := string(req.File)
			if r, ok := replies[clip]; ok {
				return r
			}
			text, ok := said[clip]
			if !ok {
				t.Errorf("a transcription of clip %q, which no window says", clip)
				return aitest.ErrorReply{Status: 418, Message: "no such clip"}
			}
			return aitest.TranscriptionReply{Text: text, InputTokens: 5, OutputTokens: 2}
		},
		Embeddings: embedAll,
	})
}

// settleConfig returns extractConfig's Config on e with Vision and route-mode Transcription set.
func settleConfig(t *testing.T, e *aitest.Endpoint, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	client := newClient(t, e.URL(), ai.TranscribeRoute)
	return extractConfig(t, e, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{Client: client, Model: visionModel, MaxTokens: visionTokens}
		c.Transcription = &docinfer.Transcription{Client: client, Model: transcriptionModel}
		if edit != nil {
			edit(c)
		}
	})
}

// saying returns what the windows say, by clip.
func saying(windows ...win) map[string]string {
	said := map[string]string{}
	for _, w := range windows {
		said[w.clip] = w.said
	}
	return said
}

// settlesAfter fails the test unless o settled each input it echoed after every result derived
// from it, and an input nothing derives from right after its echo. It tells inputs apart by their
// contents, so their contents must be distinct, but for audio plan records, told by their part.
// It returns the positions of the inputs nothing derived from.
func settlesAfter(t *testing.T, o outcome) []int {
	t.Helper()
	var (
		d                derivation
		echoAt, settleAt = map[int]int{}, map[int]int{}
		last             = map[int]int{} // each input's last derived result
		n                int
	)
	for i, r := range o.all {
		switch {
		case r.Input != nil:
			d.echo(n, *r.Input)
			echoAt[n] = i
			n++
		case r.Settled != nil:
			settleAt[*r.Settled] = i
		default:
			_, positions := d.derived(r)
			for _, p := range positions {
				last[p] = i
			}
		}
	}
	var none []int
	for p := range n {
		at, ok := settleAt[p]
		l, derived := last[p]
		switch {
		case !ok:
			t.Errorf("input %d never settled", p)
		case derived && at < l:
			t.Errorf("input %d settled at result %d, before result %d derived from it", p, at, l)
		case !derived && at != echoAt[p]+1:
			t.Errorf("input %d, from which nothing derives, settled at result %d, want right after its echo at %d", p, at, echoAt[p])
		}
		if !derived {
			none = append(none, p)
		}
	}
	return none
}

// Every input kind settles after its last derived result: a text, a Markdown output, and a
// metadata text after their batches and extraction; an image after its inference, batches, and
// extraction; the windows and the audio plan record after their group's transcript, its batches,
// and its extraction. The video plan record and the passed-through inputs, needing no work, settle
// right after their echoes.
func TestRunSettlesEveryInput(t *testing.T) {
	e := settleEndpoint(t, saying(win0, win1, win2), nil)
	content := dispatch.NewMemoryContent()
	pass := passThrough()
	inputs := []docinfer.Input{
		pass[0],
		outputInput(t, content, childPart, dispatch.TextOutput, plainText),
		windowInput(t, content, win0),
		outputInput(t, content, childPart, dispatch.MarkdownOutput, markdown),
		imageInput(t, content, imageBytes, imageType),
		pass[1],
		planInput(t, content, dispatch.MediaAudioPlan, 3),
		windowInput(t, content, win1),
		planInput(t, content, dispatch.MediaVideoPlan, 1),
		windowInput(t, content, win2),
		metadataInput(),
		pass[2],
	}
	o := collect(t, newPipeline(t, settleConfig(t, e, nil)).Run(t.Context(), content, seq(inputs...), extracting(false)))
	echoes(t, o, inputs)
	// The pass-throughs and the video plan record need no work; every other input has results.
	if none, want := settlesAfter(t, o), []int{0, 5, 8, 11}; !slices.Equal(none, want) {
		t.Errorf("inputs %v had no derived results, want %v", none, want)
	}
	if kinds := capabilities(o); kinds[docinfer.CapabilityVision] != 1 || kinds[docinfer.CapabilityTranscription] != 1 || kinds[docinfer.CapabilityExtraction] != 4 {
		t.Errorf("inferences by capability %v, want a description, a transcript, and 4 extractions", kinds)
	}
}

// capabilities counts o's inferences by capability.
func capabilities(o outcome) map[docinfer.Capability]int {
	kinds := map[docinfer.Capability]int{}
	for _, inf := range o.inferences {
		kinds[inf.Capability]++
	}
	return kinds
}

// A window group's windows and audio plan record settle together, after the group's transcript,
// its batches, and its extraction, whichever way the group ends: at its plan's window count or at
// its last window, both while the input stream is still open, or at the end of the inputs, and
// only then.
func TestRunWindowGroupSettles(t *testing.T) {
	open1 := win1
	open1.after = "" // win1 as the last window
	cases := []struct {
		name   string
		inputs func(content *dispatch.MemoryContent) []docinfer.Input
		before bool // the group settles before the inputs end
	}{
		{"plan count", func(c *dispatch.MemoryContent) []docinfer.Input {
			// Both windows have MetaSeamAfter, so only the plan's count ends the group.
			return []docinfer.Input{windowInput(t, c, win0), planInput(t, c, dispatch.MediaAudioPlan, 2), windowInput(t, c, win1)}
		}, true},
		{"last window", func(c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{windowInput(t, c, win0), windowInput(t, c, open1)}
		}, true},
		{"end of inputs", func(c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{windowInput(t, c, win0), windowInput(t, c, win1)}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := settleEndpoint(t, saying(win0, win1), nil)
			p := newPipeline(t, settleConfig(t, e, nil))
			content := dispatch.NewMemoryContent()
			inputs := tc.inputs(content)
			release := make(chan struct{})
			var late, ended atomic.Bool
			stream := endsBefore(release, &late, inputs...)
			if !tc.before {
				stream = endsFlagged(&ended, inputs...)
			}
			results := settling(p.Run(t.Context(), content, stream, extracting(false)), len(inputs), release, func() {
				if !tc.before && !ended.Load() {
					t.Error("the group settled before the inputs ended")
				}
			})
			o := collect(t, results)
			if late.Load() {
				t.Error("the group didn't settle until the inputs ended")
			}
			if none := settlesAfter(t, o); len(none) != 0 {
				t.Errorf("inputs %v had no derived results, want each the group's", none)
			}
			if kinds := capabilities(o); kinds[docinfer.CapabilityTranscription] != 1 || kinds[docinfer.CapabilityExtraction] != 1 || len(o.batches) == 0 {
				t.Errorf("inferences by capability %v and %d batches, want a transcript, its extraction, and its batches", kinds, len(o.batches))
			}
			// The members settle together: nothing comes between their settlements.
			first := slices.IndexFunc(o.all, func(r docinfer.Result) bool { return r.Settled != nil })
			for i := first; i < first+len(inputs); i++ {
				if o.all[i].Settled == nil {
					t.Errorf("result %d is %+v, between the group's settlements", i, o.all[i])
				}
			}
		})
	}
}

// endsFlagged returns inputs as a stream that sets ended once they are all pulled.
func endsFlagged(ended *atomic.Bool, inputs ...docinfer.Input) iter.Seq2[docinfer.Input, error] {
	return func(yield func(docinfer.Input, error) bool) {
		for _, in := range inputs {
			if !yield(in, nil) {
				return
			}
		}
		ended.Store(true)
	}
}

// settling passes results on, calling check at each settlement and closing release at the nth.
func settling(results iter.Seq2[docinfer.Result, error], n int, release chan struct{}, check func()) iter.Seq2[docinfer.Result, error] {
	return func(yield func(docinfer.Result, error) bool) {
		var once sync.Once
		settled := 0
		for r, err := range results {
			if r.Settled != nil {
				check()
				if settled++; settled == n {
					once.Do(func() { close(release) })
				}
			}
			if !yield(r, err) {
				return
			}
		}
	}
}

// On the last delivery, a window refused, one failed, and one never cut are still their group's,
// settling with its audio plan record and its transcribed window after the transcript, its
// batches, and its extraction; an image recorded failed settles after its inference.
func TestRunSettlesFailures(t *testing.T) {
	refused := win{index: 1, clip: "clip refused", after: "18-20", before: "8-10"}
	failed := win{index: 2, clip: "clip failed", before: "18-20", after: "28-30"}
	e := settleEndpoint(t, saying(win0), map[string]aitest.Reply{
		refused.clip: aitest.ErrorReply{Status: 400, Message: "clip refused"},
		failed.clip:  aitest.ErrorReply{Status: 500, Message: "broke"},
		"image":      aitest.ErrorReply{Status: 500, Message: "broke"},
	})
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		// The plan lists 4 windows; the fourth is never cut, so the group ends with the inputs.
		planInput(t, content, dispatch.MediaAudioPlan, 4),
		windowInput(t, content, win0),
		imageInput(t, content, imageBytes, imageType),
		windowInput(t, content, refused),
		windowInput(t, content, failed),
		outputInput(t, content, childPart, dispatch.TextOutput, plainText),
	}
	o := collect(t, newPipeline(t, settleConfig(t, e, nil)).Run(t.Context(), content, seq(inputs...), extracting(true)))
	echoes(t, o, inputs)
	if none := settlesAfter(t, o); len(none) != 0 {
		t.Errorf("inputs %v had no derived results, want each its own", none)
	}
	var tr, img *docinfer.Inference
	for i, inf := range o.inferences {
		switch inf.Capability {
		case docinfer.CapabilityTranscription:
			tr = &o.inferences[i]
		case docinfer.CapabilityVision:
			img = &o.inferences[i]
		}
	}
	if tr == nil || tr.Outcome != docinfer.OutcomeFailed || !slices.Equal(tr.Failed, []int{1, 2, 3}) {
		t.Errorf("transcript %+v, want failed, windows 1 to 3 listed", tr)
	}
	if img == nil || img.Outcome != docinfer.OutcomeFailed {
		t.Errorf("image inference %+v, want failed", img)
	}
}

// Under -race, with Pending holding most inputs back and inputs of every kind finishing out of
// order, among them window groups of several parts, every input settles exactly once, before the
// counts, which collect checks.
func TestRunSettlesOnce(t *testing.T) {
	said := map[string]string{}
	content := dispatch.NewMemoryContent()
	var inputs []docinfer.Input
	for g := range 3 {
		part := audioPart
		part.ID = fmt.Sprintf("/talk%d.mp3", g)
		var group []docinfer.Input
		for i := range 2 {
			w := win{index: i, clip: fmt.Sprintf("part %d clip %d", g, i), said: fmt.Sprintf("part %d window %d says a few words", g, i)}
			if i == 0 {
				w.after = "8-10"
			} else {
				w.before = "8-10"
			}
			said[w.clip] = w.said
			group = append(group, windowInput(t, content, w))
		}
		if g != 1 {
			group = append(group, planInput(t, content, dispatch.MediaAudioPlan, 2))
		}
		for _, in := range group {
			in.Event.Part = part
			inputs = append(inputs,
				in,
				outputInput(t, content, childPart, dispatch.TextOutput, fmt.Sprintf("Text %d of part %d, of a few words.", len(inputs), g)),
				passThrough()[len(inputs)%3],
				imageInput(t, content, fmt.Appendf(nil, "image %d", len(inputs)), imageType),
				metadataInput(),
			)
		}
	}
	e := settleEndpoint(t, said, nil)
	p := newPipeline(t, settleConfig(t, e, func(c *docinfer.Config) {
		c.Pending = 2
		c.EmbedBatch = 1
	}))
	for range 5 {
		o := collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
		echoes(t, o, inputs)
		if len(o.settled) != len(inputs) {
			t.Errorf("%d settlements, want one for each of the %d inputs", len(o.settled), len(inputs))
		}
	}
}
