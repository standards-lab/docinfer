package app

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"

	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/docinfer"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:    "version",
		Summary: "Print the docinfer command's and library's versions",
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
