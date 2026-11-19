//go:build linux

package join

import "errors"

// ErrRequest is the cause of a refused request: a field out of range, before anything runs.
var ErrRequest = errors.New("invalid request")
