package docinfer_test

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

func TestRunCountsLayerWords(t *testing.T) {
	cases := []struct {
		name, text string
		want       int
	}{
		{"empty", "", 0},
		{"whitespace and punctuation", " \n\t-- . , ; ! ", 0},
		{"plain words", "A red square on a white page.", 7},
		{"digits are words", "Page 12 of 340", 4},
		{"punctuation splits", "state-of-the-art, e.g. x/y", 8},
		{"letters beside digits", "A4 v2.0 2026-11-27", 6},
		{"other scripts", "Größe 文書 Привет мир", 4},
		{"symbols split", "café + naïve = $5 €", 3},
		{"markdown", "# Title\n\n- **bold** item\n- `code`", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, threshold := range []int{tc.want, tc.want + 1} {
				if threshold == 0 {
					continue
				}
				e := serveLayers(t, aitest.ChatReply{Text: description}, nil)
				content := dispatch.NewMemoryContent()
				inputs := seq(layerText(t, content, "", pdfPart, pdfStage, 1, tc.text), layerImage(t, content, "", pdfPart, pdfStage, 1))

				o := collect(t, newPipeline(t, layerConfig(t, e, threshold, 1, false)).Run(t.Context(), content, inputs, docinfer.Options{}))
				if o.err != nil {
					t.Fatal(o.err)
				}
				if got, want := o.counts.Vision.Skipped == 1, threshold <= tc.want; got != want {
					t.Errorf("%q at LayerWords %d: skipped %t, want %t, the text holding %d words", tc.text, threshold, got, want, tc.want)
				}
			}
		})
	}
}

func TestLayerWordsKeysNothing(t *testing.T) {
	held := docinfer.Key(docinfer.Vision{Model: "m", Served: "s", MaxTokens: 1}.Identity(), sha("content"))
	for _, n := range []int{1, 40, 1000} {
		if k := docinfer.Key(docinfer.Vision{Model: "m", Served: "s", MaxTokens: 1, LayerWords: n}.Identity(), sha("content")); k != held {
			t.Errorf("LayerWords %d keys %s, want the held description's %s", n, k, held)
		}
	}
	unset := validConfig(t).Vision.Identity()
	for _, n := range []int{1, 40} {
		cfg := validConfig(t)
		cfg.Vision.LayerWords = n
		p, err := docinfer.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := *p.Profile().Vision, unset; got != want {
			t.Errorf("LayerWords %d: Profile.Vision %+v, want %+v", n, got, want)
		}
	}
}

const (
	layerThreshold = 5
	atLayer        = "one two three four five"
	belowLayer     = "one two three four"
	aboveLayer     = "The scanned page holds a full paragraph of text."
)

var pdfChild = dispatch.Part{ID: "/scan%2Fattached.pdf", Name: "attached.pdf", Size: 4096, SHA256: strings.Repeat("9", 64),
	Parent: "/", Depth: 1, Where: "scan/attached.pdf"}

func layerMeta(page int) map[string]string {
	return map[string]string{dispatch.MetaPage: strconv.Itoa(page), dispatch.MetaCount: "2"}
}

func layerText(t *testing.T, content *dispatch.MemoryContent, origin string, part dispatch.Part, stage dispatch.Identity, page int, text string) docinfer.Input {
	t.Helper()
	in := outputInput(t, content, part, dispatch.TextOutput, text)
	in.Event.Stage, in.Origin, in.Event.Output.Meta = stage, origin, layerMeta(page)
	return in
}

func layerImage(t *testing.T, content *dispatch.MemoryContent, origin string, part dispatch.Part, stage dispatch.Identity, page int) docinfer.Input {
	t.Helper()
	data := fmt.Appendf(slices.Clone(imageBytes), " %s %s %s page %d", origin, part.ID, stage.Name, page)
	in := imageInput(t, content, data, imageType)
	in.Event.Part, in.Event.Stage, in.Origin, in.Event.Output.Meta = part, stage, origin, layerMeta(page)
	return in
}

type layerEndpoint struct {
	*aitest.Endpoint
	vision atomic.Int64
}

func serveLayers(t *testing.T, vision aitest.Reply, extract func(aitest.ChatRequest) aitest.Reply) *layerEndpoint {
	t.Helper()
	e := &layerEndpoint{}
	e.Endpoint = aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			if req.Stream && extract != nil {
				return extract(req)
			}
			e.vision.Add(1)
			return vision
		},
		Embeddings: embedAll,
	})
	return e
}

func layerConfig(t *testing.T, e *layerEndpoint, threshold, pending int, extract bool) docinfer.Config {
	t.Helper()
	return visionConfig(t, e.Endpoint, nil, func(c *docinfer.Config) {
		c.Vision.LayerWords, c.Pending = threshold, pending
		if extract {
			x := extractConfig(t, e.Endpoint, nil)
			c.Extraction = x.Extraction
		}
	})
}

func visionInferences(o outcome) []docinfer.Inference {
	var out []docinfer.Inference
	for _, inf := range o.inferences {
		if inf.Capability == docinfer.CapabilityVision {
			out = append(out, inf)
		}
	}
	return out
}

func TestRunSkipsPagesWithALayer(t *testing.T) {
	type build func(*testing.T, *dispatch.MemoryContent) []docinfer.Input
	cases := []struct {
		name      string
		threshold int
		inputs    build
		described []int
	}{
		{"at the threshold", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, atLayer), layerImage(t, c, "", pdfPart, pdfStage, 1)}
		}, nil},
		{"above the threshold", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, pdfStage, 1)}
		}, nil},
		{"below the threshold", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, belowLayer), layerImage(t, c, "", pdfPart, pdfStage, 1)}
		}, []int{1}},
		{"a page with no layer", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{
				layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, pdfStage, 1),
				layerImage(t, c, "", pdfPart, pdfStage, 2),
			}
		}, []int{2}},
		{"another page's text", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, pdfStage, 2)}
		}, []int{1}},
		{"a TIFF page of the picture stage", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{
				layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, jpegStage, 1),
				layerImage(t, c, "", pdfPart, jpegStage, 2),
			}
		}, []int{1, 2}},
		{"a text of another stage", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, jpegStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, jpegStage, 1)}
		}, []int{1}},
		{"text and image of different parts", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfChild, pdfStage, 1)}
		}, []int{1}},
		{"text and image of different inputs", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "a.pdf", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "b.pdf", pdfPart, pdfStage, 1)}
		}, []int{1}},
		{"inputs interleaved", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{
				layerText(t, c, "a.pdf", pdfPart, pdfStage, 1, aboveLayer), layerText(t, c, "b.pdf", pdfPart, pdfStage, 1, atLayer),
				layerImage(t, c, "a.pdf", pdfPart, pdfStage, 1), layerImage(t, c, "b.pdf", pdfPart, pdfStage, 1),
			}
		}, nil},
		{"an image before its text", layerThreshold, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerImage(t, c, "", pdfPart, pdfStage, 1), layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer)}
		}, []int{0}},
		{"a threshold of 0", 0, func(t *testing.T, c *dispatch.MemoryContent) []docinfer.Input {
			return []docinfer.Input{layerText(t, c, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, c, "", pdfPart, pdfStage, 1)}
		}, []int{1}},
	}
	for _, tc := range cases {
		for _, pending := range []int{1, 4} {
			t.Run(fmt.Sprintf("%s pending=%d", tc.name, pending), func(t *testing.T) {
				e := serveLayers(t, aitest.ChatReply{Text: description, PromptTokens: 11, CompletionTokens: 7}, nil)
				cfg := layerConfig(t, e, tc.threshold, pending, false)
				content := dispatch.NewMemoryContent()
				inputs := tc.inputs(t, content)

				o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
				if o.err != nil {
					t.Fatal(o.err)
				}
				if len(o.inputs) != len(inputs) {
					t.Fatalf("%d inputs echoed, want %d", len(o.inputs), len(inputs))
				}
				var images, texts int
				var want, skipped []string
				for i, in := range inputs {
					switch in.Event.Output.Kind {
					case dispatch.TextOutput:
						texts++
					case dispatch.ImageOutput:
						images++
						if slices.Contains(tc.described, i) {
							want = append(want, in.Event.Output.SHA256)
						} else {
							skipped = append(skipped, in.Event.Output.SHA256)
						}
					}
				}
				if n := int(e.vision.Load()); n != len(want) {
					t.Errorf("%d vision requests, want %d", n, len(want))
				}
				var got []string
				for _, inf := range visionInferences(o) {
					if inf.Outcome != docinfer.OutcomeDescribed {
						t.Errorf("inference %+v, want described", inf)
					}
					got = append(got, inf.ContentSHA256...)
				}
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) {
					t.Errorf("inferences of images %v, want %v", got, want)
				}
				for _, s := range skipped {
					values, err := cfg.Cache.Get(t.Context(), docinfer.Key(visionID, s))
					if err != nil {
						t.Fatal(err)
					}
					if values[0] != nil {
						t.Errorf("skipped image %s held %s", s, values[0])
					}
				}
				c := o.counts
				if c == nil {
					t.Fatal("Run yielded no counts")
				}
				wantVision := docinfer.VisionCounts{Described: len(want), Requests: len(want), Skipped: len(skipped)}
				if *c.Vision != wantVision || c.Images != images || c.Texts != texts {
					t.Errorf("counts: vision %+v, images %d, texts %d; want %+v, %d, %d", *c.Vision, c.Images, c.Texts, wantVision, images, texts)
				}
				outputs := map[string]bool{}
				for _, p := range o.chunks() {
					if p.source.Kind == "output" {
						outputs[p.source.Output.SHA256] = true
					}
				}
				if len(outputs) != texts {
					t.Errorf("chunks of %d text outputs, want each of the %d", len(outputs), texts)
				}
			})
		}
	}
}

func TestRunSkipExtractsTheLayer(t *testing.T) {
	e := serveLayers(t, aitest.ChatReply{Text: description}, func(req aitest.ChatRequest) aitest.Reply {
		checkExtractRequest(t, req, aboveLayer)
		return aitest.ChatReply{Chunks: extractPieces, PromptTokens: 20, CompletionTokens: 9}
	})
	cfg := layerConfig(t, e, layerThreshold, 4, true)
	content := dispatch.NewMemoryContent()
	inputs := seq(layerText(t, content, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, content, "", pdfPart, pdfStage, 1))

	o := collect(t, extractPipeline(t, cfg).Run(t.Context(), content, inputs, extracting(false)))
	if o.err != nil {
		t.Fatal(o.err)
	}
	if n := e.vision.Load(); n != 0 {
		t.Errorf("%d vision requests, want none", n)
	}
	if infs := visionInferences(o); len(infs) != 0 {
		t.Errorf("vision inferences %+v, want none", infs)
	}
	if inf := oneExtraction(t, o, pdfPart, aboveLayer); inf.Outcome != docinfer.OutcomeExtracted || inf.Text != extractJSON {
		t.Errorf("extraction %+v, want the layer's extracted", inf)
	}
	checkExtractionCounts(t, o, docinfer.ExtractionCounts{Extracted: 1, Requests: 1}, docinfer.Usage{Input: 20, Output: 9})
	if *o.counts.Vision != (docinfer.VisionCounts{Skipped: 1}) || o.counts.Texts != 1 || o.counts.Images != 1 || o.counts.Embedded == 0 {
		t.Errorf("counts %+v, vision %+v; want the image skipped and the text chunked", o.counts, *o.counts.Vision)
	}
}

func TestRunSkipOnRedelivery(t *testing.T) {
	e := serveLayers(t, aitest.ErrorReply{Status: 500, Message: "broke"}, nil)
	p := newPipeline(t, layerConfig(t, e, layerThreshold, 4, false))
	content := dispatch.NewMemoryContent()
	inputs := []docinfer.Input{
		layerText(t, content, "", pdfPart, pdfStage, 1, aboveLayer), layerImage(t, content, "", pdfPart, pdfStage, 1),
		layerImage(t, content, "", pdfPart, pdfStage, 2),
	}

	o := collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{}))
	if o.err == nil || docinfer.Permanent(o.err) {
		t.Fatalf("Run: %v, want an error not permanent", o.err)
	}
	if n := e.vision.Load(); n != 1 {
		t.Errorf("%d vision requests, want page 2's alone", n)
	}

	for range 2 {
		before := e.vision.Load()
		o = collect(t, p.Run(t.Context(), content, seq(inputs...), docinfer.Options{Final: true}))
		if o.err != nil {
			t.Fatalf("the final Run: %v, want the job to go on", o.err)
		}
		if n := e.vision.Load() - before; n != 1 {
			t.Errorf("the final Run sent %d vision requests, want page 2's alone", n)
		}
		infs := visionInferences(o)
		if len(infs) != 1 || infs[0].Outcome != docinfer.OutcomeFailed ||
			!slices.Equal(infs[0].ContentSHA256, []string{inputs[2].Event.Output.SHA256}) {
			t.Errorf("vision inferences %+v, want page 2's failed alone", infs)
		}
		if *o.counts.Vision != (docinfer.VisionCounts{Failed: 1, Requests: 1, Skipped: 1}) || o.counts.Texts != 1 || o.counts.Images != 2 {
			t.Errorf("counts %+v, vision %+v; want page 1 skipped, page 2 failed, the text chunked", o.counts, *o.counts.Vision)
		}
	}
}
