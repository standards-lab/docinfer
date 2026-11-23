package app

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/docinfer"
)

const versionExitStatus = `Exit status:
  0  both versions were printed
  1  they couldn't be written
  2  a usage error, such as an argument
`

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:    "version",
		Summary: "Print the docinfer command's and library's versions",
		Footer:  func(w io.Writer) { _, _ = io.WriteString(w, versionExitStatus) },
		Args:    cli.NoArgs,
		Run: func(_ context.Context, inv *cli.Invocation) error {
			command, library := versions()
			_, err := fmt.Fprintf(inv.Stdout, "command %s\nlibrary %s\n", command, library)
			return err
		},
	}
}

const devel = "(devel)"

var libraryPath = reflect.TypeFor[docinfer.Pipeline]().PkgPath()

func versions() (command, library string) {
	command, library = devel, devel
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return command, library
	}
	if info.Main.Version != "" {
		command = info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != libraryPath {
			continue
		}
		switch {
		case dep.Replace == nil && dep.Version != "":
			library = dep.Version
		case dep.Replace != nil && dep.Replace.Version != "":
			library = dep.Replace.Version
		}
	}
	return command, library
}
