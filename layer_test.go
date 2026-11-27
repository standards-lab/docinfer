package docinfer_test

import (
	"testing"

	"github.com/standards-lab/docinfer"
)

func TestLayerWords(t *testing.T) {
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
			if got := docinfer.LayerWords(tc.text); got != tc.want {
				t.Errorf("LayerWords(%q) = %d, want %d", tc.text, got, tc.want)
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
