package docinfer

import "time"

// DecodeHeld exports decodeHeld to the black-box tests.
var DecodeHeld = decodeHeld

// BoundsDo exports Bounds.do, the runner every model request goes through, to the black-box
// tests, which prove each capability's bound with one kind of go-ai client call: Pipeline.Run
// reaches all four bounds, and BoundsDo proves each one alike, its cap, its independence of the
// others, its wait's end with the context, and its slot freed on a panic, without a job per case.
var BoundsDo = (*Bounds).do

// OnWait sets f to be called as a waiter of p's claim table starts to wait, so a test holds a
// leader's request until its waiters are waiting, without a sleep. It is set before p runs a job.
func OnWait(p *Pipeline, f func()) { p.claims.onWait = f }

// SetClock sets the clock p coalesces an extraction's progress by, so a test steps it per text
// delta rather than sleep. It is set before p runs a job.
func SetClock(p *Pipeline, now func() time.Time) { p.now = now }
