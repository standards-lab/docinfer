package docinfer_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"testing"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/chunk"
)

// A vector is standard base64 of little-endian float32s: these strings are pinned, since held
// vectors and every batch's chunks carry them.
func TestDecodeVector(t *testing.T) {
	for _, tc := range []struct {
		vector string
		want   []float32
	}{
		{"AACAPw==", []float32{1}},
		{"AACAPwAAAMAAAAA/", []float32{1, -2, 0.5}},
		{"AAAAAAAAAIA=", []float32{0, float32(math.Copysign(0, -1))}},
	} {
		got, err := docinfer.DecodeVector(tc.vector)
		if err != nil {
			t.Errorf("decode %q: %v", tc.vector, err)
			continue
		}
		if !slices.EqualFunc(got, tc.want, func(a, b float32) bool { return math.Float32bits(a) == math.Float32bits(b) }) {
			t.Errorf("decode %q: %v, want %v", tc.vector, got, tc.want)
		}
	}
	want := []float32{0.25, -1e-8, 3.4e38, float32(math.Inf(1))}
	b := make([]byte, 0, 4*len(want))
	for _, f := range want {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(f))
	}
	got, err := docinfer.DecodeVector(base64.StdEncoding.EncodeToString(b))
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("decode of %v encoded: %v, %v", want, got, err)
	}
	for _, vector := range []string{
		"",          // no vector
		"AACAPw",    // unpadded
		"AACAPw=",   // padded short
		"AACA",      // three bytes
		"AACAPwAA",  // five bytes, padded
		"AACAP w==", // not base64
		"AACAPx==",  // non-zero trailing bits, which a strict decoding refuses
		"AACAP-8_",  // URL base64
	} {
		if got, err := docinfer.DecodeVector(vector); err == nil {
			t.Errorf("decode %q: %v, want an error", vector, got)
		}
	}
}

// A batch's digest is the SHA-256 of its chunks' digests, in order, each prefixed by its length.
func TestBatchDigest(t *testing.T) {
	d1, d2 := sha("first"), sha("second")
	batch := docinfer.Batch{Chunks: []docinfer.Chunk{{Digest: d1}, {Digest: d2}}}
	var in []byte
	for _, d := range []string{d1, d2} {
		in = binary.BigEndian.AppendUint64(in, uint64(len(d)))
		in = append(in, d...)
	}
	sum := sha256.Sum256(in)
	if got, want := batch.Digest(), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("Digest: %s, want %s", got, want)
	}
	swapped := docinfer.Batch{Chunks: []docinfer.Chunk{{Digest: d2}, {Digest: d1}}}
	if swapped.Digest() == batch.Digest() {
		t.Error("Digest is the same with its chunks in another order")
	}
	empty := sha256.Sum256(nil)
	if got := (docinfer.Batch{}).Digest(); got != hex.EncodeToString(empty[:]) {
		t.Errorf("an empty batch's Digest: %s, want the SHA-256 of nothing", got)
	}
}

// A Chunk's JSON is {digest, text, span, index, source, vector, error}, chunk.Chunk's fields
// inline, vector and error only when set.
func TestChunkJSON(t *testing.T) {
	c := docinfer.Chunk{
		Digest: sha("Hello"),
		Text:   "Hello",
		Span:   chunk.Span{Start: 2, End: 7},
		Index:  1,
		Source: 3,
		Vector: "AACAPw==",
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"digest":"` + sha("Hello") + `","text":"Hello","span":{"start":2,"end":7},"index":1,"source":3,"vector":"AACAPw=="}`
	if string(b) != want {
		t.Errorf("Chunk encodes as %s, want %s", b, want)
	}
	c.Vector, c.Error = "", "refused"
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"digest":"` + sha("Hello") + `","text":"Hello","span":{"start":2,"end":7},"index":1,"source":3,"error":"refused"}`
	if string(b) != want {
		t.Errorf("a refused Chunk encodes as %s, want %s", b, want)
	}
}

// Counts' JSON names are snake_case, and a skipped capability's counts and tokens are absent.
func TestCountsJSON(t *testing.T) {
	c := docinfer.Counts{
		Texts: 1, Windows: 2,
		Transcription: &docinfer.TranscriptionCounts{Transcribed: 2, SeamsMatched: 1},
		Tokens:        docinfer.Tokens{Transcription: &docinfer.Usage{Input: 3}, Embeddings: docinfer.Usage{Input: 4}},
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"texts":1,"metadata":0,"embedded":0,"rejected":0,"held":0,"requests":0,"images":0,"windows":2,` +
		`"transcription":{"transcribed":2,"incomplete":0,"refused":0,"failed":0,"held":0,"requests":0,` +
		`"transcripts":0,"seams_matched":1,"seams_unmatched":0},` +
		`"tokens":{"transcription":{"input":3,"output":0},"embeddings":{"input":4,"output":0}}}`
	if string(b) != want {
		t.Errorf("Counts encodes as\n%s, want\n%s", b, want)
	}
}
