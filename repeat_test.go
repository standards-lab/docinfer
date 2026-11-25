package docinfer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

const (
	loopHead = "Table 3. Results by row."
	loopLine = "\n| row | 12.5 | 3.1 |"
)

var (
	loopText = loopHead + strings.Repeat(loopLine, 30)
	loopKept = loopHead + loopLine
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "repetition", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTrimRepeatLoops(t *testing.T) {
	kept := map[string]int{
		"17490794.2048.txt":    2019,
		"17490794.6144.txt":    1875,
		"17490794.t0.2048.txt": 1287,
		"27a02d36.2048.txt":    1161,
		"27a02d36.6144.txt":    2312,
		"27a02d36.t0.2048.txt": 1382,
		"2d3ae246.t0.2048.txt": 3752,
		"3fffd217.t0.2048.txt": 795,
		"533d359c.2048.txt":    533,
		"533d359c.t0.2048.txt": 547,
		"8a0a8bc4.2048.txt":    3154,
		"8a0a8bc4.t0.2048.txt": 1989,
	}
	files, err := filepath.Glob(filepath.Join("testdata", "repetition", "loop", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(kept) {
		t.Fatalf("%d loop fixtures, want %d", len(files), len(kept))
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			text := readFixture(t, filepath.Join("loop", name))
			got, ok := docinfer.TrimRepeat(text)
			if !ok || !strings.HasPrefix(text, got) {
				t.Fatalf("TrimRepeat: %t, a prefix %t; want the looping tail trimmed", ok, strings.HasPrefix(text, got))
			}
			if n := utf8.RuneCountInString(got); n != kept[name] {
				t.Errorf("kept %d of %d runes, want %d", n, utf8.RuneCountInString(text), kept[name])
			}
			if again, ok := docinfer.TrimRepeat(got); ok {
				t.Errorf("the trimmed text trims again, to %d runes", utf8.RuneCountInString(again))
			}
		})
	}
}

func TestTrimRepeatCompleteTexts(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "repetition", "complete", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 28 {
		t.Fatalf("%d complete fixtures, want 28", len(files))
	}
	for _, f := range files {
		name := filepath.Base(f)
		text := readFixture(t, filepath.Join("complete", name))
		if got, ok := docinfer.TrimRepeat(text); ok || got != text {
			t.Errorf("%s: trimmed %d runes of a text that doesn't loop", name, utf8.RuneCountInString(text)-utf8.RuneCountInString(got))
		}
	}
}

func TestTrimRepeat(t *testing.T) {
	cases := []struct {
		name, text, want string
		trimmed          bool
	}{
		{"a line repeated", loopText, loopKept, true},
		{"a short repeat", "Ha. " + strings.Repeat("ha ", 60), "Ha. " + strings.Repeat("ha ", 60), false},
		{"a cycle of runes", "Intro. " + strings.Repeat("→ é ", 100), "Intro. → é", true},
		{"a run of one rune", "Rule:" + strings.Repeat("-", 250), "Rule:-", true},
		{"two copies of a long line", "Intro." + strings.Repeat("\n"+strings.Repeat("x y ", 30), 2), "Intro.\n" + strings.Repeat("x y ", 30), true},
		{"nothing repeated", description, description, false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := docinfer.TrimRepeat(tc.text)
			if got != tc.want || ok != tc.trimmed {
				t.Errorf("TrimRepeat = %q, %t; want %q, %t", got, ok, tc.want, tc.trimmed)
			}
		})
	}
}

func TestRunImageRepetition(t *testing.T) {
	cases := []struct {
		name, finish, text, reason string
		outcome                    docinfer.Outcome
		counts                     docinfer.VisionCounts
	}{
		{"loop at max tokens", "length", loopKept, "repetition", docinfer.OutcomeIncomplete, docinfer.VisionCounts{Incomplete: 1, Requests: 1}},
		{"repeated lines, complete", "stop", loopText, "", docinfer.OutcomeDescribed, docinfer.VisionCounts{Described: 1, Requests: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: chatRoute(t, aitest.ChatReply{Text: loopText, Finish: tc.finish, PromptTokens: 11, CompletionTokens: 9}),
				Embeddings:      embedAll,
			})
			cfg := visionConfig(t, e, nil, nil)
			p := newPipeline(t, cfg)
			content := dispatch.NewMemoryContent()
			in := imageInput(t, content, imageBytes, imageType)
			o := collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
			if o.err != nil {
				t.Fatal(o.err)
			}
			inf := oneInference(t, o)
			if inf.Outcome != tc.outcome || inf.Reason != tc.reason || inf.Text != tc.text || inf.TextSHA256 != sha(tc.text) {
				t.Errorf("inference %+v, want %s for %q, its text %q", inf, tc.outcome, tc.reason, tc.text)
			}
			if h := heldImage(t, cfg.Cache); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: tc.text, Incomplete: tc.reason}) {
				t.Errorf("held %+v, want the text %q and the reason %q", h, tc.text, tc.reason)
			}
			chunks := o.chunks()
			if len(chunks) == 0 {
				t.Error("no chunks")
			}
			for _, c := range chunks {
				if c.source.SHA256 != sha(tc.text) || c.source.Incomplete != tc.reason {
					t.Errorf("chunk source %+v, want the text's SHA-256, incomplete for %q", c.source, tc.reason)
				}
			}
			if o.counts == nil || o.counts.Vision == nil || *o.counts.Vision != tc.counts {
				t.Errorf("vision counts %+v, want %+v", o.counts, tc.counts)
			}

			o = collect(t, p.Run(t.Context(), content, seq(in), docinfer.Options{}))
			if inf := oneInference(t, o); o.err != nil || inf.Outcome != tc.outcome || inf.Reason != tc.reason || inf.Text != tc.text || !inf.Held {
				t.Errorf("the second run: %v, inference %+v; want the held text", o.err, inf)
			}
		})
	}
}

func TestRunExtractionRepetition(t *testing.T) {
	head, unit := `{"rows":[`, `"a b",`
	chunks := []string{head}
	for range 60 {
		chunks = append(chunks, unit)
	}
	kept := head + unit
	e := aitest.Serve(t, aitest.Routes{
		ChatCompletions: extractRoute(t, plainText, aitest.ChatReply{Chunks: chunks, Finish: "length", CompletionTokens: extractTokens}),
		Embeddings:      embedAll,
	})
	cfg := extractConfig(t, e, nil)
	p := extractPipeline(t, cfg)
	o := runText(t, p, extracting(false))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if inf := oneExtraction(t, o, childPart, plainText); inf.Outcome != docinfer.OutcomeIncomplete ||
		inf.Reason != "repetition" || inf.Text != kept || inf.TextSHA256 != sha(kept) {
		t.Errorf("extraction %+v, want incomplete for repetition, its text %q", inf, kept)
	}
	if h := heldExtraction(t, cfg.Cache, plainText); h == nil || !reflect.DeepEqual(*h, docinfer.Held{Text: kept, Incomplete: "repetition"}) {
		t.Errorf("held %+v, want the trimmed text and repetition", h)
	}
}
