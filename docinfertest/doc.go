// Package docinfertest holds docinfer's cache contract as a conformance suite, for every
// implementation of docinfer.Cache to run, the module's own and a provider's. It imports the
// docinfer root and the standard library alone.
//
// Cache runs the contract against a new, empty cache for each case: a miss is nil; a Get of many
// keys answers each in order, a key asked twice twice; a Put replaces, a shorter value a longer;
// values come back whole, large and binary ones among them, and share no slice with the caller; an
// empty value is refused, matching docinfer.ErrRequest, and stays a miss; a text's chunk spans,
// and the empty spans of a text of no chunks, come back byte for byte; the same content under each
// capability is five keys; an ended context fails Get and Put; and concurrent Puts and Gets
// of one key and of many leave each key one value put whole.
//
// The package exports:
//
//   - [Cache], the cache contract
package docinfertest
