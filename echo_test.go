package docinfer_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

// The echo tests drive Pipeline.Run's Result.Input through aitest over MemoryContent: each input
// echoed once, in pull order, before anything derived from it, which collect checks of every run.

// passThrough returns inputs that are neither an output nor a metadata text: a warning, a part's
// own event, and an unread child's.
func passThrough() []docinfer.Input {
	return []docinfer.Input{
		{Event: dispatch.Event{Part: childPart, Stage: textStage, Warning: &dispatch.Warning{Tool: "office", Where: "word/document.xml", What: "a field is unreadable"}}},
		{Event: dispatch.Event{Part: childPart, Stage: textStage, Outcome: dispatch.Prepared, Outputs: 2}},
		{Event: dispatch.Event{Part: dispatch.Part{ID: "/big", Parent: "/", Depth: 1}, Outcome: dispatch.Limited, Reason: "past the parts cap"}},
	}
}

// echoes fails the test unless o echoed exactly inputs, in order, and ended without an error.
func echoes(t *testing.T, o outcome, inputs []docinfer.Input) {
	t.Helper()
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	if !slices.EqualFunc(o.inputs, inputs, func(a, b docinfer.Input) bool { return reflect.DeepEqual(a, b) }) {
		t.Errorf("Run echoed %d inputs, want each of the %d once, in pull order:\n got %+v\nwant %+v", len(o.inputs), len(inputs), o.inputs, inputs)
	}
}

// Every input kind is echoed once, in pull order, before its derived results: a text, a Markdown
// output, a metadata text, an image, an audio plan record and its windows, a video plan record, and
// passed-through inputs, with vision, transcription, and extraction on. The passed-through inputs
// do no work and count nothing: the run's counts are those of a run without them.
func TestRunEchoesEveryInput(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if !req.Stream {
				return aitest.ChatReply{Text: description, PromptTokens: 7, CompletionTokens: 3}
			}
			return aitest.ChatReply{Chunks: []string{`{"total":`, `1}`}, PromptTokens: 4, CompletionTokens: 2}
		},
		Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
			for _, w := range []win{win0, win1, win2} {
				if string(req.File) == w.clip {
					return aitest.TranscriptionReply{Text: w.said, InputTokens: 5, OutputTokens: 2}
				}
			}
			t.Errorf("a transcription of clip %q, which no window says", req.File)
			return aitest.ErrorReply{Status: 418, Message: "no such clip"}
		},
		Embeddings: embedAll,
	})
	config := func() docinfer.Config {
		client := newClient(t, e.URL(), ai.TranscribeRoute)
		return extractConfig(t, e, func(c *docinfer.Config) {
			c.Vision = &docinfer.Vision{Client: client, Model: visionModel, MaxTokens: visionTokens}
			c.Transcription = &docinfer.Transcription{Client: client, Model: transcriptionModel}
		})
	}
	content := dispatch.NewMemoryContent()
	work := []docinfer.Input{
		outputInput(t, content, childPart, dispatch.TextOutput, plainText),
		windowInput(t, content, win0),
		outputInput(t, content, childPart, dispatch.MarkdownOutput, markdown),
		imageInput(t, content, imageBytes, imageType),
		planInput(t, content, dispatch.MediaAudioPlan, 3),
		windowInput(t, content, win1),
		planInput(t, content, dispatch.MediaVideoPlan, 1),
		windowInput(t, content, win2),
		metadataInput(),
	}
	pass := passThrough()
	// The passed-through inputs sit among the others: one first, one in the middle, one last.
	all := slices.Concat(pass[:1], work[:4], pass[1:2], work[4:], pass[2:])

	o := collect(t, newPipeline(t, config()).Run(t.Context(), content, seq(all...), extracting(false)))
	echoes(t, o, all)
	// collect's checks are vacuous without derived results of each kind to check.
	kinds := map[docinfer.Capability]int{}
	for _, inf := range o.inferences {
		kinds[inf.Capability]++
	}
	if kinds[docinfer.CapabilityVision] != 1 || kinds[docinfer.CapabilityTranscription] != 1 || kinds[docinfer.CapabilityExtraction] != 4 {
		t.Errorf("inferences by capability %v, want a description, a transcript, and 4 extractions", kinds)
	}
	sources := map[string]bool{}
	for _, c := range o.chunks() {
		sources[c.source.Kind] = true
	}
	for _, k := range []string{"output", "metadata", "vision", "transcript"} {
		if !sources[k] {
			t.Errorf("no batch holds a chunk of kind %s", k)
		}
	}
	if len(o.progress) == 0 {
		t.Error("no extraction progress")
	}

	without := collect(t, newPipeline(t, config()).Run(t.Context(), content, seq(work...), extracting(false)))
	echoes(t, without, work)
	if o.counts == nil || without.counts == nil {
		t.Fatal("a run yielded no counts")
	}
	if !reflect.DeepEqual(*o.counts, *without.counts) {
		t.Errorf("counts with the passed-through inputs\n %+v\nwant those without them\n %+v", *o.counts, *without.counts)
	}
}

// A run of passed-through inputs alone echoes each, asks nothing, and counts nothing: each echo
// followed at once by its settlement, then zero counts.
func TestRunPassesThrough(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, runConfig(t, e, nil))
	inputs := passThrough()
	o := collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(inputs...), docinfer.Options{}))
	echoes(t, o, inputs)
	if o.results != 2*len(inputs)+1 || o.counts == nil {
		t.Fatalf("%d results, want %d echoes, each followed by its settlement, and the counts", o.results, len(inputs))
	}
	for i := range inputs {
		if echo, settled := o.all[2*i], o.all[2*i+1]; echo.Input == nil || settled.Settled == nil || *settled.Settled != i {
			t.Errorf("results %d and %d are %+v and %+v, want input %d's echo, then its settlement", 2*i, 2*i+1, echo, settled, i)
		}
	}
	if *o.counts != (docinfer.Counts{}) {
		t.Errorf("counts %+v, want none", *o.counts)
	}
	if n := len(e.Requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

// Under -race, with Pending holding most inputs back and inputs finishing out of order, each input
// is echoed exactly once, in pull order.
func TestRunEchoesOnce(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
	p := newPipeline(t, runConfig(t, e, func(c *docinfer.Config) {
		c.Pending = 2
		c.EmbedBatch = 1
	}))
	content := dispatch.NewMemoryContent()
	var inputs []docinfer.Input
	for i := range 30 {
		switch i % 3 {
		case 0:
			inputs = append(inputs, outputInput(t, content, childPart, dispatch.TextOutput, fmt.Sprintf("Text number %d, of a few words.", i)))
		case 1:
			inputs = append(inputs, passThrough()[i%2])
		default:
			inputs = append(inputs, metadataInput())
		}
	}
	for range 5 {
		echoes(t, collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{})), inputs)
	}
}
