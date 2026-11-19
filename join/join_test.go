package join_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docinfer/join"
)

// matched is a matched seam's report.
func matched(matches, overlap, position int) join.Seam {
	return join.Seam{Matched: true, Matches: matches, Overlap: overlap, Position: position}
}

// unmatched is an unmatched seam's report, the later window's words from position on.
func unmatched(position int) join.Seam { return join.Seam{Position: position} }

// repeated is a nine-word phrase of the fixture's, its words distinct, which the repeated-phrase
// cases say twice and more.
const repeated = "he would have to pay her the money which"

func TestJoin(t *testing.T) {
	tests := []struct {
		name        string
		transcripts []string
		// seconds is each seam's span, at 8 words a second; zero means 2 s, a bound of 16 words.
		seconds float64
		text    string
		seams   []join.Seam
	}{
		{
			name:        "exact overlap",
			transcripts: []string{"he was in a fevered state of mind", "state of mind owing to the blight"},
			// The three words overlap at the third offset; the splice keeps "state" of the left and
			// "of mind" of the right.
			text:  "he was in a fevered state of mind owing to the blight",
			seams: []join.Seam{matched(3, 3, 6)},
		},
		{
			name: "partial overlap with differences at the edges",
			transcripts: []string{
				"hurstwood walked the floor mentally arranging the chief point",
				"ranging the chief points of his situation",
			},
			// Each window heard a word cut at its edge differently: 2 of the 4 overlapped words
			// match, and the splice at their middle drops both cut forms.
			text:  "hurstwood walked the floor mentally arranging the chief points of his situation",
			seams: []join.Seam{matched(2, 4, 7)},
		},
		{
			name:        "punctuation and case differ",
			transcripts: []string{"He also thought of his managerial position.", "His Managerial Position, Hurstwood walked"},
			text:        "He also thought of his Managerial Position, Hurstwood walked",
			seams:       []join.Seam{matched(3, 3, 5)},
		},
		{
			name:        "no overlap is concatenated",
			transcripts: []string{"it did not matter", "what he did"},
			text:        "it did not matter what he did",
			seams:       []join.Seam{unmatched(4)},
		},
		{
			name:        "one matching word is concatenated",
			transcripts: []string{"the money which", "which she would"},
			text:        "the money which which she would",
			seams:       []join.Seam{unmatched(3)},
		},
		{
			name:        "dashes match nothing",
			transcripts: []string{"a b — --", "— -- c d"},
			text:        "a b — -- — -- c d",
			seams:       []join.Seam{unmatched(4)},
		},
		{
			name:        "single window has no seams",
			transcripts: []string{"  He was  in\na state "},
			text:        "He was in a state",
		},
		{
			name: "no windows",
			text: "",
		},
		{
			name:        "empty windows",
			transcripts: []string{"", " "},
			text:        "",
			seams:       []join.Seam{unmatched(0)},
		},
		{
			name:        "an empty window between two is unmatched on both sides",
			transcripts: []string{"a b c", "", "c d e"},
			// The windows either side don't share a seam, so their words are concatenated, the
			// "c" each heard kept twice.
			text:  "a b c c d e",
			seams: []join.Seam{unmatched(3), unmatched(3)},
		},
		{
			name: "three windows chain on what each kept",
			transcripts: []string{
				"hurstwood walked the floor",
				"the floor mentally arranging the chief",
				"the chief points of his situation",
			},
			text:  "hurstwood walked the floor mentally arranging the chief points of his situation",
			seams: []join.Seam{matched(2, 2, 3), matched(2, 2, 7)},
		},
		{
			name: "a phrase repeated beyond the bound isn't the splice",
			transcripts: []string{
				repeated + " " + repeated,
				repeated + " " + repeated + " or there",
			},
			// A bound of 16 can't align the two windows' whole 18-word repeats, so the
			// splice is the nine-word overlap and the left's second repeat and the right's two are
			// all kept.
			text:  strings.Repeat(repeated+" ", 3) + "or there",
			seams: []join.Seam{matched(9, 9, 13)},
		},
		{
			name: "a bound wide enough latches onto the repeat",
			transcripts: []string{
				repeated + " " + repeated,
				repeated + " " + repeated + " or there",
			},
			seconds: 5,
			// A bound of 40 is as good as unbounded: transformers' score prefers the 18-word
			// perfect match, a repeat dropped.
			text:  strings.Repeat(repeated+" ", 2) + "or there",
			seams: []join.Seam{matched(18, 18, 9)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seconds := tc.seconds
			if seconds == 0 {
				seconds = 2
			}
			got, err := join.Join(tc.transcripts, evenSeams(len(tc.transcripts), seconds), join.Request{WordsPerSecond: 8})
			if err != nil {
				t.Fatalf("Join: %v", err)
			}
			if got.Text() != tc.text {
				t.Errorf("text\n%q, want\n%q", got.Text(), tc.text)
			}
			if !reflect.DeepEqual(got.Seams, tc.seams) {
				t.Errorf("seams %+v, want %+v", got.Seams, tc.seams)
			}
		})
	}
}

// evenSeams is n-1 seams of seconds each, for n transcripts.
func evenSeams(n int, seconds float64) []float64 {
	seams := make([]float64, max(0, n-1))
	for i := range seams {
		seams[i] = seconds
	}
	return seams
}

// TestJoinBound measures each seam's bound with windows of one repeated word, which match
// wherever they overlap: the best offset is the widest whole overlap, so a seam's Matches is its
// bound. The bound is the seam's seconds times WordsPerSecond, rounded up, and at least 2.
func TestJoinBound(t *testing.T) {
	window := strings.TrimSpace(strings.Repeat("so ", 50))
	tests := []struct {
		seconds        float64
		wordsPerSecond int
		bound          int
	}{
		{seconds: 2, wordsPerSecond: 8, bound: 16},
		{seconds: 2.5, wordsPerSecond: 2, bound: 5},
		{seconds: 1.01, wordsPerSecond: 3, bound: 4},
		{seconds: 0.125, wordsPerSecond: 20, bound: 3},
		{seconds: 0.25, wordsPerSecond: 8, bound: 2},
		{seconds: 0.1, wordsPerSecond: 8, bound: 2},
		{seconds: 0, wordsPerSecond: 1, bound: 2},
		{seconds: 1000, wordsPerSecond: 1000, bound: 50},
	}
	for _, tc := range tests {
		got, err := join.Join([]string{window, window}, []float64{tc.seconds}, join.Request{WordsPerSecond: tc.wordsPerSecond})
		if err != nil {
			t.Fatalf("%v s at %d words a second: Join: %v", tc.seconds, tc.wordsPerSecond, err)
		}
		if m := got.Seams[0].Matches; m != tc.bound {
			t.Errorf("%v s at %d words a second: bound %d, want %d", tc.seconds, tc.wordsPerSecond, m, tc.bound)
		}
	}
}

// TestJoinBoundPerSeam gives each seam its own seconds: the same windows match on the first
// seam's bound and the second's.
func TestJoinBoundPerSeam(t *testing.T) {
	window := strings.TrimSpace(strings.Repeat("so ", 50))
	got, err := join.Join([]string{window, window, window}, []float64{0.5, 1.5}, join.Request{WordsPerSecond: 6})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if m0, m1 := got.Seams[0].Matches, got.Seams[1].Matches; m0 != 3 || m1 != 9 {
		t.Errorf("bounds %d and %d, want 3 and 9", m0, m1)
	}
}

// A WordsPerSecond below 1 is refused with ErrRequest: it has no default. So are seams that
// don't fit the transcripts.
func TestJoinRefuses(t *testing.T) {
	tests := []struct {
		name        string
		transcripts []string
		seams       []float64
		req         join.Request
	}{
		{name: "zero words per second", transcripts: []string{"a", "b"}, seams: []float64{2}},
		{name: "negative words per second", transcripts: []string{"a", "b"}, seams: []float64{2}, req: join.Request{WordsPerSecond: -1}},
		{name: "a seam too few", transcripts: []string{"a", "b"}, req: join.Request{WordsPerSecond: 8}},
		{name: "a seam too many", transcripts: []string{"a"}, seams: []float64{2}, req: join.Request{WordsPerSecond: 8}},
		{name: "seams without transcripts", seams: []float64{2}, req: join.Request{WordsPerSecond: 8}},
		{name: "negative seconds", transcripts: []string{"a", "b"}, seams: []float64{-1}, req: join.Request{WordsPerSecond: 8}},
		{name: "NaN seconds", transcripts: []string{"a", "b"}, seams: []float64{math.NaN()}, req: join.Request{WordsPerSecond: 8}},
		{name: "infinite seconds", transcripts: []string{"a", "b"}, seams: []float64{math.Inf(1)}, req: join.Request{WordsPerSecond: 8}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := join.Join(tc.transcripts, tc.seams, tc.req)
			if !errors.Is(err, join.ErrRequest) || !reflect.DeepEqual(got, join.Transcript{}) {
				t.Errorf("Join = %+v, %v, want an empty transcript and ErrRequest", got, err)
			}
		})
	}
}

// Seam's JSON names its members.
func TestSeamJSON(t *testing.T) {
	got, err := json.Marshal(matched(3, 4, 7))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"matched":true,"matches":3,"overlap":4,"position":7}`; string(got) != want {
		t.Errorf("Seam's JSON %s, want %s", got, want)
	}
}

func TestJoinDeterministic(t *testing.T) {
	transcripts := []string{
		"He was in a fevered state of mind, owing to the blight",
		"to the blight his wife's action threatened to cast",
		"to cast upon his entire future.",
	}
	seams := []float64{2, 2}
	req := join.Request{WordsPerSecond: 8}
	first, err := join.Join(transcripts, seams, req)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	for range 5 {
		again, err := join.Join(transcripts, seams, req)
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
		if !reflect.DeepEqual(again, first) {
			t.Fatalf("Join gave %+v, then %+v", first, again)
		}
	}
}

// TestJoinReconstructs cuts random runs of distinct words into windows that overlap by 2 to 8
// words, each keeping words of its own, and joins them back: the run comes back whole, every seam
// matched on its overlap, the splice where the later window's kept words begin.
func TestJoinReconstructs(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 2))
	for trial := range 200 {
		var words, transcripts []string
		var overlaps, positions []int
		end := 0
		windows := 1 + rng.IntN(5)
		for w := range windows {
			start := 0
			if w > 0 {
				k := 2 + rng.IntN(7)
				start = end - k
				overlaps = append(overlaps, k)
				positions = append(positions, start+k/2)
			}
			end += 9 + rng.IntN(10)
			for len(words) < end {
				words = append(words, fmt.Sprintf("w%d", len(words)))
			}
			transcripts = append(transcripts, strings.Join(words[start:end], " "))
		}
		got, err := join.Join(transcripts, evenSeams(len(transcripts), 2), join.Request{WordsPerSecond: 8})
		if err != nil {
			t.Fatalf("trial %d: Join: %v", trial, err)
		}
		if got.Text() != strings.Join(words, " ") {
			t.Fatalf("trial %d: joined %q, want %q, from %q", trial, got.Text(), strings.Join(words, " "), transcripts)
		}
		if len(got.Seams) != windows-1 {
			t.Fatalf("trial %d: %d seams, want %d", trial, len(got.Seams), windows-1)
		}
		for i, s := range got.Seams {
			if want := matched(overlaps[i], overlaps[i], positions[i]); s != want {
				t.Errorf("trial %d: seam %d is %+v, want %+v", trial, i, s, want)
			}
		}
	}
}

// checkSettled joins every prefix of transcripts, the first k, and checks Join's settled prefix
// against the join of them all: the words before the prefix's latest seam, and its seams, are the
// whole join's.
func checkSettled(t *testing.T, transcripts []string, seams []float64, req join.Request) {
	t.Helper()
	full, err := join.Join(transcripts, seams, req)
	if err != nil {
		t.Fatal(err)
	}
	for k := 1; k <= len(transcripts); k++ {
		part, err := join.Join(transcripts[:k], seams[:k-1], req)
		if err != nil {
			t.Fatal(err)
		}
		settled := 0
		if k > 1 {
			settled = part.Seams[k-2].Position
		}
		if !slices.Equal(part.Words[:settled], full.Words[:settled]) {
			t.Fatalf("the first %d of %q: settled words %q, the whole join's %q",
				k, transcripts, part.Words[:settled], full.Words[:settled])
		}
		if !slices.Equal(part.Seams, full.Seams[:k-1]) {
			t.Fatalf("the first %d of %q: seams %+v, the whole join's %+v", k, transcripts, part.Seams, full.Seams[:k-1])
		}
	}
}

// TestJoinSettledPrefix joins random window transcripts, from a fixed seed, as they would arrive,
// each prefix of them in turn: the words before the latest seam never change as more arrive. The
// transcripts are drawn from a small vocabulary, so words repeat across seams, and each window's
// edges are perturbed as a cut word is heard differently, some windows empty as a failed one is,
// so seams match, splice short, and fail to match.
func TestJoinSettledPrefix(t *testing.T) {
	vocabulary := strings.Fields("the a of to he she was in it his her and money state mind future, Future. — would")
	rng := rand.New(rand.NewPCG(8, 9))
	for range 500 {
		var words []string
		for range 20 + rng.IntN(200) {
			words = append(words, vocabulary[rng.IntN(len(vocabulary))])
		}
		var transcripts []string
		for start := 0; start < len(words); {
			end := min(len(words), start+3+rng.IntN(30))
			w := slices.Clone(words[start:end])
			if rng.IntN(4) == 0 && len(w) > 0 {
				w[0] = vocabulary[rng.IntN(len(vocabulary))]
			}
			if rng.IntN(4) == 0 && len(w) > 0 {
				w = w[:len(w)-1]
			}
			if rng.IntN(10) == 0 {
				w = nil
			}
			transcripts = append(transcripts, strings.Join(w, " "))
			start = max(start+1, end-rng.IntN(9))
		}
		var seams []float64
		for range len(transcripts) - 1 {
			seams = append(seams, []float64{0, 0.5, 2, 5}[rng.IntN(4)])
		}
		checkSettled(t, transcripts, seams, join.Request{WordsPerSecond: 8})
	}
}

// TestJoinSettledPrefixReal joins the levels fixture's transcript cut into windows as a plan of it
// would be, each window's last few words heard again at the next one's start, one cut word heard
// differently and one window failed: the words before the latest seam never change as more
// arrive.
func TestJoinSettledPrefixReal(t *testing.T) {
	var f levelsFixture
	readTruth(t, levelsTruth, &f)
	words := strings.Fields(strings.ToLower(strings.Join(f.Transcript, " ")))
	var transcripts []string
	for start := 0; start < len(words); start += 40 {
		w := slices.Clone(words[start:min(len(words), start+45)])
		transcripts = append(transcripts, strings.Join(w, " "))
	}
	transcripts[1] = "ing " + transcripts[1] + "."
	transcripts[3] = ""
	checkSettled(t, transcripts, evenSeams(len(transcripts), 2), join.Request{WordsPerSecond: 8})
}
