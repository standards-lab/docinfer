package docinfer_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

var (
	videoPart = dispatch.Part{ID: "/talk.mp4", Name: "talk.mp4", Size: 8192, SHA256: strings.Repeat("e", 64),
		Parent: "/", Depth: 1, Where: "talk.mp4"}
	videoStage = dispatch.Identity{Name: "video", Version: "1", Config: "v0"}
)

const (
	titleCard   = "A title card.\n\nWhite text on black."
	deskScene   = "A man at a desk."
	creditsRoll = "Credits roll."
	frameRefuse = "I can't describe this frame."
)

type shot struct {
	image string
	at    string
}

var (
	shotTitle   = shot{image: "frame title", at: "0.5"}
	shotDesk    = shot{image: "frame desk", at: "8"}
	shotCredits = shot{image: "frame credits", at: "3725.2"}
	shotRefused = shot{image: "frame refused", at: "12"}
)

func frameInput(t *testing.T, content *dispatch.MemoryContent, s shot, index int) docinfer.Input {
	t.Helper()
	if err := content.Put(t.Context(), sha(s.image), strings.NewReader(s.image)); err != nil {
		t.Fatal(err)
	}
	meta := map[string]string{dispatch.MetaFrame: strconv.Itoa(index), dispatch.MetaReason: "scene"}
	if s.at != "" {
		meta[dispatch.MetaTime] = s.at
	}
	return docinfer.Input{Event: dispatch.Event{
		Part:   videoPart,
		Stage:  videoStage,
		Output: &dispatch.Output{Kind: dispatch.ImageOutput, Name: "frame", MediaType: imageType, Meta: meta, SHA256: sha(s.image)},
	}}
}

func videoWindowInput(t *testing.T, content *dispatch.MemoryContent, w win, start, end string) docinfer.Input {
	t.Helper()
	in := windowInput(t, content, w)
	in.Event.Part, in.Event.Stage = videoPart, videoStage
	in.Event.Output.Meta[dispatch.MetaStart] = start
	in.Event.Output.Meta[dispatch.MetaEnd] = end
	return in
}

func ownEvent(part dispatch.Part, stage dispatch.Identity) docinfer.Input {
	return docinfer.Input{Event: dispatch.Event{Part: part, Stage: stage, Outcome: dispatch.Prepared, Outputs: 1}}
}

type timelineServer struct {
	t           *testing.T
	descs       map[string]string
	said        map[string]string
	extract     func(text string) aitest.Reply
	mu          sync.Mutex
	extractions []string
}

func (s *timelineServer) serve(t *testing.T) *aitest.Endpoint {
	t.Helper()
	s.t = t
	return aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if !req.Stream {
				if d, ok := s.descs[string(req.Messages[0].Images[0].Data)]; ok {
					return aitest.ChatReply{Text: d}
				}
				return aitest.ChatReply{Refusal: frameRefuse}
			}
			text := req.Messages[len(req.Messages)-1].Text
			s.mu.Lock()
			s.extractions = append(s.extractions, text)
			s.mu.Unlock()
			if s.extract != nil {
				return s.extract(text)
			}
			checkExtractRequest(t, req, text)
			return aitest.ChatReply{Chunks: []string{`{"total":1}`}, PromptTokens: 11, CompletionTokens: 3}
		},
		Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
			return aitest.TranscriptionReply{Text: s.said[string(req.File)]}
		},
		Embeddings: embedAll,
	})
}

func (s *timelineServer) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.extractions)
}

func timelineConfig(t *testing.T, e *aitest.Endpoint, edit func(*docinfer.Config)) docinfer.Config {
	t.Helper()
	client := newClient(t, e.URL(), ai.TranscribeRoute)
	return extractConfig(t, e, func(c *docinfer.Config) {
		c.Vision = &docinfer.Vision{Client: client, Model: visionModel, Served: visionServed, MaxTokens: visionTokens}
		c.Transcription = &docinfer.Transcription{Client: client, Model: transcriptionModel, Served: transcriptionServed}
		if edit != nil {
			edit(c)
		}
	})
}

func timelining() docinfer.Options { return docinfer.Options{Timeline: new(theExtract)} }

func timelines(o outcome) []docinfer.Inference {
	var out []docinfer.Inference
	for _, inf := range o.inferences {
		if inf.Scope == docinfer.ScopeTimeline {
			out = append(out, inf)
		}
	}
	return out
}

func resultIndex(o outcome, match func(docinfer.Result) bool) int {
	return slices.IndexFunc(o.all, match)
}

func videoInputs(t *testing.T, content *dispatch.MemoryContent) []docinfer.Input {
	t.Helper()
	return []docinfer.Input{
		planInput(t, content, dispatch.MediaVideoPlan, 0),
		frameInput(t, content, shotTitle, 0),
		videoWindowInput(t, content, win{index: 0, clip: "clip 0", said: said0, after: "8-10"}, "0", "10"),
		frameInput(t, content, shotDesk, 1),
		videoWindowInput(t, content, win{index: 1, clip: "clip 1", said: said1, before: "8-10"}, "8", "19.5"),
		frameInput(t, content, shotRefused, 2),
		frameInput(t, content, shotCredits, 3),
		ownEvent(videoPart, videoStage),
	}
}

func videoServer() *timelineServer {
	return &timelineServer{
		descs: map[string]string{shotTitle.image: titleCard, shotDesk.image: deskScene, shotCredits.image: creditsRoll},
		said:  map[string]string{"clip 0": said0, "clip 1": said1},
	}
}

const videoTimeline = "[0:00–0:10] heard: one two three four\n" +
	"[0:00] seen: A title card. White text on black.\n" +
	"[0:08–0:19] heard: five six seven eight nine ten\n" +
	"[0:08] seen: A man at a desk.\n" +
	"[1:02:05] seen: Credits roll."

func TestRunTimeline(t *testing.T) {
	s := videoServer()
	e := s.serve(t)
	cfg := timelineConfig(t, e, nil)
	p := newPipeline(t, cfg)
	content := dispatch.NewMemoryContent()
	inputs := videoInputs(t, content)
	o := collect(t, p.Run(t.Context(), content, seq(inputs...), timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if got := s.asked(); !slices.Equal(got, []string{videoTimeline}) {
		t.Fatalf("extracted %q, want the timeline\n%s", got, videoTimeline)
	}
	tls := timelines(o)
	if len(tls) != 1 {
		t.Fatalf("%d timeline inferences, want 1", len(tls))
	}
	tr := joinedAt(t, []string{said0, said1}, 2)
	if len(tr.Seams) != 1 || tr.Seams[0].Position != 4 {
		t.Fatalf("the join's seams %+v, want the second window's words from word 4", tr.Seams)
	}
	transcriptText := tr.Text()
	inf := tls[0]
	want := []string{sha(transcriptText), sha(titleCard), sha(deskScene), sha(creditsRoll)}
	if inf.Capability != docinfer.CapabilityExtraction || inf.Part != videoPart.ID || inf.Outcome != docinfer.OutcomeExtracted ||
		inf.Text != `{"total":1}` || !slices.Equal(inf.ContentSHA256, want) || inf.Held || len(inf.Failed) != 0 ||
		inf.Usage != (docinfer.Usage{Input: 11, Output: 3}) {
		t.Errorf("timeline inference %+v, want extracted of part %s from %v", inf, videoPart.ID, want)
	}
	if got := deltas(t, o, videoTimeline); !slices.Equal(got, []string{`{"total":1}`}) {
		t.Errorf("progress %q, want the timeline's one delta", got)
	}

	own := len(inputs) - 1
	at := resultIndex(o, func(r docinfer.Result) bool { return r.Inference != nil && r.Inference.Scope == docinfer.ScopeTimeline })
	echo := resultIndex(o, func(r docinfer.Result) bool { return r.Input != nil && r.Input.Event.Output == nil })
	settled := resultIndex(o, func(r docinfer.Result) bool { return r.Settled != nil && *r.Settled == own })
	if echo < 0 || settled < 0 || echo >= at || at >= settled {
		t.Errorf("the own event's echo at %d, the timeline at %d, the own event settled at %d; want them in that order", echo, at, settled)
	}
	for i, r := range o.all {
		if inf := r.Inference; inf != nil && inf.Capability != docinfer.CapabilityExtraction && i > at {
			t.Errorf("the %s inference of %v at %d, after the timeline at %d", inf.Capability, inf.ContentSHA256, i, at)
		}
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: 1}, docinfer.Usage{Input: 11, Output: 3})

	first := len(e.Requests())
	o = collect(t, p.Run(t.Context(), content, seq(videoInputs(t, content)...), timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if tls := timelines(o); len(tls) != 1 || !tls[0].Held || tls[0].Text != `{"total":1}` || tls[0].Usage != (docinfer.Usage{}) {
		t.Errorf("the held timeline %+v, want one, held, with no usage", tls)
	}
	if n := len(e.Requests()); n != first {
		t.Errorf("%d requests after the cached rerun, want the first run's %d", n, first)
	}
	if len(o.progress) != 0 {
		t.Errorf("a held timeline yielded progress %+v", o.progress)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Held: 1}, docinfer.Usage{})
}

func TestRunTimelineOfNoAudioPart(t *testing.T) {
	s := &timelineServer{said: map[string]string{"clip 0": said0, "clip 1": said1, "clip 2": said2}}
	e := s.serve(t)
	content := dispatch.NewMemoryContent()
	inputs := seq(windowInput(t, content, win0), windowInput(t, content, win1), windowInput(t, content, win2),
		ownEvent(audioPart, audioStage))
	o := collect(t, newPipeline(t, timelineConfig(t, e, nil)).Run(t.Context(), content, inputs, timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if n := len(timelines(o)); n != 0 || len(s.asked()) != 0 {
		t.Errorf("%d timelines, extractions %q, of an audio part; want none", n, s.asked())
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{}, docinfer.Usage{})
}

func TestRunTimelineOfNothingSeenOrHeard(t *testing.T) {
	s := &timelineServer{said: map[string]string{"clip 0": ""}}
	e := s.serve(t)
	content := dispatch.NewMemoryContent()
	inputs := seq(frameInput(t, content, shotRefused, 0),
		videoWindowInput(t, content, win{index: 0, clip: "clip 0"}, "0", "10"), ownEvent(videoPart, videoStage))
	o := collect(t, newPipeline(t, timelineConfig(t, e, nil)).Run(t.Context(), content, inputs, timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if n := len(timelines(o)); n != 0 || len(s.asked()) != 0 {
		t.Errorf("%d timelines, extractions %q, of a video with no description and no words; want none", n, s.asked())
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{}, docinfer.Usage{})
}

func TestRunTimelineWithoutOwnEvent(t *testing.T) {
	s := videoServer()
	e := s.serve(t)
	content := dispatch.NewMemoryContent()
	inputs := videoInputs(t, content)
	o := collect(t, newPipeline(t, timelineConfig(t, e, nil)).Run(t.Context(), content, seq(inputs[:len(inputs)-1]...), timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if got := s.asked(); !slices.Equal(got, []string{videoTimeline}) || len(timelines(o)) != 1 {
		t.Errorf("extracted %q, %d timelines; want the timeline once the inputs end", got, len(timelines(o)))
	}
}

func TestRunTimelineWindows(t *testing.T) {
	const bound = 100
	s := videoServer()
	s.extract = func(text string) aitest.Reply {
		if strings.HasPrefix(text, "Part 1:\n") {
			return aitest.ChatReply{Chunks: []string{`{"total":9}`}, PromptTokens: 7, CompletionTokens: 2}
		}
		return aitest.ChatReply{Chunks: []string{`{"total":1}`}, PromptTokens: 5, CompletionTokens: 1}
	}
	e := s.serve(t)
	cfg := timelineConfig(t, e, func(c *docinfer.Config) { c.Extraction.WindowRunes = bound })
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(videoInputs(t, content)...), timelining()))
	if o.err != nil {
		t.Fatal(o.err)
	}
	asked := s.asked()
	var windows, combines []string
	for _, text := range asked {
		if strings.HasPrefix(text, "Part 1:\n") {
			combines = append(combines, text)
		} else {
			windows = append(windows, text)
		}
	}
	if len(windows) < 2 || len(combines) != 1 {
		t.Fatalf("%d window calls, %d combines; want the timeline windowed and combined once: %q", len(windows), len(combines), asked)
	}
	for line := range strings.SplitSeq(videoTimeline, "\n") {
		if !slices.ContainsFunc(windows, func(w string) bool { return strings.Contains(w, line) }) {
			t.Errorf("no window holds the line %q", line)
		}
	}
	for _, w := range windows {
		if n := len([]rune(w)); n > bound {
			t.Errorf("window %q of %d runes, past %d", w, n, bound)
		}
	}
	tls := timelines(o)
	if len(tls) != 1 {
		t.Fatalf("%d timelines, want 1", len(tls))
	}
	n := len(windows)
	usage := docinfer.Usage{Input: 5*n + 7, Output: n + 2}
	if inf := tls[0]; inf.Text != `{"total":9}` || inf.Outcome != docinfer.OutcomeExtracted || inf.Usage != usage {
		t.Errorf("timeline %+v, want the combine's reply, extracted, usage %+v", inf, usage)
	}
	for _, p := range o.progress {
		if !slices.ContainsFunc(asked, func(text string) bool { return sha(text) == p.ContentSHA256 }) {
			t.Errorf("progress of %s, no call's input", p.ContentSHA256)
		}
	}
	if len(o.progress) != n+1 {
		t.Errorf("%d progress results, want one per call, %d", len(o.progress), n+1)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: n + 1}, usage)
}

func TestRunTimelineBesideDocument(t *testing.T) {
	s := videoServer()
	e := s.serve(t)
	content := dispatch.NewMemoryContent()
	opts := docinfer.Options{Extract: new(theExtract), Document: new(theExtract), Timeline: new(theExtract)}
	o := collect(t, newPipeline(t, timelineConfig(t, e, nil)).Run(t.Context(), content, seq(videoInputs(t, content)...), opts))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if !slices.Contains(s.asked(), videoTimeline) || len(timelines(o)) != 1 {
		t.Errorf("extracted %q, %d timelines; want the timeline among them", s.asked(), len(timelines(o)))
	}
	const sources = 4
	inf := theDocument(t, o)
	if len(inf.ContentSHA256) != sources {
		t.Errorf("document of %v, want the three descriptions and the transcript", inf.ContentSHA256)
	}
	if n := len(extractions(o)); n != sources+2 {
		t.Errorf("%d extractions, want each source's, the timeline's, and the document's, %d", n, sources+2)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: sources + 2, Requests: sources + 2},
		docinfer.Usage{Input: 11 * (sources + 2), Output: 3 * (sources + 2)})
}

func TestRunTimelineFrameWithoutTime(t *testing.T) {
	s := videoServer()
	e := s.serve(t)
	content := dispatch.NewMemoryContent()
	inputs := seq(frameInput(t, content, shot{image: shotTitle.image}, 0), ownEvent(videoPart, videoStage))
	o := collect(t, newPipeline(t, timelineConfig(t, e, nil)).Run(t.Context(), content, inputs, timelining()))
	if !docinfer.Permanent(o.err) || !strings.Contains(o.err.Error(), dispatch.MetaTime) {
		t.Errorf("Run: %v, want a permanent error naming the frame's time", o.err)
	}
}
