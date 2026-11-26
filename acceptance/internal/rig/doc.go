// Package rig is what the acceptance tools share: the endpoint a measurement sends to, taken from
// its flags and the environment, a go-ai client of it, and an in-process embeddings endpoint, so a
// measurement loads only the row it measures.
//
// Its exported identifiers:
//
//   - [Endpoint] is the row a measurement sends to; [Endpoint.FromEnv] fills it from go-ai's tier
//     variables, and [Endpoint.Client] returns a client of it.
//   - [Embeddings] serves a fake embeddings route on loopback.
//   - [Append] appends a value to a JSON-lines file.
package rig
