//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai"
)

// audioPart is the part phrase.wav is a window of.
var audioPart = dispatch.Part{ID: "/phrase.wav", Name: "phrase.wav", Size: 193678, SHA256: strings.Repeat("b", 64),
	Parent: "/", Depth: 1, Where: "phrase.wav"}

// TestTranscription runs phrase.wav through Run as an audio window, the one window of its group,
// placed at index 0 and without MetaSeamAfter, so it is its group's last: in route mode on the
// transcription role's endpoint, and in chat mode, through Chat Completions, on the audio role's.
// Each wants one transcript of outcome transcribed, no window failed, its text non-empty, asked
// of the model, and chunked as kind transcript and embedded at the configured dimensions. A
// second run over the same content is held whole, from the FileCache, asking no model.
func TestTranscription(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    role
		mode ai.TranscriptionMode
	}{
		{"route", transcription, ai.TranscribeRoute},
		{"chat", audio, ai.TranscribeChat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, model := endpoint(t, tc.r, ai.Options{TranscriptionMode: tc.mode})
			emb, dims := embeddings(t)
			p := newPipeline(t, emb, dims, func(c *docinfer.Config) {
				c.Transcription = &docinfer.Transcription{Client: client, Model: model}
			})
			content := dispatch.NewMemoryContent()
			in := docinfer.Input{Event: dispatch.Event{
				Part:  audioPart,
				Stage: stage,
				Output: &dispatch.Output{Kind: dispatch.AudioOutput, Name: "window", MediaType: "audio/wav",
					Meta: map[string]string{dispatch.MetaWindow: "0"}, SHA256: put(t, content, readTestdata(t, "phrase.wav"))},
			}}

			first := run(t, p, content, docinfer.Options{}, in)

			inf := first.inference(t, docinfer.CapabilityTranscription)
			if inf.Outcome != docinfer.OutcomeTranscribed || len(inf.Failed) != 0 || strings.TrimSpace(inf.Text) == "" || inf.Held {
				t.Fatalf("the transcript = %+v, want transcribed with text, no window failed, asked of the model", inf)
			}
			t.Logf("%s transcribes phrase.wav in %s mode: %q", model, tc.mode, inf.Text)
			checkChunks(t, first, dims, "transcript")
			if c := first.counts.Transcription; c == nil || c.Transcribed != 1 || c.Transcripts != 1 || c.Requests != 1 {
				t.Errorf("Counts.Transcription = %+v, want one window transcribed of one request, one transcript", c)
			}

			checkHeld(t, first, run(t, p, content, docinfer.Options{}, in))
		})
	}
}
