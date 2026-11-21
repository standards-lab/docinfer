package docinfer_test

import (
	"testing"
	"time"
	_ "time/tzdata"
)

// See docs/development.md
func TestMain(m *testing.M) {
	zone, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	time.Local = zone
	m.Run()
}
