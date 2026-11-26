package transcribe_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer/acceptance/internal/rig"
	"github.com/standards-lab/docinfer/acceptance/internal/transcribe"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/rundir"
	"github.com/standards-lab/docproc/rundir/schema"
	"github.com/standards-lab/go-ai/aitest"
)

const (
	inputs        = 6
	windowsEach   = 2
	loopedInputs  = 2
	shortReply    = 60
	runawayReply  = 900
	cappedTokens  = 512
	looping       = "loop"
	audioPlanJSON = `{"duration":%d,"limit":30,"overlap":2,"windows":[%s],"seams":[]}`
)

func TestRunCappedAgainstUncapped(t *testing.T) {
	root := docprocRun(t)
	ep := aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if len(req.Messages) != 1 || len(req.Messages[0].Audio) != 1 {
				t.Errorf("request: %d messages", len(req.Messages))
				return aitest.ChatReply{}
			}
			words := shortReply
			if bytes.HasPrefix(req.Messages[0].Audio[0].Data, []byte(looping)) {
				words = runawayReply
			}
			finish := "stop"
			if req.MaxTokens > 0 && words > req.MaxTokens {
				words, finish = req.MaxTokens, "length"
			}
			return aitest.ChatReply{Text: strings.TrimSpace(strings.Repeat("word ", words)), Finish: finish,
				PromptTokens: 700, CompletionTokens: words, TotalTokens: 700 + words}
		},
	})
	dir := t.TempDir()
	out := filepath.Join(dir, "results.jsonl")
	for _, mt := range []int{cappedTokens, 0} {
		var log bytes.Buffer
		err := transcribe.Run(t.Context(), transcribe.Config{
			RunDir: root, Endpoint: rig.Endpoint{BaseURL: ep.URL()}, Model: "audio", Served: "served-audio",
			Concurrency: 4, MaxTokens: mt, N: 50, MaxWindows: 80, Seed: 1, Label: "max" + strconv.Itoa(mt),
			Out: out, Texts: filepath.Join(dir, "texts"), Log: &log,
		})
		if err != nil {
			t.Fatalf("max_tokens %d: %v\n%s", mt, err, log.String())
		}
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("results: %d lines, want 2", len(lines))
	}
	var capped, uncapped transcribe.Result
	if err := json.Unmarshal(lines[0], &capped); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &uncapped); err != nil {
		t.Fatal(err)
	}
	if capped.Windows != inputs*windowsEach || len(capped.WindowRecords) != capped.Windows || capped.Counts == nil || capped.Counts.Unplaced != 0 {
		t.Fatalf("capped: %d windows, %d records, counts %v; want %d windows", capped.Windows, len(capped.WindowRecords), capped.Counts, inputs*windowsEach)
	}
	incomplete := 0
	for _, w := range capped.WindowRecords {
		if w.Outcome != "incomplete" {
			continue
		}
		incomplete++
		if w.Finish != "length" || w.CompletionTokens != cappedTokens || w.ReplySHA256 == "" {
			t.Errorf("incomplete window %+v", w)
		}
		if _, err := os.Stat(filepath.Join(dir, "texts", capped.Label, w.SHA256+".reply.txt")); err != nil {
			t.Errorf("incomplete window %s: no reply text: %v", w.SHA256, err)
		}
	}
	if incomplete != loopedInputs || capped.Counts.Incomplete != loopedInputs {
		t.Errorf("capped: %d incomplete windows, counts %+v; want %d", incomplete, capped.Counts, loopedInputs)
	}
	for _, w := range uncapped.WindowRecords {
		if w.Outcome != "transcribed" {
			t.Errorf("uncapped window %+v, want transcribed", w)
		}
	}
	if uncapped.MaxCompletion != runawayReply {
		t.Errorf("uncapped: max completion %d, want %d", uncapped.MaxCompletion, runawayReply)
	}
}

func docprocRun(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "docproc")
	d, err := rundir.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Begin(schema.Manifest{})
	if err != nil {
		t.Fatal(err)
	}
	put := func(content []byte) string {
		h := sha256.Sum256(content)
		s := hex.EncodeToString(h[:])
		if err := d.Content().Put(t.Context(), s, bytes.NewReader(content)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	write := func(input string, out *dispatch.Output) {
		e := schema.Event{Kind: schema.OutputEvent, Input: input,
			Part:   dispatch.Part{ID: "/", Name: filepath.Base(input)},
			Stage:  dispatch.Identity{Name: "audio", Version: "1", Config: "a0"},
			Output: out}
		if err := r.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	for k := range inputs {
		input := fmt.Sprintf("audio/input-%d.wav", k)
		var windows []string
		for i := range windowsEach {
			clip := fmt.Sprintf("clip %d %d", k, i)
			if k < loopedInputs && i == 0 {
				clip = looping + " " + clip
			}
			meta := map[string]string{dispatch.MetaWindow: strconv.Itoa(i)}
			if i > 0 {
				meta[dispatch.MetaSeamBefore] = seam(i)
			}
			if i < windowsEach-1 {
				meta[dispatch.MetaSeamAfter] = seam(i + 1)
			}
			write(input, &dispatch.Output{Kind: dispatch.AudioOutput, Name: "window", MediaType: "audio/wav",
				Meta: meta, SHA256: put([]byte(clip))})
			windows = append(windows, fmt.Sprintf(`{"index":%d,"start":%d,"end":%d,"cut":"silence"}`, i, 10*i, 10*i+10))
		}
		plan := fmt.Sprintf(audioPlanJSON, 10*windowsEach, strings.Join(windows, ","))
		write(input, &dispatch.Output{Kind: dispatch.RecordOutput, Name: "plan", MediaType: dispatch.MediaAudioPlan,
			SHA256: put([]byte(plan))})
	}
	if err := r.End(nil); err != nil {
		t.Fatal(err)
	}
	return root
}

func seam(window int) string {
	return fmt.Sprintf("%d-%d", 10*window-2, 10*window)
}
