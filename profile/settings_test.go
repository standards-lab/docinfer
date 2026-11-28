package profile_test

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/protocol"

	"github.com/standards-lab/docinfer"
	"github.com/standards-lab/docinfer/profile"
)

const settingsJSON = `{
	"vision": {
		"endpoint": "gpu", "model": "vision", "served_model": "org/vision-1",
		"prompt": "Describe the image.", "max_tokens": 2048, "reasoning": "none",
		"native": {"temperature":1.0,"top_k":64}, "max_pixels": 2580480, "layer_words": 100
	},
	"transcription": {
		"endpoint": "audio", "model": "whisper", "language": "en", "max_tokens": 0, "native": {}
	},
	"extraction": {
		"endpoint": "gpu", "model": "agent", "max_tokens": 4096, "reasoning": "medium", "window_runes": 16000
	},
	"embeddings": {
		"endpoint": "embed", "model": "e5", "dimensions": 1024, "tokenizer": "embed-root",
		"batch": 16, "chunk_size": 480, "chunk_runes": 8192
	},
	"pending": 16,
	"batch_bytes": 524288,
	"words_per_second": 8
}`

func decode(t *testing.T, data string) profile.Settings {
	t.Helper()
	s, err := profile.Decode([]byte(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return s
}

func TestDecodeRoundTrips(t *testing.T) {
	s := decode(t, settingsJSON)
	if v := s.Vision; v == nil || v.Endpoint != "gpu" || v.Model != "vision" || v.ServedModel != "org/vision-1" ||
		*v.MaxTokens != 2048 || v.Reasoning != protocol.ReasoningEffortNone || *v.MaxPixels != 2580480 || *v.LayerWords != 100 {
		t.Errorf("vision %+v, want the JSON's", v)
	}
	if x := s.Extraction; x == nil || *x.WindowRunes != 16000 || x.Reasoning != protocol.ReasoningEffortMedium {
		t.Errorf("extraction %+v, want the JSON's", x)
	}
	if e := s.Embeddings; e.Tokenizer != "embed-root" || *e.ChunkSize != 480 || *e.ChunkRunes != 8192 || *e.Batch != 16 {
		t.Errorf("embeddings %+v, want the JSON's", e)
	}
	if *s.Pending != 16 || *s.BatchBytes != 524288 || *s.WordsPerSecond != 8 {
		t.Errorf("pending %d, batch_bytes %d, words_per_second %d, want the JSON's", *s.Pending, *s.BatchBytes, *s.WordsPerSecond)
	}
	data, err := jsonv2.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if again := decode(t, string(data)); !reflect.DeepEqual(again, s) {
		t.Errorf("settings after a round trip through %s:\n%+v, want\n%+v", data, again, s)
	}
}

func TestDecodeTakesOptionalValuesUnset(t *testing.T) {
	s := decode(t, `{
		"embeddings": {"endpoint": "embed", "model": "e5", "dimensions": 1, "tokenizer": "embed",
			"batch": 1, "chunk_size": 1, "chunk_runes": 1},
		"transcription": {"endpoint": "audio", "model": "whisper", "max_tokens": 0},
		"pending": 1, "batch_bytes": 1, "words_per_second": 1
	}`)
	if s.Vision != nil || s.Extraction != nil {
		t.Errorf("vision %+v and extraction %+v, want both unset", s.Vision, s.Extraction)
	}
	if tr := s.Transcription; tr.ServedModel != "" || tr.Prompt != "" || tr.Language != "" || tr.Native != nil {
		t.Errorf("transcription %+v, want its optional values unset", tr)
	}
}

func TestDecodeRefusesAnUnknownMember(t *testing.T) {
	data := strings.Replace(settingsJSON, `"chunk_size"`, `"chunk_tokens": 1, "chunk_size"`, 1)
	if _, err := profile.Decode([]byte(data)); !errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), "chunk_tokens") {
		t.Errorf("Decode with chunk_tokens: %v, want ErrRequest naming it", err)
	}
}

func without(t *testing.T, path ...string) string {
	t.Helper()
	return edited(t, path, nil)
}

func edited(t *testing.T, path []string, value any) string {
	t.Helper()
	var m map[string]any
	if err := jsonv2.Unmarshal([]byte(settingsJSON), &m); err != nil {
		t.Fatal(err)
	}
	parent := m
	for _, key := range path[:len(path)-1] {
		parent = parent[key].(map[string]any)
	}
	last := path[len(path)-1]
	if value == nil {
		delete(parent, last)
	} else {
		parent[last] = value
	}
	data, err := jsonv2.Marshal(m, jsontext.Multiline(false))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDecodeRefusesEachMissingValueByName(t *testing.T) {
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"vision", "endpoint"}, "vision: no endpoint"},
		{[]string{"vision", "model"}, "vision: no model"},
		{[]string{"vision", "prompt"}, "vision: no prompt"},
		{[]string{"vision", "max_tokens"}, "vision: no max_tokens"},
		{[]string{"vision", "max_pixels"}, "vision: no max_pixels"},
		{[]string{"vision", "layer_words"}, "vision: no layer_words"},
		{[]string{"transcription", "endpoint"}, "transcription: no endpoint"},
		{[]string{"transcription", "model"}, "transcription: no model"},
		{[]string{"transcription", "max_tokens"}, "transcription: no max_tokens"},
		{[]string{"extraction", "endpoint"}, "extraction: no endpoint"},
		{[]string{"extraction", "model"}, "extraction: no model"},
		{[]string{"extraction", "max_tokens"}, "extraction: no max_tokens"},
		{[]string{"extraction", "window_runes"}, "extraction: no window_runes"},
		{[]string{"embeddings", "endpoint"}, "embeddings: no endpoint"},
		{[]string{"embeddings", "model"}, "embeddings: no model"},
		{[]string{"embeddings", "dimensions"}, "embeddings: no dimensions"},
		{[]string{"embeddings", "tokenizer"}, "embeddings: no tokenizer"},
		{[]string{"embeddings", "batch"}, "embeddings: no batch"},
		{[]string{"embeddings", "chunk_size"}, "embeddings: no chunk_size"},
		{[]string{"embeddings", "chunk_runes"}, "embeddings: no chunk_runes"},
		{[]string{"embeddings"}, "embeddings: no endpoint"},
		{[]string{"pending"}, "no pending"},
		{[]string{"batch_bytes"}, "no batch_bytes"},
		{[]string{"words_per_second"}, "no words_per_second"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "."), func(t *testing.T) {
			_, err := profile.Decode([]byte(without(t, tc.path...)))
			if !errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Decode without %s: %v, want ErrRequest naming %q", strings.Join(tc.path, "."), err, tc.want)
			}
		})
	}
}

func TestDecodeRefusesAValueOutOfRange(t *testing.T) {
	cases := []struct {
		path  []string
		value int
		want  string
	}{
		{[]string{"vision", "max_tokens"}, 0, "vision: max_tokens 0 is below 1"},
		{[]string{"vision", "max_pixels"}, -1, "vision: max_pixels -1 is below 0"},
		{[]string{"vision", "layer_words"}, -1, "vision: layer_words -1 is below 0"},
		{[]string{"transcription", "max_tokens"}, -1, "transcription: max_tokens -1 is below 0"},
		{[]string{"extraction", "max_tokens"}, 0, "extraction: max_tokens 0 is below 1"},
		{[]string{"extraction", "window_runes"}, 0, "extraction: window_runes 0 is below 1"},
		{[]string{"embeddings", "dimensions"}, 0, "embeddings: dimensions 0 is below 1"},
		{[]string{"embeddings", "batch"}, 0, "embeddings: batch 0 is below 1"},
		{[]string{"embeddings", "chunk_size"}, 0, "embeddings: chunk_size 0 is below 1"},
		{[]string{"embeddings", "chunk_runes"}, 0, "embeddings: chunk_runes 0 is below 1"},
		{[]string{"pending"}, 0, "pending 0 is below 1"},
		{[]string{"batch_bytes"}, 0, "batch_bytes 0 is below 1"},
		{[]string{"words_per_second"}, 0, "words_per_second 0 is below 1"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.path, "."), func(t *testing.T) {
			_, err := profile.Decode([]byte(edited(t, tc.path, tc.value)))
			if !errors.Is(err, docinfer.ErrRequest) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Decode with %s %d: %v, want ErrRequest naming %q", strings.Join(tc.path, "."), tc.value, err, tc.want)
			}
		})
	}
}

func TestDecodeAcceptsZeroWhereZeroSendsNone(t *testing.T) {
	for _, path := range [][]string{{"vision", "max_pixels"}, {"vision", "layer_words"}, {"transcription", "max_tokens"}} {
		if _, err := profile.Decode([]byte(edited(t, path, 0))); err != nil {
			t.Errorf("Decode with %s 0: %v", strings.Join(path, "."), err)
		}
	}
}
