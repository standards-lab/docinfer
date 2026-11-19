package join_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// levelsTruth is a streams fixture's truth file, copied from spike-ai-pipeline-processing v0.9.0's
// audio/testdata, whose speech's transcript the settled-prefix test cuts into windows.
const levelsTruth = "speech-levels.truth.json"

// levelsFixture is the part of speech-levels.truth.json the tests read: the speech's transcript,
// as the truth file's lines of it.
type levelsFixture struct {
	Transcript []string `json:"transcript"`
}

// readTruth decodes the truth file name in testdata into v.
func readTruth(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}
