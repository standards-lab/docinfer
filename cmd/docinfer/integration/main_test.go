//go:build integration

package integration_test

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/standards-lab/docinfer/cmd/docinfer/integration"
)

// See docs/development.md
func TestMain(m *testing.M) {
	zone, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	time.Local = zone
	integration.Main(m)
}
