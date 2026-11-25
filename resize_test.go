package docinfer_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/go-ai/aitest"
)

func TestBoxResize(t *testing.T) {
	checker := image.NewGray(image.Rect(0, 0, 4, 2))
	for i := range checker.Pix {
		if (i%4+i/4)%2 == 0 {
			checker.Pix[i] = 255
		}
	}
	got := docinfer.BoxResize(checker, 2, 1).(*image.Gray)
	if want := []uint8{128, 128}; !bytes.Equal(got.Pix, want) || got.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Errorf("a 4×2 checkerboard to 2×1: %v of %v, want %v", got.Pix, got.Bounds(), want)
	}

	ramp := image.NewGray(image.Rect(5, 5, 8, 6))
	copy(ramp.Pix, []uint8{0, 90, 180})
	if got := docinfer.BoxResize(ramp, 2, 1).(*image.Gray); !bytes.Equal(got.Pix, []uint8{30, 150}) {
		t.Errorf("0, 90, 180 to two pixels: %v, want each 1.5 pixels' average, 30 and 150", got.Pix)
	}

	colors := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	red, blue := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}
	for y := range 2 {
		for x := range 4 {
			c := red
			if (x+y)%2 == 1 {
				c = blue
			}
			colors.Set(x, y, c)
		}
	}
	purple := color.RGBA{128, 0, 128, 255}
	rgba := docinfer.BoxResize(colors, 2, 1).(*image.RGBA)
	for x := range 2 {
		if c := rgba.RGBAAt(x, 0); c != purple {
			t.Errorf("a red and blue checkerboard's pixel %d: %v, want %v", x, c, purple)
		}
	}
}

func TestFitSize(t *testing.T) {
	cases := []struct {
		w, h, bound, wantW, wantH int
	}{
		{8358, 5339, 2580480, 2010, 1283},
		{5339, 8358, 2580480, 1283, 2010},
		{300, 200, 6000, 95, 63},
		{4, 2, 2, 2, 1},
		{1000, 1, 10, 10, 1},
		{1, 1000, 10, 1, 10},
		{100, 100, 1, 1, 1},
	}
	for _, tc := range cases {
		w, h := docinfer.FitSize(tc.w, tc.h, tc.bound)
		if w != tc.wantW || h != tc.wantH {
			t.Errorf("%d×%d within %d: %d×%d, want %d×%d", tc.w, tc.h, tc.bound, w, h, tc.wantW, tc.wantH)
		}
		if w*h > tc.bound {
			t.Errorf("%d×%d within %d: %d×%d is %d pixels", tc.w, tc.h, tc.bound, w, h, w*h)
		}
	}
}

func picture(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	return img
}

func encoded(t *testing.T, format string, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&b, img)
	case "jpeg":
		err = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	case "gif":
		err = gif.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type sentImages struct {
	mu   sync.Mutex
	sent [][]byte
}

func (s *sentImages) serve(t *testing.T) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{
		ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, m := range req.Messages {
				for _, img := range m.Images {
					s.sent = append(s.sent, img.Data)
				}
			}
			return aitest.ChatReply{Text: description}
		},
		Embeddings: embedAll,
	})
}

func (s *sentImages) one(t *testing.T) []byte {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) != 1 {
		t.Fatalf("%d images sent, want 1", len(s.sent))
	}
	return s.sent[0]
}

func describeImage(t *testing.T, data []byte, mediaType string, maxPixels int) (docinfer.Config, []byte) {
	t.Helper()
	var s sentImages
	cfg := visionConfig(t, s.serve(t), nil, func(c *docinfer.Config) { c.Vision.MaxPixels = maxPixels })
	content := dispatch.NewMemoryContent()
	o := collect(t, newPipeline(t, cfg).Run(t.Context(), content, seq(imageInput(t, content, data, mediaType)), docinfer.Options{}))
	if o.err != nil {
		t.Fatal(o.err)
	}
	values, err := cfg.Cache.Get(t.Context(), docinfer.Key(cfg.Vision.Identity(), sha(string(data))))
	if err != nil {
		t.Fatal(err)
	}
	if values[0] == nil {
		t.Error("the description isn't held under the source image's SHA-256")
	}
	return cfg, s.one(t)
}

func TestRunDownscalesImage(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			src := encoded(t, format, picture(300, 200))
			_, sent := describeImage(t, src, "image/"+format, 6000)
			cfg, got, err := image.DecodeConfig(bytes.NewReader(sent))
			if err != nil {
				t.Fatalf("the image sent: %v", err)
			}
			if got != format || cfg.Width != 95 || cfg.Height != 63 {
				t.Errorf("sent a %s of %d×%d, want a %s of 95×63", got, cfg.Width, cfg.Height, format)
			}
			if _, _, err := image.Decode(bytes.NewReader(sent)); err != nil {
				t.Errorf("the image sent doesn't decode: %v", err)
			}
		})
	}
}

func TestRunSendsImageAsIs(t *testing.T) {
	oversized := encoded(t, "png", picture(300, 200))
	cases := []struct {
		name      string
		data      []byte
		mediaType string
		maxPixels int
	}{
		{"within the bound", oversized, "image/png", 300 * 200},
		{"with no bound", oversized, "image/png", 0},
		{"undecodable", imageBytes, imageType, 1},
		{"a GIF", encoded(t, "gif", picture(300, 200)), "image/gif", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, sent := describeImage(t, tc.data, tc.mediaType, tc.maxPixels); !bytes.Equal(sent, tc.data) {
				t.Errorf("sent %d bytes, want the image's %d as they are", len(sent), len(tc.data))
			}
		})
	}
}

func TestMaxPixelsKeys(t *testing.T) {
	seen := map[string]int{}
	for _, maxPixels := range []int{0, 1, 2580480} {
		k := docinfer.Key(docinfer.Vision{Model: "m", Served: "s", MaxTokens: 1, MaxPixels: maxPixels}.Identity(), sha("content"))
		if prior, ok := seen[k]; ok {
			t.Errorf("MaxPixels %d keys the same as %d", maxPixels, prior)
		}
		seen[k] = maxPixels
	}
}
