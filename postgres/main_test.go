package postgres_test

import (
	"testing"
	"time"
	_ "time/tzdata" // the zone loads without the host's zoneinfo
)

// TestMain runs the package's tests with time.Local in Asia/Kolkata, whose offset is never zero,
// so a time that leaves the code unconverted to UTC reads differently from one that is.
func TestMain(m *testing.M) {
	zone, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	time.Local = zone
	m.Run()
}
