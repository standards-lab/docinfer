// Package app is docinfer's composition root. [New] describes the program's dependencies as one
// go-core graph and builds the command tree on go-cli-sdk over it; [App.Run] runs that tree, as a
// go-cli-sdk cli.Program, on the process arguments. It was started from go-cli-sdk-template's
// template/v0.3.1. Each architecture layer has one file, which defines the layer's nodes in its
// define function and owns its mount, so the layer files are the architecture's layer list:
//
//   - app.go: [App], [New], and [Nodes], which call the layer files' define and mount functions
//     in layer order, lowest first;
//   - infrastructure.go: defineInfrastructure: the lifecycle configuration; [Config], read from
//     the directory the root flag --config or DOCINFER_CONFIG names, the flag winning; each
//     endpoint's one HTTP client, over a clone of the default transport with the endpoint's
//     ResponseHeaderTimeout; and each capability's own *ai.Client, vision's, transcription's,
//     and embeddings', in its endpoint's format and with its endpoint's options, transcription's
//     in its configured mode, a capability the configuration leaves unset having none. A client
//     go-ai's ai.New refuses is reported named for its endpoint. config.go holds [Config], its
//     starting values, its overrides, and its checks, so a configuration error fails the Build,
//     labelled "config", before any command reads anything;
//   - domain.go: defineDomain and mountDomain: the run configuration, internal/run's Config, in
//     the library's shapes over the capabilities' clients, with each capability's endpoint and
//     the build's versions its manifests record, and the run command over it, and the report
//     command, which declares no nodes;
//   - version.go: the version command, which declares no nodes and prints two lines, the
//     command's version, the main module's, and the library's, the docinfer module's the binary
//     was built with, each read from the build info, or (devel) when the binary carries none, as
//     under go test or in the workspace.
//
// Each command's help, the root's among them, ends with its exit status: 0, 1, and 2, help and a
// usage error being 2.
//
// envPrefix, in infrastructure.go, is the single constant every DOCINFER_ variable name derives
// from; go-core's config convention composes the rest, such as DOCINFER_SHUTDOWN_TIMEOUT and
// DOCINFER_ENDPOINTS_<NAME>_RESPONSE_HEADER_TIMEOUT, each where it is read. Every
// infrastructure value is read or constructed in its node's constructor, which runs only when a
// Build reaches the node, so configuration is read only for a command that declares a node, never
// in [New].
//
// [Nodes] holds one handle per node: each define function fills its own part of one Nodes
// value, and its constructors read the lower layers' nodes from it with Use. Each field's node is
// named for the field, so no comment names it; graph_test.go checks that rule with go-core's
// graphtest.CheckNames, and checks that every leaf of the tree, as Command.Leaves yields it,
// builds its nodes without dropping an order between layers. Defining the nodes constructs
// nothing. A command declares the nodes it needs with Use; the dispatcher builds them, with the
// lifecycle configuration node, only when the command runs, starts what was built layer by
// layer, and shuts it down in reverse when the command returns. A run that builds nothing, such
// as help, a usage error, or version, reads no configuration; help and a usage error exit 2.
//
// The App publishes its composition: [App.Graph], [App.Nodes], and [App.Root] return the graph,
// a handle on each of its nodes, and the command tree. The program itself never calls them; they
// are for a caller that observes what a run builds, Replaces a node's constructor with a
// substitute, or adds a command over the nodes, before the App runs, as internal/apptest's
// fixtures do.
package app
