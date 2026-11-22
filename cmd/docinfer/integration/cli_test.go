//go:build integration

package integration_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docinfer/cmd/docinfer/integration"
)

var versionLines = regexp.MustCompile(`^command \S+\nlibrary (v\d+\.\d+\.\d+\S*|\(devel\))\n$`)

func TestCLI_Version(t *testing.T) {
	res := integration.Run(t, "version")

	if res.Code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	if !versionLines.MatchString(res.Stdout) {
		t.Errorf("stdout = %q, want the command's and the library's versions, matching %s", res.Stdout, versionLines)
	}
	if res.Stderr != "" {
		t.Errorf("stderr = %q, want empty", res.Stderr)
	}
}

func TestCLI_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := integration.Run(t, args...)

			if res.Code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", res.Code, process.ExitUsage, res.Stderr)
			}
			if !strings.Contains(res.Stdout, "version") {
				t.Errorf("stdout = %q, want the root help listing version", res.Stdout)
			}
			if res.Stderr != "" {
				t.Errorf("stderr = %q, want empty", res.Stderr)
			}
		})
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	res := integration.Run(t, "bogus")

	if res.Code != process.ExitUsage {
		t.Errorf("code = %d, want %d", res.Code, process.ExitUsage)
	}
	if want := `docinfer: unknown command "bogus"`; !strings.HasPrefix(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", res.Stderr, want)
	}
	if !strings.Contains(res.Stderr, "version") {
		t.Errorf("stderr = %q, want the usage listing version", res.Stderr)
	}
	if res.Stdout != "" {
		t.Errorf("stdout = %q, want empty", res.Stdout)
	}
}
