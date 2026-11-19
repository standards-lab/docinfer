package docinfer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/standards-lab/docinfer"
)

// update rewrites the golden keys from Key as it stands:
//
//	go test . -run TestGoldenKeys -update
//
// Do it only with a new docinfer.Version, which names the file, never to make a changed key pass
// under the old one: a changed key misses every result held under the old, and a key of the old
// form over a new shape decodes wrong.
var update = flag.Bool("update", false, "rewrite the golden keys for docinfer.Version")

// goldenKey is one identity and content and the key they give.
type goldenKey struct {
	Identity docinfer.Identity `json:"identity"`
	Content  string            `json:"content"`
	Key      string            `json:"key"`
}

// goldenKeys are the golden file's inputs, one per capability, each with settings of the form the
// capability's identity gives, the embeddings' "".
var goldenKeys = []goldenKey{
	{
		Identity: docinfer.Identity{
			Capability: docinfer.CapabilityVision, Model: "gpt-4.1-mini",
			Settings: `{"request":"1","prompt":"Describe the image.","max_tokens":1024,"effort":"low"}`,
		},
		Content: sha("an image"),
	},
	{
		Identity: docinfer.Identity{
			Capability: docinfer.CapabilityTranscription, Model: "whisper-1",
			Settings: `{"request":"1","mode":"route","prompt":"","language":"en"}`,
		},
		Content: sha("a window"),
	},
	{
		Identity: docinfer.Identity{
			Capability: docinfer.CapabilityExtraction, Model: "gpt-4.1",
			Settings: `{"request":"1","name":"invoice","prompt_sha256":"` + sha("prompt") + `","schema_sha256":"` +
				sha("schema") + `","max_tokens":4096,"effort":"medium"}`,
		},
		Content: sha("a source text"),
	},
	{
		Identity: docinfer.Identity{Capability: docinfer.CapabilityEmbeddings, Model: "text-embedding-3-small"},
		Content:  sha("a chunk's text"),
	},
}

// The committed golden keys pin Key's form under docinfer.Version: a change to any key must come
// with a new version, since the cache holds every result by its key.
func TestGoldenKeys(t *testing.T) {
	var got []goldenKey
	for _, g := range goldenKeys {
		g.Key = docinfer.Key(g.Identity, g.Content)
		got = append(got, g)
	}
	path := filepath.Join("testdata", "keys-v"+docinfer.Version+".json")
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
		t.Fatalf("no golden keys for docinfer.Version %q: %v; write them with -update", docinfer.Version, err)
	}
	var want []goldenKey
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d keys, the golden file %s holds %d: change the version to change the keys", len(got), path, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s key: %+v, the golden file %s has %+v: the key changed, so change the version",
				got[i].Identity.Capability, got[i], path, want[i])
		}
	}
}

// A key is its capability, a slash, and a SHA-256 as lowercase hex, and every part it hashes
// counts, each apart from its neighbours.
func TestKey(t *testing.T) {
	id := docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "m", Settings: "s"}
	key := docinfer.Key(id, sha("c"))
	if !regexp.MustCompile(`^vision/[0-9a-f]{64}$`).MatchString(key) {
		t.Fatalf("Key: %q, want vision/ and 64 lowercase hex digits", key)
	}
	if again := docinfer.Key(id, sha("c")); again != key {
		t.Errorf("Key twice: %q then %q", key, again)
	}
	others := map[string]string{
		"another capability": docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityExtraction, Model: "m", Settings: "s"}, sha("c")),
		"another model":      docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "n", Settings: "s"}, sha("c")),
		"other settings":     docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "m", Settings: "t"}, sha("c")),
		"other content":      docinfer.Key(id, sha("d")),
		// The boundary between two parts moved: only the length prefixes tell these apart.
		"model and settings shifted":   docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "ms", Settings: ""}, sha("c")),
		"settings and content shifted": docinfer.Key(docinfer.Identity{Capability: docinfer.CapabilityVision, Model: "m", Settings: "s" + sha("c")[:1]}, sha("c")[1:]),
	}
	seen := map[string]string{key: "the key"}
	for name, k := range others {
		if prior, ok := seen[k]; ok {
			t.Errorf("%s gives %s's key %q", name, prior, k)
		}
		seen[k] = name
	}
}

// sha returns s's SHA-256 as lowercase hex.
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
