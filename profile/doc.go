// Package profile builds a docinfer.Pipeline from a profile's settings as JSON, for any host: the
// settings of each capability, vision, transcription, extraction, and embeddings with its chunker,
// and of the pipeline, under the keys cmd/docinfer's configuration names. Nothing has a default:
// [Settings.Validate] refuses each required value left unset by its key, and [Decode] refuses a
// member it doesn't name.
//
// The host builds each capability's go-ai client, against the endpoint the capability's settings
// name, with its own transport, retry, and bounds, and passes them as [Clients]; the package
// builds no client, reads no environment, writes no file, and sets no deadline. A client's
// options are the source of what it sends: transcription's mode and the embeddings' input-type
// conventions are the clients', as docinfer reads them.
//
// [Resolve] resolves each capability's served model, the model the deployment serves under the
// one its requests name, which keys docinfer's results: the entry's root in the model listing the
// capability's client's Models returns, else the capability's served_model. It refuses a root
// that disagrees with a set served_model, and a model with neither, naming the capability, the
// endpoint, and the model. [New] builds the pipeline of the settings over the clients, the
// [Model]s Resolve resolved, and the host's docinfer.Bounds and docinfer.Cache. Its chunker
// counts a text's tokens through the tokenizer client's Tokenize.
//
// The package exports:
//
//   - [Settings], its [Capability], [Vision], [Transcription], [Extraction], and [Embeddings]
//     settings, [Settings.Validate], and [Decode], which decodes and validates them
//   - [Clients], the host's client for each capability and the tokenizer
//   - [Resolve], each capability's [Model] and its listing's [State]: [Listed], [Unlisted],
//     [Unchecked], or [Unreachable]
//   - [Config] and [New], which builds the docinfer.Pipeline
package profile
