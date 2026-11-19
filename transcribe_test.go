package docinfer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/join"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
)

// The transcription tests transcribe windows through Run, against aitest's transcriptions route
// in route mode and its Chat Completions route in chat mode, then join each part's windows, and
// take the transcription rows of design.md's failure table, one test per row.

const (
	transcriptionModel = "transcription-model"
	transcriptionLang  = "en"
)

// The audio part the windows come from, and the stage that cut them.
var (
	audioPart = dispatch.Part{ID: "/talk.mp3", Name: "talk.mp3", Size: 4096, SHA256: strings.Repeat("d", 64),
		Parent: "/", Depth: 1, Where: "talk.mp3"}
	audioStage = dispatch.Identity{Name: "audio", Version: "1", Config: "a0"}
)

// The windows' transcripts: each pair of neighbours shares words a 2-second seam at the tests'
// 3 words a second reaches, and the first pair's overlap is past a 1-second seam's reach.
const (
	said0 = "one two three four five six seven"
	said1 = "three four five six seven eight nine ten"
	said2 = "nine ten eleven twelve"
)

// win is a window a test sends: its index, its clip, the transcript the endpoint says for it, its
// seams' Meta values, "" for none, and its media type, audio/wav when "".
type win struct {
	index         int
	clip          string
	said          string
	before, after string
	mediaType     string
}

// The three windows of the tests' plan, each seam 2 seconds, the last without MetaSeamAfter.
var (
	win0 = win{index: 0, clip: "clip 0", said: said0, after: "8-10"}
	win1 = win{index: 1, clip: "clip 1", said: said1, before: "8-10", after: "18-20"}
	win2 = win{index: 2, clip: "clip 2", said: said2, before: "18-20"}
)

// windowInput returns an input of w as an audio output of audioPart from audioStage, its clip put
// in content, unless content is nil.
func windowInput(t *testing.T, content *dispatch.MemoryContent, w win) docinfer.Input {
	t.Helper()
	if content != nil {
		if err := content.Put(t.Context(), sha(w.clip), strings.NewReader(w.clip)); err != nil {
			t.Fatal(err)
		}
	}
	mediaType := w.mediaType
	if mediaType == "" {
		mediaType = "audio/wav"
	}
	meta := map[string]string{dispatch.MetaWindow: strconv.Itoa(w.index)}
	if w.before != "" {
		meta[dispatch.MetaSeamBefore] = w.before
	}
	if w.after != "" {
		meta[dispatch.MetaSeamAfter] = w.after
	}
	return docinfer.Input{Event: dispatch.Event{
		Part:   audioPart,
		Stage:  audioStage,
		Output: &dispatch.Output{Kind: dispatch.AudioOutput, Name: "window", MediaType: mediaType, Meta: meta, SHA256: sha(w.clip)},
	}}
}

// planInput returns an input of a plan record of mediaType listing n windows, as docproc's audio
// plan does, its content put in content.
func planInput(t *testing.T, content *dispatch.MemoryContent, mediaType string, n int) docinfer.Input {
	t.Helper()
	windows := make([]map[string]any, n)
	for i := range windows {
		windows[i] = map[string]any{"index": i, "start": 10 * i, "end": 10*i + 10, "cut": "silence"}
	}
	b, err := json.Marshal(map[string]any{"duration": 10 * n, "limit": 30, "overlap": 2, "windows": windows, "seams": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := content.Put(t.Context(), sha(string(b)), strings.NewReader(string(b))); err != nil {
		t.Fatal(err)
	}
	return docinfer.Input{Event: dispatch.Event{
		Part:   audioPart,
		Stage:  audioStage,
		Output: &dispatch.Output{Kind: dispatch.RecordOutput, Name: "plan", MediaType: mediaType, SHA256: sha(string(b))},
	}}
}

// speaker is the fake endpoint's transcription in one mode: what it says for each clip, a reply
// of its own for the clips in replies, and, when gate is set, each request parked on it first.
type speaker struct {
	mode    ai.TranscriptionMode
	said    map[string]string
	replies map[string]aitest.Reply
	gate    *aitest.Gate
}

// speakerOf returns a speaker in mode that says each window's transcript.
func speakerOf(mode ai.TranscriptionMode, windows ...win) *speaker {
	s := &speaker{mode: mode, said: map[string]string{}, replies: map[string]aitest.Reply{}}
	for _, w := range windows {
		s.said[w.clip] = w.said
	}
	return s
}

// reply answers clip: its reply of its own, or its transcript in the mode's shape, 5 input and 2
// output tokens.
func (s *speaker) reply(ctx context.Context, t *testing.T, clip string) aitest.Reply {
	if s.gate != nil {
		if err := s.gate.Wait(ctx); err != nil {
			return aitest.ErrorReply{Status: 503, Message: err.Error()}
		}
	}
	if r, ok := s.replies[clip]; ok {
		return r
	}
	text, ok := s.said[clip]
	if !ok {
		t.Errorf("a request for clip %q, which no window says", clip)
	}
	if s.mode == ai.TranscribeChat {
		return aitest.ChatReply{Text: text, PromptTokens: 5, CompletionTokens: 2}
	}
	return aitest.TranscriptionReply{Text: text, InputTokens: 5, OutputTokens: 2}
}

// serve serves s's routes, beside embedAll, checking each transcription request: the model, the
// language, and a .wav or .mp3 clip.
func (s *speaker) serve(t *testing.T) *aitest.Endpoint {
	t.Helper()
	routes := aitest.Routes{Embeddings: embedAll}
	if s.mode == ai.TranscribeChat {
		routes.ChatCompletions = func(ctx context.Context, req aitest.ChatRequest) aitest.Reply {
			if req.Model != transcriptionModel || len(req.Messages) != 1 || len(req.Messages[0].Audio) != 1 {
				t.Errorf("chat request of model %q, %d messages; want %s and one clip", req.Model, len(req.Messages), transcriptionModel)
				return aitest.ErrorReply{Status: 418, Message: "not a transcription"}
			}
			m := req.Messages[0]
			if m.Text != ai.TranscriptionInstruction(transcriptionLang) || m.Audio[0].Format != "wav" {
				t.Errorf("chat transcription of text %q, format %q; want the instruction and wav", m.Text, m.Audio[0].Format)
			}
			return s.reply(ctx, t, string(m.Audio[0].Data))
		}
	} else {
		routes.Transcriptions = func(ctx context.Context, req aitest.TranscriptionRequest) aitest.Reply {
			if req.Model != transcriptionModel || req.Language != transcriptionLang || req.Prompt != "" ||
				(req.FileName != "window.wav" && req.FileName != "window.mp3") {
				t.Errorf("transcription of model %q, language %q, prompt %q, file %q; want %s, %s, none, window.wav or .mp3",
					req.Model, req.Language, req.Prompt, req.FileName, transcriptionModel, transcriptionLang)
			}
			return s.reply(ctx, t, string(req.File))
		}
	}
	return aitest.Serve(t, routes)
}

// transcriptionConfig returns runConfig's Config with Transcription set on e in mode.
func transcriptionConfig(t *testing.T, e *aitest.Endpoint, mode ai.TranscriptionMode, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	return runConfig(t, e, func(c *docinfer.Config) {
		c.Transcription = &docinfer.Transcription{
			Client: newClient(t, e.URL(), mode), Model: transcriptionModel, Language: transcriptionLang,
		}
		if edit != nil {
			edit(c)
		}
	})
}

// transcriptionID is the identity of transcriptionConfig's transcripts in mode.
func transcriptionID(t *testing.T, mode ai.TranscriptionMode) docinfer.Identity {
	t.Helper()
	return docinfer.Transcription{
		Client: newClient(t, "http://localhost/v1", mode), Model: transcriptionModel, Language: transcriptionLang,
	}.Identity()
}

// heldWindow returns the Held the cache holds for clip's transcript in mode, or nil.
func heldWindow(t *testing.T, cache docinfer.Cache, mode ai.TranscriptionMode, clip string) *docinfer.Held {
	t.Helper()
	values, err := cache.Get(t.Context(), docinfer.Key(transcriptionID(t, mode), sha(clip)))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		return nil
	}
	var h docinfer.Held
	if err := json.Unmarshal(values[0], &h); err != nil {
		t.Fatalf("the held transcript: %v", err)
	}
	return &h
}

// joinedAt returns join.Join of texts at seams, at the tests' 3 words a second.
func joinedAt(t *testing.T, texts []string, seams ...float64) join.Transcript {
	t.Helper()
	tr, err := join.Join(texts, seams, join.Request{WordsPerSecond: 3})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// transcript returns o's one inference, failing the test unless there is exactly one, a
// transcript of audioPart's windows, contents in order, "" for one never cut.
func transcript(t *testing.T, o outcome, contents ...string) docinfer.Inference {
	t.Helper()
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	if len(o.inferences) != 1 {
		t.Fatalf("%d inferences, want 1", len(o.inferences))
	}
	inf := o.inferences[0]
	if inf.Capability != docinfer.CapabilityTranscription || inf.Part != audioPart.ID || !slices.Equal(inf.ContentSHA256, contents) {
		t.Errorf("inference of capability %q, part %q, contents %v; want the transcript of %v", inf.Capability, inf.Part, inf.ContentSHA256, contents)
	}
	return inf
}

// shas returns each clip's SHA-256, "" for "".
func shas(clips ...string) []string {
	out := make([]string, len(clips))
	for i, c := range clips {
		if c != "" {
			out[i] = sha(c)
		}
	}
	return out
}

// checkTranscript fails the test unless inf is outcome with text tr's, seams tr's, failed the
// windows listed, and held as said.
func checkTranscript(t *testing.T, inf docinfer.Inference, outcome docinfer.Outcome, tr join.Transcript, failed []int, held bool) {
	t.Helper()
	text := tr.Text()
	if inf.Outcome != outcome || inf.Text != text || !slices.Equal(inf.Failed, failed) || inf.Held != held ||
		!reflect.DeepEqual(inf.Seams, tr.Seams) || inf.Reason != "" {
		t.Errorf("transcript %+v\nwant %s, text %q, seams %+v, failed %v, held %t", inf, outcome, text, tr.Seams, failed, held)
	}
	if want := sha(text); text != "" && inf.TextSHA256 != want || text == "" && inf.TextSHA256 != "" {
		t.Errorf("transcript's text SHA-256 %q, want its text's", inf.TextSHA256)
	}
}

// transcriptCounts is the Counts of a job of windows windows, tc theirs, made of them transcribed
// by a request, at 5 and 2 tokens each, and o's chunks embedded by one request, or read from the
// cache when held.
func transcriptCounts(o outcome, windows int, tc docinfer.TranscriptionCounts, made int, held bool) docinfer.Counts {
	chunks := len(o.chunks())
	c := docinfer.Counts{
		Windows: windows, Transcription: &tc, Embedded: chunks,
		Tokens: docinfer.Tokens{Transcription: &docinfer.Usage{Input: 5 * made, Output: 2 * made}},
	}
	if held {
		c.Held = chunks
	} else if chunks > 0 {
		c.Requests, c.Tokens.Embeddings.Input = 1, 3*chunks
	}
	return c
}

// endsBefore returns inputs as a stream that, once they are all pulled, waits for release before
// it ends, at most failsafe, setting late if it waited it out: a group that ends before the
// inputs do releases it.
func endsBefore(release <-chan struct{}, late *atomic.Bool, inputs ...docinfer.Input) iter.Seq2[docinfer.Input, error] {
	return func(yield func(docinfer.Input, error) bool) {
		for _, in := range inputs {
			if !yield(in, nil) {
				return
			}
		}
		select {
		case <-release:
		case <-time.After(failsafe):
			late.Store(true)
		}
	}
}

// releasing passes results on, closing release at the first inference.
func releasing(results iter.Seq2[docinfer.Result, error], release chan struct{}) iter.Seq2[docinfer.Result, error] {
	return func(yield func(docinfer.Result, error) bool) {
		var once sync.Once
		for r, err := range results {
			if r.Inference != nil {
				once.Do(func() { close(release) })
			}
			if !yield(r, err) {
				return
			}
		}
	}
}

// Each window is transcribed from its content and held under transcription's identity and its
// SHA-256, in route mode through the transcriptions route and in chat mode as a Chat Completions
// request of the clip; the part's windows are joined at their seams into one transcript, yielded
// before its batches, chunked as kind transcript, and embedded. A second run reads every window
// from the cache, asks nothing, opens no content, and the transcript is held.
func TestRunTranscribesWindows(t *testing.T) {
	for _, mode := range []ai.TranscriptionMode{ai.TranscribeRoute, ai.TranscribeChat} {
		t.Run(mode.String(), func(t *testing.T) {
			e := speakerOf(mode, win0, win1, win2).serve(t)
			cfg := transcriptionConfig(t, e, mode, nil)
			p := newPipeline(t, cfg)
			content := dispatch.NewMemoryContent()
			inputs := []docinfer.Input{windowInput(t, content, win0), windowInput(t, content, win1), windowInput(t, content, win2)}

			o := collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
			inf := transcript(t, o, shas(win0.clip, win1.clip, win2.clip)...)
			tr := joinedAt(t, []string{said0, said1, said2}, 2, 2)
			if len(tr.Seams) != 2 || !tr.Seams[0].Matched || !tr.Seams[1].Matched {
				t.Fatalf("the test's windows join with seams %+v, want both matched", tr.Seams)
			}
			checkTranscript(t, inf, docinfer.OutcomeTranscribed, tr, nil, false)
			if inf.Usage != (docinfer.Usage{Input: 15, Output: 6}) {
				t.Errorf("transcript's usage %+v, want its three requests'", inf.Usage)
			}
			for _, w := range []win{win0, win1, win2} {
				if h := heldWindow(t, cfg.Cache, mode, w.clip); h == nil || *h != (docinfer.Held{Text: w.said}) {
					t.Errorf("window %d held as %+v, want its transcript", w.index, h)
				}
			}
			pieces, _ := cfg.Chunker.Split(tr.Text())
			chunks := o.chunks()
			if len(chunks) != len(pieces) || len(pieces) < 2 {
				t.Fatalf("%d chunks, want the transcript's %d", len(chunks), len(pieces))
			}
			for i, c := range chunks {
				s := c.source
				if c.chunk.Text != pieces[i].Text || c.chunk.Vector == "" || s.Kind != "transcript" || s.SHA256 != sha(tr.Text()) ||
					s.Incomplete != "" || s.Stage == nil || *s.Stage != audioStage || s.Output == nil ||
					!reflect.DeepEqual(*s.Output, *inputs[0].Event.Output) || c.part.ID != audioPart.ID {
					t.Errorf("chunk %d: %+v of source %+v, want %q of the transcript, the first window its output", i, c.chunk, s, pieces[i].Text)
				}
			}
			checkCounts(t, o, transcriptCounts(o, 3, docinfer.TranscriptionCounts{Transcribed: 3, Requests: 3, Transcripts: 1, SeamsMatched: 2}, 3, false))

			requests := len(e.Requests())
			o = collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(inputs...), docinfer.Options{}))
			if n := len(e.Requests()); n != requests {
				t.Errorf("the second run sent %d requests, want none", n-requests)
			}
			inf = transcript(t, o, shas(win0.clip, win1.clip, win2.clip)...)
			checkTranscript(t, inf, docinfer.OutcomeTranscribed, tr, nil, true)
			if inf.Usage != (docinfer.Usage{}) {
				t.Errorf("the held transcript's usage %+v, want none", inf.Usage)
			}
			checkCounts(t, o, transcriptCounts(o, 3, docinfer.TranscriptionCounts{Transcribed: 3, Held: 3, Transcripts: 1, SeamsMatched: 2}, 0, true))
		})
	}
}

// A group ends at the first of its audio plan record's window count, its last window once every
// earlier index is in, and the end of Run's inputs: the first two are seen to end it while the
// input stream is still open.
func TestRunWindowGroupEnds(t *testing.T) {
	open1 := win1
	open1.after, open1.said = "", said1 // win1 as the last window
	cases := []struct {
		name   string
		inputs func(content *dispatch.MemoryContent) []docinfer.Input
		before bool // the group ends before the inputs do
	}{
		{"plan count", func(c *dispatch.MemoryContent) []docinfer.Input {
			// Both windows have MetaSeamAfter, so only the plan's count ends the group.
			return []docinfer.Input{windowInput(t, c, win0), windowInput(t, c, win1), planInput(t, c, dispatch.MediaAudioPlan, 2)}
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
			e := speakerOf(ai.TranscribeRoute, win0, win1).serve(t)
			p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil))
			content := dispatch.NewMemoryContent()
			inputs := tc.inputs(content)
			var o outcome
			if tc.before {
				release := make(chan struct{})
				var late atomic.Bool
				o = collect(t, releasing(p.Run(t.Context(), content, endsBefore(release, &late, inputs...), docinfer.Options{}), release))
				if late.Load() {
					t.Error("the group wasn't joined until the inputs ended")
				}
			} else {
				o = collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
			}
			inf := transcript(t, o, shas(win0.clip, win1.clip)...)
			checkTranscript(t, inf, docinfer.OutcomeTranscribed, joinedAt(t, []string{said0, said1}, 2), nil, false)
		})
	}
}

// A video plan record holds no windows and ends no group, even one whose content would read as a
// plan of one window: both windows join at the end of the inputs.
func TestRunVideoPlanEndsNoGroup(t *testing.T) {
	e := speakerOf(ai.TranscribeRoute, win0, win1).serve(t)
	p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, func(c *docinfer.Config) { c.Pending = 1 }))
	content := dispatch.NewMemoryContent()
	inputs := seq(planInput(t, content, dispatch.MediaVideoPlan, 1), windowInput(t, content, win0), windowInput(t, content, win1))
	o := collect(t, p.Run(t.Context(), content, inputs, docinfer.Options{}))
	inf := transcript(t, o, shas(win0.clip, win1.clip)...)
	checkTranscript(t, inf, docinfer.OutcomeTranscribed, joinedAt(t, []string{said0, said1}, 2), nil, false)
}

// The first event of an index wins: a later event of it, of other content, is counted a window
// and never transcribed. Windows arriving out of order are joined in index order.
func TestRunWindowPlacement(t *testing.T) {
	again := win{index: 0, clip: "clip 0 again", said: "not this one", after: "8-10"}
	e := speakerOf(ai.TranscribeRoute, win0, win1, win2, again).serve(t)
	p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil))
	content := dispatch.NewMemoryContent()
	inputs := seq(windowInput(t, content, win2), windowInput(t, content, win0), windowInput(t, content, again), windowInput(t, content, win1))
	o := collect(t, p.Run(t.Context(), content, inputs, docinfer.Options{}))
	inf := transcript(t, o, shas(win0.clip, win1.clip, win2.clip)...)
	checkTranscript(t, inf, docinfer.OutcomeTranscribed, joinedAt(t, []string{said0, said1, said2}, 2, 2), nil, false)
	for _, r := range e.Requests() {
		if strings.Contains(string(r.Body), again.clip) {
			t.Error("the repeated index was transcribed")
		}
	}
	checkCounts(t, o, transcriptCounts(o, 4, docinfer.TranscriptionCounts{Transcribed: 3, Requests: 3, Transcripts: 1, SeamsMatched: 2}, 3, false))
}

// A seam's seconds are the earlier window's MetaSeamAfter, or, without it, the later window's
// MetaSeamBefore: the windows' overlap is within a 2-second seam's bound and past a 1-second
// one's.
func TestRunSeamSeconds(t *testing.T) {
	cases := []struct {
		name          string
		after, before string
		matched       bool
	}{
		{"after decides", "8-10", "9-10", true},
		{"after decides, short", "9-10", "8-10", false},
		{"before alone", "", "8-10", true},
		{"before alone, short", "", "9-10", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w0, w1 := win0, win1
			w0.after, w1.before, w1.after = tc.after, tc.before, ""
			e := speakerOf(ai.TranscribeRoute, w0, w1).serve(t)
			// A Pending of 1 admits the plan's count before the first window, so the count, not
			// a window without MetaSeamAfter, ends the group.
			p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, func(c *docinfer.Config) { c.Pending = 1 }))
			content := dispatch.NewMemoryContent()
			o := collect(t, p.Run(t.Context(), content,
				seq(planInput(t, content, dispatch.MediaAudioPlan, 2), windowInput(t, content, w0), windowInput(t, content, w1)), docinfer.Options{}))
			seconds := 1.0
			if tc.matched {
				seconds = 2
			}
			tr := joinedAt(t, []string{said0, said1}, seconds)
			if tr.Seams[0].Matched != tc.matched {
				t.Fatalf("the test's windows at %v seconds: matched %t, want %t", seconds, tr.Seams[0].Matched, tc.matched)
			}
			checkTranscript(t, transcript(t, o, shas(w0.clip, w1.clip)...), docinfer.OutcomeTranscribed, tr, nil, false)
			want := docinfer.TranscriptionCounts{Transcribed: 2, Requests: 2, Transcripts: 1, SeamsUnmatched: 1}
			if tc.matched {
				want.SeamsMatched, want.SeamsUnmatched = 1, 0
			}
			checkCounts(t, o, transcriptCounts(o, 2, want, 2, false))
		})
	}
}

// A window refused, incomplete, failed on the last delivery, or never cut joins as "" and is
// listed in Failed; the transcript is failed when a window failed, refused when one was refused
// and none failed, and incomplete otherwise. A refusal and an incomplete transcript are held, a
// failure never is.
func TestRunWindowsNotTranscribed(t *testing.T) {
	refusedText := (&ai.APIError{Status: 400, Message: "clip refused"}).Error()
	cases := []struct {
		name    string
		mode    ai.TranscriptionMode
		replies map[string]aitest.Reply
		final   bool
		plan    int // windows the plan lists, 0 for no plan
		outcome docinfer.Outcome
		held1   *docinfer.Held // window 1's
		counts  docinfer.TranscriptionCounts
	}{
		{"refused and never cut", ai.TranscribeRoute, map[string]aitest.Reply{win1.clip: aitest.ErrorReply{Status: 400, Message: "clip refused"}},
			false, 4, docinfer.OutcomeRefused, &docinfer.Held{Refused: refusedText},
			docinfer.TranscriptionCounts{Transcribed: 2, Refused: 1, Requests: 3, Transcripts: 1, SeamsUnmatched: 3}},
		{"incomplete and never cut", ai.TranscribeChat, map[string]aitest.Reply{win1.clip: aitest.ChatReply{Text: "three four", Finish: "length"}},
			false, 4, docinfer.OutcomeIncomplete, &docinfer.Held{Incomplete: "max_output_tokens"},
			docinfer.TranscriptionCounts{Transcribed: 2, Incomplete: 1, Requests: 3, Transcripts: 1, SeamsUnmatched: 3}},
		{"failed on the last delivery", ai.TranscribeRoute, map[string]aitest.Reply{win1.clip: aitest.ErrorReply{Status: 500, Message: "broke"}},
			true, 0, docinfer.OutcomeFailed, nil,
			docinfer.TranscriptionCounts{Transcribed: 2, Failed: 1, Requests: 3, Transcripts: 1, SeamsUnmatched: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := speakerOf(tc.mode, win0, win1, win2)
			s.replies = tc.replies
			e := s.serve(t)
			cfg := transcriptionConfig(t, e, tc.mode, nil)
			content := dispatch.NewMemoryContent()
			w2 := win2
			w2.after = "28-30" // so only the plan, or the end of the inputs, ends the group
			inputs := []docinfer.Input{windowInput(t, content, win0), windowInput(t, content, win1), windowInput(t, content, w2)}
			texts, contents, failed := []string{said0, "", said2}, []string{win0.clip, win1.clip, win2.clip}, []int{1}
			if tc.plan > 0 {
				inputs = append(inputs, planInput(t, content, dispatch.MediaAudioPlan, tc.plan))
				texts, contents, failed = append(texts, ""), append(contents, ""), append(failed, 3)
			}
			o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(inputs...), docinfer.Options{Final: tc.final}))
			inf := transcript(t, o, shas(contents...)...)
			tr := joinedAt(t, texts, make([]float64, len(texts)-1)...)
			// The seams' seconds don't matter: every seam has an empty window beside it.
			checkTranscript(t, inf, tc.outcome, tr, failed, false)
			if h := heldWindow(t, cfg.Cache, tc.mode, win1.clip); !reflect.DeepEqual(h, tc.held1) {
				t.Errorf("window 1 held as %+v, want %+v", h, tc.held1)
			}
			for _, c := range o.chunks() {
				if c.source.Kind != "transcript" || c.source.Incomplete != string(tc.outcome) {
					t.Errorf("chunk source %+v, want kind transcript, incomplete for %s", c.source, tc.outcome)
				}
			}
			checkCounts(t, o, transcriptCounts(o, 3, tc.counts, 2, false))
		})
	}
}

// Two jobs transcribing one window at once send one request: the second waits for the first's
// transcript, holding no transcription slot, and counts it held, with no tokens.
func TestRunJoinsWindow(t *testing.T) {
	only := win{index: 0, clip: "the only clip", said: said0}
	s := speakerOf(ai.TranscribeRoute, only)
	s.gate = &aitest.Gate{}
	e := s.serve(t)
	p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil))
	waits := make(chan struct{}, 64)
	docinfer.OnWait(p, func() { waits <- struct{}{} })
	content := dispatch.NewMemoryContent()
	in := windowInput(t, content, only)

	leader := startRun(t, p, content, in, docinfer.Options{})
	s.gate.Await(t, 1)
	waiter := startRun(t, p, content, in, docinfer.Options{})
	awaitWaiters(t, waits, 1)
	s.gate.Open()

	lo, wo := leader.outcome(t), waiter.outcome(t)
	transcriptions := 0
	for _, r := range e.Requests() {
		if strings.HasSuffix(r.Path, "/audio/transcriptions") {
			transcriptions++
		}
	}
	if transcriptions != 1 {
		t.Errorf("%d transcription requests, want 1", transcriptions)
	}
	tr := joinedAt(t, []string{said0})
	checkTranscript(t, transcript(t, lo, sha(only.clip)), docinfer.OutcomeTranscribed, tr, nil, false)
	checkTranscript(t, transcript(t, wo, sha(only.clip)), docinfer.OutcomeTranscribed, tr, nil, true)
	if got := *wo.counts.Transcription; got != (docinfer.TranscriptionCounts{Transcribed: 1, Held: 1, Transcripts: 1}) ||
		*wo.counts.Tokens.Transcription != (docinfer.Usage{}) {
		t.Errorf("the waiter's transcription counts %+v, tokens %+v; want transcribed and held, no tokens", got, wo.counts.Tokens.Transcription)
	}
	if got := *lo.counts.Transcription; got != (docinfer.TranscriptionCounts{Transcribed: 1, Requests: 1, Transcripts: 1}) {
		t.Errorf("the leader's transcription counts %+v, want transcribed by its one request", got)
	}
}

// A window holds its input's slot only until its own transcript is in: a group of more windows
// than Pending, ended by its last window, is joined rather than waiting on windows it can't pull.
func TestRunWindowGroupWithinPending(t *testing.T) {
	e := speakerOf(ai.TranscribeRoute, win0, win1, win2).serve(t)
	p := newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, func(c *docinfer.Config) { c.Pending = 1 }))
	content := dispatch.NewMemoryContent()
	inputs := seq(windowInput(t, content, win0), windowInput(t, content, win1), windowInput(t, content, win2),
		outputInput(t, content, childPart, dispatch.TextOutput, plainText))
	o := collect(t, p.Run(t.Context(), content, inputs, docinfer.Options{}))
	checkTranscript(t, transcript(t, o, shas(win0.clip, win1.clip, win2.clip)...), docinfer.OutcomeTranscribed,
		joinedAt(t, []string{said0, said1, said2}, 2, 2), nil, false)
	if o.counts.Texts != 1 {
		t.Errorf("counts %+v, want the text after the windows chunked too", o.counts)
	}
}

// A window of a media type other than audio/wav or audio/mpeg, or whose Meta doesn't place it, is
// the input's defect: it stops the job, permanent, before anything is asked. With transcription
// skipped, it is counted and skipped.
func TestRunWindowDefects(t *testing.T) {
	cases := []struct {
		name string
		edit func(*docinfer.Input)
	}{
		{"media type", func(in *docinfer.Input) { in.Event.Output.MediaType = "audio/ogg" }},
		{"no index", func(in *docinfer.Input) { delete(in.Event.Output.Meta, dispatch.MetaWindow) }},
		{"negative index", func(in *docinfer.Input) { in.Event.Output.Meta[dispatch.MetaWindow] = "-1" }},
		{"bad seam", func(in *docinfer.Input) { in.Event.Output.Meta[dispatch.MetaSeamAfter] = "ten" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := speakerOf(ai.TranscribeRoute, win0).serve(t)
			content := dispatch.NewMemoryContent()
			in := windowInput(t, content, win0)
			tc.edit(&in)
			o := collect(t, newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil)).Run(t.Context(), content, seq(in), docinfer.Options{Final: true}))
			if o.err == nil || !docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want a permanent error", o.err)
			}
			if n := len(e.Requests()); n != 0 {
				t.Errorf("%d requests, want none", n)
			}
			o = collect(t, newPipeline(t, runConfig(t, e, nil)).Run(t.Context(), content, seq(in), docinfer.Options{}))
			if o.err != nil {
				t.Fatalf("Run with transcription skipped: %v", o.err)
			}
			checkCounts(t, o, docinfer.Counts{Windows: 1})
		})
	}
	// An audio/mpeg window is sent as window.mp3.
	mp3 := win0
	mp3.mediaType, mp3.after = "audio/mpeg", ""
	e := speakerOf(ai.TranscribeRoute, mp3).serve(t)
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil)).Run(t.Context(), content, seq(windowInput(t, content, mp3)), docinfer.Options{}))
	transcript(t, o, sha(mp3.clip))
	if body := string(e.Requests()[0].Body); !strings.Contains(body, `filename="window.mp3"`) {
		t.Errorf("an audio/mpeg window's upload %q, want it named window.mp3", body)
	}
}

// windowDefectError returns the error Run stops with over a window of audio/ogg.
func windowDefectError(t *testing.T) error {
	t.Helper()
	e := aitest.Serve(t, aitest.Routes{})
	content := dispatch.NewMemoryContent()
	w := win0
	w.mediaType = "audio/ogg"
	o := collect(t, newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil)).Run(t.Context(), content, seq(windowInput(t, content, w)), docinfer.Options{}))
	if o.err == nil {
		t.Fatal("Run over an audio/ogg window yielded no error")
	}
	return o.err
}

// A window whose content the store misses stops the job with a *MissingError, final or not.
func TestRunWindowMissingContent(t *testing.T) {
	e := speakerOf(ai.TranscribeRoute, win0).serve(t)
	only := win0
	only.after = ""
	o := collect(t, newPipeline(t, transcriptionConfig(t, e, ai.TranscribeRoute, nil)).Run(t.Context(),
		dispatch.NewMemoryContent(), seq(windowInput(t, nil, only)), docinfer.Options{Final: true}))
	if _, ok := errors.AsType[*docinfer.MissingError](o.err); !ok || docinfer.Permanent(o.err) {
		t.Errorf("Run: %v, want a *MissingError, not permanent", o.err)
	}
}

// runWindow runs a pipeline of cfg over one window, the last of its group, with opts.
func runWindow(t *testing.T, cfg docinfer.Config, opts docinfer.Options) outcome {
	t.Helper()
	only := win0
	only.after = ""
	content := dispatch.NewMemoryContent()
	return collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(windowInput(t, content, only)), opts))
}

// windowStopped fails the test unless the run stopped yielding nothing but its error, after at
// most one request, and held no transcript.
func windowStopped(t *testing.T, o outcome, e *aitest.Endpoint, cache docinfer.Cache, mode ai.TranscriptionMode) {
	t.Helper()
	if o.counts != nil || len(o.batches) > 0 || len(o.inferences) > 0 {
		t.Error("a stopped run yielded inferences, batches, or counts")
	}
	if n := len(e.Requests()); n > 1 {
		t.Errorf("%d requests, want at most the one", n)
	}
	if h := heldWindow(t, cache, mode, win0.clip); h != nil {
		t.Errorf("a stopped run held %+v", h)
	}
}

// A refused status, 400, 413, 415, or 422, is the window's outcome refused, its text the error's,
// held, and the transcript refused.
func TestRunWindowRefusedStatus(t *testing.T) {
	for _, status := range []int{400, 413, 415, 422} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := speakerOf(ai.TranscribeRoute)
			s.replies[win0.clip] = aitest.ErrorReply{Status: status, Message: "clip refused"}
			e := s.serve(t)
			cfg := transcriptionConfig(t, e, ai.TranscribeRoute, nil)
			o := runWindow(t, cfg, docinfer.Options{})
			checkTranscript(t, transcript(t, o, sha(win0.clip)), docinfer.OutcomeRefused, joinedAt(t, []string{""}), []int{0}, false)
			want := (&ai.APIError{Status: status, Message: "clip refused"}).Error()
			if h := heldWindow(t, cfg.Cache, ai.TranscribeRoute, win0.clip); h == nil || *h != (docinfer.Held{Refused: want}) {
				t.Errorf("held %+v, want the refusal", h)
			}
			checkCounts(t, o, transcriptCounts(o, 1, docinfer.TranscriptionCounts{Refused: 1, Requests: 1, Transcripts: 1}, 0, false))
		})
	}
}

// A permanent status, 401, 403, 404, 501, 505, or any other 4xx but 408 and 429, and a request
// the format can't send, stop the job, final or not, holding nothing, and Permanent reports it.
func TestRunWindowPermanent(t *testing.T) {
	for _, status := range []int{401, 403, 404, 405, 409, 418, 451, 501, 505} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				s := speakerOf(ai.TranscribeRoute)
				s.replies[win0.clip] = aitest.ErrorReply{Status: status, Message: "no"}
				e := s.serve(t)
				cfg := transcriptionConfig(t, e, ai.TranscribeRoute, nil)
				o := runWindow(t, cfg, docinfer.Options{Final: final})
				if apiErr, ok := errors.AsType[*ai.APIError](o.err); !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if !docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is false", o.err)
				}
				windowStopped(t, o, e, cfg.Cache, ai.TranscribeRoute)
			})
		}
	}
	// A chat-mode client whose format carries no audio, Messages, refuses before anything is sent.
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("unsupported final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
			cfg := transcriptionConfig(t, e, ai.TranscribeChat, func(c *docinfer.Config) {
				client, err := ai.New(e.URL(), http.DefaultClient, messages.New(messages.Options{}), ai.Options{TranscriptionMode: ai.TranscribeChat})
				if err != nil {
					t.Fatal(err)
				}
				c.Transcription.Client = client
			})
			o := runWindow(t, cfg, docinfer.Options{Final: final})
			if !docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want a permanent error", o.err)
			}
			if n := len(e.Requests()); n != 0 {
				t.Errorf("%d requests, want none sent", n)
			}
			windowStopped(t, o, e, cfg.Cache, ai.TranscribeChat)
		})
	}
}

// A transient status, 408, 429, or any 5xx but 500, 501, and 505, stops the job, final or not,
// its *ai.APIError's RetryAfter carried for the redelivery.
func TestRunWindowTransient(t *testing.T) {
	for _, status := range []int{408, 429, 502, 503, 504, 507, 599} {
		for _, final := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d final=%t", status, final), func(t *testing.T) {
				s := speakerOf(ai.TranscribeRoute)
				s.replies[win0.clip] = aitest.ErrorReply{Status: status, Message: "later", RetryAfter: "7"}
				e := s.serve(t)
				cfg := transcriptionConfig(t, e, ai.TranscribeRoute, nil)
				o := runWindow(t, cfg, docinfer.Options{Final: final})
				apiErr, ok := errors.AsType[*ai.APIError](o.err)
				if !ok || apiErr.Status != status {
					t.Fatalf("Run: %v, want the %d *ai.APIError", o.err, status)
				}
				if apiErr.RetryAfter != 7*time.Second {
					t.Errorf("RetryAfter %v, want 7s", apiErr.RetryAfter)
				}
				if docinfer.Permanent(o.err) {
					t.Errorf("Permanent(%v) is true", o.err)
				}
				windowStopped(t, o, e, cfg.Cache, ai.TranscribeRoute)
			})
		}
	}
}

// A transport failure, a *url.Error with no reply, stops the job, final or not, and isn't
// permanent.
func TestRunWindowTransportFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String() + "/v1"
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	for _, final := range []bool{false, true} {
		t.Run(fmt.Sprintf("final=%t", final), func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll})
			cfg := transcriptionConfig(t, e, ai.TranscribeRoute, func(c *docinfer.Config) {
				c.Transcription.Client = newClient(t, closed, ai.TranscribeRoute)
			})
			o := runWindow(t, cfg, docinfer.Options{Final: final})
			if _, ok := errors.AsType[*url.Error](o.err); !ok {
				t.Fatalf("Run against a closed port: %v, want a *url.Error", o.err)
			}
			if docinfer.Permanent(o.err) {
				t.Errorf("Permanent(%v) is true", o.err)
			}
			windowStopped(t, o, e, cfg.Cache, ai.TranscribeRoute)
		})
	}
}

// A 500, or a 2xx reply that doesn't decode, stops the job; on the last delivery the window is
// recorded failed instead, never held, and the transcript is failed.
func TestRunWindowFailed(t *testing.T) {
	cases := []struct {
		name  string
		reply aitest.Reply
	}{
		{"500", aitest.ErrorReply{Status: 500, Message: "broke"}},
		{"undecodable", aitest.RawReply{Body: []byte(`not JSON`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := speakerOf(ai.TranscribeRoute)
			s.replies[win0.clip] = tc.reply
			e := s.serve(t)
			cfg := transcriptionConfig(t, e, ai.TranscribeRoute, nil)
			o := runWindow(t, cfg, docinfer.Options{})
			if o.err == nil || docinfer.Permanent(o.err) {
				t.Fatalf("Run: %v, want an error not permanent", o.err)
			}
			windowStopped(t, o, e, cfg.Cache, ai.TranscribeRoute)

			cfg = transcriptionConfig(t, e, ai.TranscribeRoute, nil)
			o = runWindow(t, cfg, docinfer.Options{Final: true})
			checkTranscript(t, transcript(t, o, sha(win0.clip)), docinfer.OutcomeFailed, joinedAt(t, []string{""}), []int{0}, false)
			if h := heldWindow(t, cfg.Cache, ai.TranscribeRoute, win0.clip); h != nil {
				t.Errorf("a failed window was held: %+v", h)
			}
			checkCounts(t, o, transcriptCounts(o, 1, docinfer.TranscriptionCounts{Failed: 1, Requests: 1, Transcripts: 1}, 0, false))
		})
	}
}

// A chat-mode reply the model didn't complete is an *ai.IncompleteTranscriptError: the window is
// incomplete, joined as "", held with its reason, and a second run reads it back.
func TestRunWindowIncomplete(t *testing.T) {
	s := speakerOf(ai.TranscribeChat)
	s.replies[win0.clip] = aitest.ChatReply{Text: "one two", Finish: "content_filter"}
	e := s.serve(t)
	cfg := transcriptionConfig(t, e, ai.TranscribeChat, nil)
	o := runWindow(t, cfg, docinfer.Options{})
	tr := joinedAt(t, []string{""})
	checkTranscript(t, transcript(t, o, sha(win0.clip)), docinfer.OutcomeIncomplete, tr, []int{0}, false)
	if h := heldWindow(t, cfg.Cache, ai.TranscribeChat, win0.clip); h == nil || *h != (docinfer.Held{Incomplete: "content_filter"}) {
		t.Errorf("held %+v, want the window incomplete for content_filter", h)
	}
	checkCounts(t, o, transcriptCounts(o, 1, docinfer.TranscriptionCounts{Incomplete: 1, Requests: 1, Transcripts: 1}, 0, false))

	p := newPipeline(t, cfg)
	only := win0
	only.after = ""
	o = collect(t, p.Run(t.Context(), dispatch.NewMemoryContent(), seq(windowInput(t, nil, only)), docinfer.Options{}))
	checkTranscript(t, transcript(t, o, sha(win0.clip)), docinfer.OutcomeIncomplete, tr, []int{0}, true)
	checkCounts(t, o, transcriptCounts(o, 1, docinfer.TranscriptionCounts{Incomplete: 1, Held: 1, Transcripts: 1}, 0, true))
}

// openCounter is Run's content that counts each Open of the content sha256 names.
type openCounter struct {
	dispatch.Content
	sha256 string
	opens  atomic.Int64
}

func (c *openCounter) Open(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	if sha256 == c.sha256 {
		c.opens.Add(1)
	}
	return c.Content.Open(ctx, sha256)
}

// A window is opened by each attempt: with go-ai's retry policy, a 503 and then the transcript
// are two requests, each with the window's content opened afresh, and the window is transcribed.
func TestRunWindowOpenedPerAttempt(t *testing.T) {
	var attempts atomic.Int64
	e := aitest.Serve(t, aitest.Routes{Embeddings: embedAll, Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
		if string(req.File) != win0.clip {
			t.Errorf("a request for clip %q, want %q", req.File, win0.clip)
		}
		if attempts.Add(1) == 1 {
			return aitest.ErrorReply{Status: 503, Message: "loading", RetryAfter: "0"}
		}
		return aitest.TranscriptionReply{Text: win0.said, InputTokens: 5, OutputTokens: 2}
	}})
	cfg := transcriptionConfig(t, e, ai.TranscribeRoute, func(c *docinfer.Config) {
		client, err := ai.New(e.URL(), http.DefaultClient, completions.New(completions.Options{}),
			ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 3}})
		if err != nil {
			t.Fatal(err)
		}
		c.Transcription.Client = client
	})
	only := win0
	only.after = ""
	memory := dispatch.NewMemoryContent()
	in := windowInput(t, memory, only)
	content := &openCounter{Content: memory, sha256: sha(win0.clip)}

	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(in), docinfer.Options{}))
	checkTranscript(t, transcript(t, o, sha(win0.clip)), docinfer.OutcomeTranscribed, joinedAt(t, []string{win0.said}), nil, false)
	if n := attempts.Load(); n != 2 {
		t.Errorf("%d transcription requests, want 2: the 503 and its retry", n)
	}
	if n := content.opens.Load(); n != 2 {
		t.Errorf("the window's content opened %d times, want once per attempt, 2", n)
	}
}
