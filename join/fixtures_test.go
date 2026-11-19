//go:build linux

package join_test

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
)

// The streams fixtures' truth files, which scripts/fixtures-streams.sh writes beside the fixtures
// in testdata.
const (
	levelsTruth = "speech-levels.truth.json"
)

// gapTruth is an inserted gap of digital silence in a truth file, in milliseconds.
type gapTruth struct {
	StartMS int `json:"start_ms"`
	EndMS   int `json:"end_ms"`
}

// levelsFixture is speech-levels.truth.json: the level-change variants, the same speech changed
// at ChangeMS, and the threshold fixed from the speech before it.
type levelsFixture struct {
	Source        string     `json:"source"`
	SampleRate    int        `json:"sample_rate"`
	DurationMS    int        `json:"duration_ms"`
	AttenuationDB float64    `json:"attenuation_db"`
	Floor         noise      `json:"floor"`
	Gaps          []gapTruth `json:"gaps"`
	Transcript    []string   `json:"transcript"`
	ChangeMS      int        `json:"change_ms"`
	// FirstMeanVolumeDB is volumedetect's mean volume of the speech before the change, and
	// FixedThresholdDB that less audio.DefaultBelow.
	FirstMeanVolumeDB float64    `json:"first_mean_volume_db"`
	FixedThresholdDB  float64    `json:"fixed_threshold_db"`
	MinSilenceMS      int        `json:"min_silence_ms"`
	GapsAfterChange   []gapTruth `json:"gaps_after_change"`
	Variants          []struct {
		File   string  `json:"file"`
		Change string  `json:"change"`
		GainDB float64 `json:"gain_db,omitzero"`
		Noise  *noise  `json:"noise,omitempty"`
	} `json:"variants"`
}

// noise is a seeded anoisesrc in a truth file.
type noise struct {
	Color     string  `json:"color"`
	Amplitude float64 `json:"amplitude"`
	Seed      int     `json:"seed"`
}

// readTruth decodes the truth file name in testdata into v, refusing a member v doesn't name.
func readTruth(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(data, v, jsonv2.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}
