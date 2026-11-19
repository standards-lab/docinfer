package join

import "errors"

// ErrRequest is the cause of a refused request: a Request's WordsPerSecond below 1, or seams that
// don't fit the transcripts, before anything runs.
var ErrRequest = errors.New("join: invalid request")
