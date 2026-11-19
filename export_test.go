package docinfer

// DecodeHeld exports decodeHeld to the black-box tests.
var DecodeHeld = decodeHeld

// BoundsDo exports Bounds.do, the runner every model request goes through, to the black-box
// tests, which prove each capability's bound with go-ai client calls: Pipeline.Run's own tests
// reach embeddings' bound alone until vision, transcription, and extraction run.
var BoundsDo = (*Bounds).do
