package chunk_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer/chunk"
)

// update rewrites the golden corpus from the splitting as it stands:
//
//	go test ./chunk -run TestGoldenCorpus -update
//
// Do it only with a new chunk.Version, which names the corpus's file, never to make a changed cut
// pass under the old one: docinfer's Profile reports the version, so chunks cut differently under
// the same version would be taken for the old.
var update = flag.Bool("update", false, "rewrite the golden corpus of chunks for chunk.Version")

// goldenCase is one input of the golden corpus and the chunks it gives.
type goldenCase struct {
	Name   string        `json:"name"`
	Bound  int           `json:"bound"`
	Text   string        `json:"text"`
	Chunks []chunk.Chunk `json:"chunks"`
}

// goldenInputs are the corpus's inputs, each part of the splitting at least once: headings, a
// heading in a fenced code block, paragraphs, sentences and their closers, lines, multibyte text,
// a hard cut, and texts with nothing to cut.
var goldenInputs = []goldenCase{
	{Name: "empty", Bound: 10, Text: ""},
	{Name: "white space only", Bound: 10, Text: " \n\n\t  \r\n "},
	{Name: "fits", Bound: 100, Text: "\n  Hello, world.  \n"},
	{
		Name:  "headings",
		Bound: 60,
		Text: "# Intro\nA short opening line.\n\n## Background\nSome context that matters here.\n" +
			"### Detail\nMore.\n# Code\n```\n# not a heading\nprint(1)\n```\n#tag is no heading\n" +
			"####### nor is seven\n## Last\nThe end.",
	},
	{
		Name:  "paragraphs",
		Bound: 50,
		Text: "The first paragraph has a sentence or two. It goes on.\n\nA second, shorter one.\n\n\n" +
			"A third.\n\nAnd a fourth paragraph that runs a little longer than the bound allows it.",
	},
	{
		Name:  "long sentences",
		Bound: 40,
		Text: `She said "it shipped." Then v1.2 broke! Did anyone notice? (Nobody did.) ` +
			"This one sentence is far too long to fit within the bound, so it is cut hard.\n" +
			"- an item\n- another item\n- a third item",
	},
	{
		Name:  "multibyte",
		Bound: 8,
		Text: "日本語のテキストです。とても長い文章になります！短い？\n\n" +
			"Ünïcödé façade naïve café — résumé. 😀😀😀🎉🎉🎉😀😀😀🎉",
	},
	{Name: "hard cut", Bound: 7, Text: strings.Repeat("abcdefghij", 5)},
	{Name: "bound one", Bound: 1, Text: "a b\ncd é😀"},
}

// The committed corpus pins how texts are cut under chunk.Version: a change to any chunk must come
// with a new version, since docinfer's Profile reports it.
func TestGoldenCorpus(t *testing.T) {
	var got []goldenCase
	for _, in := range goldenInputs {
		cs := split(t, in.Text, in.Bound)
		valid(t, in.Text, in.Bound, cs)
		got = append(got, goldenCase{Name: in.Name, Bound: in.Bound, Text: in.Text, Chunks: cs})
	}
	path := filepath.Join("testdata", "chunks-v"+chunk.Version+".json")
	if *update {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden corpus for chunk.Version %q: %v; write it with -update", chunk.Version, err)
	}
	var want []goldenCase
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d cases, the corpus %s holds %d: change the version to change the corpus", len(got), path, len(want))
	}
	for i := range got {
		g, w := got[i], want[i]
		if g.Name != w.Name || g.Bound != w.Bound || g.Text != w.Text || !slices.Equal(g.Chunks, w.Chunks) {
			t.Errorf("case %q: chunks %+v, the corpus %s has %+v: the splitting changed, so change the version", g.Name, g.Chunks, path, w.Chunks)
		}
	}
}
