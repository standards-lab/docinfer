package chunk_test

import (
	"context"
	"errors"
	"fmt"
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

func runes(_ context.Context, text string) (int, error) { return utf8.RuneCountInString(text), nil }

func words(_ context.Context, text string) (int, error) {
	n := 2 + strings.Count(text, "\n")
	for f := range strings.FieldsSeq(text) {
		n += (utf8.RuneCountInString(f) + 2) / 3
	}
	return n, nil
}

func split(t *testing.T, text string, bound int) []chunk.Chunk {
	t.Helper()
	return splitBy(t, runes, text, bound)
}

func splitBy(t *testing.T, count chunk.Counter, text string, bound int) []chunk.Chunk {
	t.Helper()
	cs, err := chunk.Splitter{Bound: bound, Count: count}.Split(t.Context(), text)
	if err != nil {
		t.Fatalf("Split(%q) with bound %d: %v", text, bound, err)
	}
	valid(t, count, text, bound, cs)
	return cs
}

func valid(t *testing.T, count chunk.Counter, text string, bound int, cs []chunk.Chunk) {
	t.Helper()
	last := 0
	for i, c := range cs {
		if n, _ := count(t.Context(), c.Text); n > bound || c.Text == "" {
			t.Errorf("chunk %d %q measures %d tokens, want 1 to %d", i, c.Text, n, bound)
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
	want := []string{"# One\nalpha beta\n## Two\ngamma", "# Three\n```\n# not a heading\n```\nend"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_NotHeadings(t *testing.T) {
	text := "# A\nxxxx\n#tag yyyy\n####### seven\nzzzz"
	got := split(t, text, 30)
	want := []string{"# A\nxxxx\n#tag yyyy", "####### seven\nzzzz"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Paragraphs(t *testing.T) {
	text := "First paragraph here.\n\nSecond one.\n\n\nThird paragraph, longer."
	got := split(t, text, 40)
	want := []string{"First paragraph here.\n\nSecond one.", "Third paragraph, longer."}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Sentences(t *testing.T) {
	text := `He said "go." Then v1.2 shipped! Did it work? Yes。Done`
	got := split(t, text, 20)
	want := []string{`He said "go."`, "Then v1.2 shipped!", "Did it work? Yes。", "Done"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_HardCut(t *testing.T) {
	text := "abcdefghijklmnopqrstuvwxyz"
	got := split(t, text, 10)
	want := []string{"abcdefghij", "klmnopqrst", "uvwxyz"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

func TestSplit_Multibyte(t *testing.T) {
	text := "日本語のテキストです😀😀😀🎉🎉"
	got := split(t, text, 4)
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
	want := []string{"ab", "\xffc", "d\xfe"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
}

var multiScript = strings.Repeat("# Heading\n\nSome text. More text, with ünïcödé! Another line\n- item one\n- item two\n\n", 20) +
	"日本語の文です。もう一つ！\n\nΤο κείμενο είναι ελληνικό. Это русский текст. هذا نص عربي. זה טקסט בעברית.\n\n" +
	"यह हिंदी पाठ है। 한국어 텍스트입니다.\n\n" + strings.Repeat("x", 333) + "\n\t "

func TestSplit_Properties(t *testing.T) {
	for _, bound := range []int{1, 2, 7, 20, 64, 500, 5000} {
		splitBy(t, runes, multiScript, bound)
	}
	for _, bound := range []int{3, 4, 9, 30, 120, 480, 5000} {
		splitBy(t, words, multiScript, bound)
	}
}

func joins(_ context.Context, text string) (int, error) {
	n, _ := words(context.Background(), text)
	return n + 5*strings.Count(text, "\n\n"), nil
}

func TestSplit_RecutsAPackOverTheBound(t *testing.T) {
	text := "one two three\n\nfour five six\n\nseven eight"
	got := splitBy(t, joins, text, 13)
	want := []string{"one two three", "four five six", "seven eight"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split = %q, want %q", texts(got), want)
	}
	got = splitBy(t, words, text, 13)
	want = []string{"one two three\n\nfour five six", "seven eight"}
	if !slices.Equal(texts(got), want) {
		t.Errorf("Split counting a join's line breaks = %q, want %q", texts(got), want)
	}
}

func TestSplit_HardCutMeasures(t *testing.T) {
	text := strings.Repeat("日本語のテキスト", 40) + " " + strings.Repeat("abcdefghij", 30)
	for _, bound := range []int{3, 5, 17, 60} {
		got := splitBy(t, words, text, bound)
		if len(got) < 2 {
			t.Errorf("bound %d: %d chunks, want the text cut", bound, len(got))
		}
	}
}

func TestSplit_CharacterOverTheBound(t *testing.T) {
	cs, err := chunk.Splitter{Bound: 2, Count: words}.Split(t.Context(), "ab cd")
	if !errors.Is(err, chunk.ErrRequest) || cs != nil {
		t.Errorf("Split with every character over the bound = %v, %v, want no chunk and ErrRequest", cs, err)
	}
}

func TestSplit_CountError(t *testing.T) {
	failure := errors.New("count failed")
	for _, after := range []int{0, 1, 5} {
		calls := 0
		count := func(ctx context.Context, text string) (int, error) {
			if calls++; calls > after {
				return 0, failure
			}
			return words(ctx, text)
		}
		cs, err := chunk.Splitter{Bound: 9, Count: count}.Split(t.Context(), multiScript)
		if !errors.Is(err, failure) || cs != nil {
			t.Errorf("Split with the count failing after %d calls = %d chunks, %v, want none and the count's error", after, len(cs), err)
		}
	}
}

func TestSplit_PassesTheContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "job")
	count := func(ctx context.Context, text string) (int, error) {
		if ctx.Value(key{}) != "job" {
			return 0, fmt.Errorf("count of %q without Split's context", text)
		}
		return words(ctx, text)
	}
	if _, err := (chunk.Splitter{Bound: 9, Count: count}).Split(ctx, multiScript); err != nil {
		t.Error(err)
	}
}

func TestSplit_Calls(t *testing.T) {
	var paragraphs []string
	for i := range 200 {
		paragraphs = append(paragraphs, fmt.Sprintf("Paragraph %d says a few words about item %d here.", i, i))
	}
	text := strings.Join(paragraphs, "\n\n")
	calls := 0
	count := func(ctx context.Context, text string) (int, error) {
		calls++
		return words(ctx, text)
	}
	cs, err := chunk.Splitter{Bound: 120, Count: count}.Split(t.Context(), text)
	if err != nil {
		t.Fatal(err)
	}
	valid(t, words, text, 120, cs)
	if limit := 2 + len(paragraphs) + 2*len(cs); calls > limit {
		t.Errorf("%d calls for %d paragraphs in %d chunks, want at most %d", calls, len(paragraphs), len(cs), limit)
	}
	calls = 0
	if _, err := (chunk.Splitter{Bound: 5000, Count: count}).Split(t.Context(), text); err != nil || calls != 1 {
		t.Errorf("a text within the bound took %d calls, %v; want 1", calls, err)
	}
}

func TestSplit_Refused(t *testing.T) {
	for _, s := range []chunk.Splitter{{Bound: 0, Count: runes}, {Bound: -1, Count: runes}, {Bound: 10}} {
		cs, err := s.Split(t.Context(), "ab")
		if !errors.Is(err, chunk.ErrRequest) || cs != nil {
			t.Errorf("Split with bound %d, count set %t = %v, %v, want no chunk and ErrRequest", s.Bound, s.Count != nil, cs, err)
		}
	}
}

func TestSplitter_Settings(t *testing.T) {
	got := chunk.Splitter{Bound: 480, Count: runes, Model: "org/embedder"}.Settings()
	if want := `{"version":"` + chunk.Version + `","bound":480,"model":"org/embedder"}`; got != want {
		t.Errorf("Settings = %s, want %s", got, want)
	}
}
