package docinfer

// DecodeHeld exports decodeHeld to the black-box tests.
var DecodeHeld = decodeHeld

// BoundsDo exports Bounds.do, the runner every model request goes through, to the black-box
// tests, which prove the bound with go-ai client calls until Pipeline.Run's tests can.
var BoundsDo = (*Bounds).do
