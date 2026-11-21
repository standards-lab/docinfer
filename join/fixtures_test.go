package join_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// From spike-ai-pipeline-processing (v0.9.0)
const levelsTruth = "speech-levels.truth.json"

type levelsFixture struct {
	Transcript []string `json:"transcript"`
}

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
