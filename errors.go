package docinfer

import "errors"

// ErrRequest is the cause of a refused request, before anything is asked of a model or a store:
// a [New] whose Config holds a count or bound below 1, an empty model, or a Native that isn't a
// JSON object with unique member names, a [NewBounds] of a bound below 1, a [NewFileCache] with no
// directory, and a [FileCache] or [MemoryCache] asked to hold an empty value, or a FileCache given
// a key not of [Key]'s form, and a [Pipeline.Run] given an [Extract] whose name, prompt, or schema
// is empty or invalid.
var ErrRequest = errors.New("docinfer: invalid request")
