package docinfer

import "errors"

// ErrRequest is the cause of a refused request: a [NewFileCache] with no directory, and a
// [FileCache] or [MemoryCache] asked to hold an empty value, or a FileCache given a key not of
// [Key]'s form, before anything is read or written.
var ErrRequest = errors.New("docinfer: invalid request")
