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

// See docs/development.md
var update = flag.Bool("update", false, "rewrite the golden corpus of chunks for chunk.Version")

type goldenCase struct {
	Name   string        `json:"name"`
	Bound  int           `json:"bound"`
	Runes  int           `json:"runes"`
	Count  string        `json:"count,omitzero"`
	Text   string        `json:"text"`
	Chunks []chunk.Chunk `json:"chunks"`
}

var goldenInputs = []goldenCase{
	{Name: "empty", Bound: 10, Runes: 1000, Text: ""},
	{Name: "white space only", Bound: 10, Runes: 1000, Text: " \n\n\t  \r\n "},
	{Name: "fits", Bound: 100, Runes: 1000, Text: "\n  Hello, world.  \n"},
	{
		Name:  "headings",
		Bound: 60, Runes: 1000,
		Text: "# Intro\nA short opening line.\n\n## Background\nSome context that matters here.\n" +
			"### Detail\nMore.\n# Code\n```\n# not a heading\nprint(1)\n```\n#tag is no heading\n" +
			"####### nor is seven\n## Last\nThe end.",
	},
	{
		Name:  "paragraphs",
		Bound: 50, Runes: 1000,
		Text: "The first paragraph has a sentence or two. It goes on.\n\nA second, shorter one.\n\n\n" +
			"A third.\n\nAnd a fourth paragraph that runs a little longer than the bound allows it.",
	},
	{
		Name:  "long sentences",
		Bound: 40, Runes: 1000,
		Text: `She said "it shipped." Then v1.2 broke! Did anyone notice? (Nobody did.) ` +
			"This one sentence is far too long to fit within the bound, so it is cut hard.\n" +
			"- an item\n- another item\n- a third item",
	},
	{
		Name:  "multibyte",
		Bound: 6, Runes: 1000,
		Text: "日本語のテキストです。とても長い文章になります！短い？\n\n" +
			"Ünïcödé façade naïve café — résumé. 😀😀😀🎉🎉🎉😀😀😀🎉",
	},
	{
		Name:  "scripts",
		Bound: 12, Runes: 1000,
		Text: "Το κείμενο είναι ελληνικό και αρκετά μακρύ. Это русский текст, и он довольно длинный.\n\n" +
			"هذا نص عربي طويل بما فيه الكفاية. זה טקסט בעברית שהוא ארוך מספיק.\n\n" +
			"यह हिंदी पाठ काफी लंबा है। 한국어 텍스트는 충분히 깁니다. 中文文本足够长了。",
	},
	{Name: "recut", Bound: 14, Runes: 1000, Text: "one two three four\n\nfive six seven\n\neight nine ten eleven\n\ntwelve"},
	{Name: "hard cut", Bound: 7, Runes: 1000, Text: strings.Repeat("abcdefghij", 5)},
	{Name: "bound three", Bound: 3, Runes: 1000, Text: "a b\ncd é😀"},
	{Name: "runes", Bound: 60, Runes: 30, Text: "One short sentence. Another one here. A third.\n\nA paragraph of its own."},
	{
		Name: "white-space runs", Bound: 60, Runes: 400, Count: "space runs",
		Text: "Left column" + strings.Repeat(" ", 610) + "middle" + strings.Repeat(" ", 540) + "right\n" +
			"one" + strings.Repeat(" ", 618) + "two" + strings.Repeat(" ", 543) + "three\n" +
			strings.Repeat(" ", 1290) + "a page number 7\n" +
			"end" + strings.Repeat(" ", 900) + "of" + strings.Repeat(" ", 700) + "columns",
	},
}

var goldenCounts = map[string]chunk.Counter{"": words, "space runs": spaceRuns}

func TestGoldenCorpus(t *testing.T) {
	var got []goldenCase
	for _, in := range goldenInputs {
		cs := splitWithin(t, goldenCounts[in.Count], in.Text, in.Bound, in.Runes)
		got = append(got, goldenCase{Name: in.Name, Bound: in.Bound, Runes: in.Runes, Count: in.Count, Text: in.Text, Chunks: cs})
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
		if g.Name != w.Name || g.Bound != w.Bound || g.Runes != w.Runes || g.Count != w.Count || g.Text != w.Text || !slices.Equal(g.Chunks, w.Chunks) {
			t.Errorf("case %q: chunks %+v, the corpus %s has %+v: the splitting changed, so change the version", g.Name, g.Chunks, path, w.Chunks)
		}
	}
}
