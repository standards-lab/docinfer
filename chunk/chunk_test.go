package chunk_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/standards-lab/docinfer/chunk"
)

func texts(cs []chunk.Chunk) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

func split(t *testing.T, text string, bound int) []chunk.Chunk {
	t.Helper()
	cs, err := chunk.Splitter{Bound: bound}.Split(text)
	if err != nil {
		t.Fatalf("Split(%q) with bound %d: %v", text, bound, err)
	}
	return cs
}

func valid(t *testing.T, text string, bound int, cs []chunk.Chunk) {
	t.Helper()
	last := 0
	for i, c := range cs {
		if n := utf8.RuneCountInString(c.Text); n > bound || n == 0 {
			t.Errorf("chunk %d %q has %d runes, want 1 to %d", i, c.Text, n, bound)
		}
		if c.Span.Start < last || c.Span.End <= c.Span.Start || c.Span.End > len(text) {
			t.Fatalf("chunk %d span [%d, %d) after %d in %d bytes, want ordered and inside", i, c.Span.Start, c.Span.End, last, len(text))
		}
		if text[c.Span.Start:c.Span.End] != c.Text {
			t.Errorf("chunk %d text %q, want the span's %q", i, c.Text, text[c.Span.Start:c.Span.End])
		}
		if utf8.ValidString(text) && !utf8.ValidString(c.Text) {
			t.Errorf("chunk %d %q splits a UTF-8 character", i, c.Text)
		}
		if strings.TrimSpace(c.Text) != c.Text {
			t.Errorf("chunk %d %q has white space at an end", i, c.Text)
		}
		if gap := text[last:c.Span.Start]; strings.TrimSpace(gap) != "" {
			t.Errorf("chunk %d: the bytes %q before it are in no chunk", i, gap)
		}
		last = c.Span.End
	}
	if rest := text[last:]; strings.TrimSpace(rest) != "" {
		t.Errorf("the bytes %q after the last chunk are in no chunk", rest)
	}
}

func TestSplit_Empty(t *testing.T) {
	for _, text := range []string{"", " ", "\n\n\t  \n", "  "} {
		if cs := split(t, text, 10); len(cs) != 0 {
			t.Errorf("Split(%q) = %v, want no chunk", text, cs)
		}
	}
}

func TestSplit_Fits(t *testing.T) {
	text := "\n  Hello, world.  \n"
	got := split(t, text, 100)
	want := []chunk.Chunk{{Text: "Hello, world.", Span: chunk.Span{Start: 3, End: 16}}}
	if !slices.Equal(got, want) {
		t.Errorf("Split = %v, want %v", got, want)
	}
}

func TestSplit_Headings(t *testing.T) {
	text := "# One\nalpha beta\n## Two\ngamma\n# Three\n```\n# not a heading\n```\nend"
	got := split(t, text, 40)
	valid(t, text, 40, got)
	want := []string{"# One\nalpha beta\n## Two\ngamma", "# Three\n```\n# not a heading\n```\nend"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_NotHeadings(t *testing.T) {
	text := "# A\nxxxx\n#tag yyyy\n####### seven\nzzzz"
	got := split(t, text, 30)
	valid(t, text, 30, got)
	want := []string{"# A\nxxxx\n#tag yyyy", "####### seven\nzzzz"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Paragraphs(t *testing.T) {
	text := "First paragraph here.\n\nSecond one.\n\n\nThird paragraph, longer."
	got := split(t, text, 40)
	valid(t, text, 40, got)
	want := []string{"First paragraph here.\n\nSecond one.", "Third paragraph, longer."}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Sentences(t *testing.T) {
	text := `He said "go." Then v1.2 shipped! Did it work? Yes。Done`
	got := split(t, text, 20)
	valid(t, text, 20, got)
	want := []string{`He said "go."`, "Then v1.2 shipped!", "Did it work? Yes。", "Done"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_HardCut(t *testing.T) {
	text := "abcdefghijklmnopqrstuvwxyz"
	got := split(t, text, 10)
	valid(t, text, 10, got)
	want := []string{"abcdefghij", "klmnopqrst", "uvwxyz"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Multibyte(t *testing.T) {
	text := "日本語のテキストです😀😀😀🎉🎉"
	got := split(t, text, 4)
	valid(t, text, 4, got)
	want := []string{"日本語の", "テキスト", "です😀😀", "😀🎉🎉"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
	if got := split(t, "ééééé", 5); len(got) != 1 {
		t.Errorf("Split(ééééé, 5) = %q, want one chunk", texts(got))
	}
}

func TestSplit_InvalidUTF8(t *testing.T) {
	text := "ab\xffcd\xfe"
	got := split(t, text, 2)
	valid(t, text, 2, got)
	want := []string{"ab", "\xffc", "d\xfe"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Properties(t *testing.T) {
	text := strings.Repeat("# Heading\n\nSome text. More text, with ünïcödé! Another line\n- item one\n- item two\n\n", 20) +
		"日本語の文です。もう一つ！\n\n" + strings.Repeat("x", 333) + "\n\t "
	for _, bound := range []int{1, 2, 7, 20, 64, 500, 5000} {
		valid(t, text, bound, split(t, text, bound))
	}
}

func TestSplit_BoundBelowOne(t *testing.T) {
	for _, bound := range []int{0, -1} {
		cs, err := chunk.Splitter{Bound: bound}.Split("ab")
		if !errors.Is(err, chunk.ErrRequest) || cs != nil {
			t.Errorf("Split with bound %d = %v, %v, want no chunk and ErrRequest", bound, cs, err)
		}
	}
}

func TestSplitter_Settings(t *testing.T) {
	got := chunk.Splitter{Bound: 1200}.Settings()
	if want := `{"version":"` + chunk.Version + `","bound":1200}`; got != want {
		t.Errorf("Settings = %s, want %s", got, want)
	}
}
