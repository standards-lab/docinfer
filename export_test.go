package docinfer

import "time"

// OnWait sets f to be called as a waiter of p's claim table starts to wait, so a test holds a
// leader's request until its waiters are waiting, without a sleep. It is set before p runs a job.
func OnWait(p *Pipeline, f func()) { p.claims.onWait = f }

// OnSlotWait sets f to be called with a request's capability as the request finds b's slots for
// it all held and starts to wait, so a test sees requests held at a bound without a sleep. It is
// set before any pipeline on b runs a job.
func OnSlotWait(b *Bounds, f func(Capability)) { b.onWait = f }

// SetClock sets the clock p coalesces an extraction's progress by, so a test steps it per text
// delta rather than sleep. It is set before p runs a job.
func SetClock(p *Pipeline, now func() time.Time) { p.now = now }

// LayerWords counts the words in a page's text layer as [Pipeline.Run] does.
func LayerWords(text string) int { return layerWords(text) }
