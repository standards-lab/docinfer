package docinfer

// DecodeHeld exports decodeHeld to the black-box tests.
var DecodeHeld = decodeHeld

// BoundsDo exports Bounds.do, the runner every model request goes through, to the black-box
// tests, which prove each capability's bound with go-ai client calls: Pipeline.Run's own tests
// reach embeddings' bound alone until vision, transcription, and extraction run.
var BoundsDo = (*Bounds).do

// OnWait sets f to be called as a waiter of p's claim table starts to wait, so a test holds a
// leader's request until its waiters are waiting, without a sleep. It is set before p runs a job.
func OnWait(p *Pipeline, f func()) { p.claims.onWait = f }
