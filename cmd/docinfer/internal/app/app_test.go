package app_test

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/docinfer/cmd/docinfer/internal/app"
	"github.com/standards-lab/docinfer/cmd/docinfer/internal/apptest"
)

var shutdownTimeout = lifecycle.NewEnv("DOCINFER").ShutdownTimeout

const unreadableDuration = "not-a-duration"

var versionLines = regexp.MustCompile(`^command \(devel\)\nlibrary (v\d+\.\d+\.\d+\S*|\(devel\))\n$`)

func newApp(t *testing.T) (a *app.App, stdout, stderr *bytes.Buffer, built *apptest.Recorder) {
	t.Helper()
	stdout, stderr = new(bytes.Buffer), new(bytes.Buffer)
	a = app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr})
	return a, stdout, stderr, apptest.Builds(a)
}

func probe(a *app.App) *bool {
	ran := new(bool)
	a.Root().Add((&cli.Command{
		Name:    "probe",
		Summary: "Build the lifecycle config",
		Run: func(context.Context, *cli.Invocation) error {
			*ran = true
			return nil
		},
	}).Use(a.Nodes().LifecycleConfig))
	return ran
}

func TestRun_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv(shutdownTimeout, unreadableDuration)
			a, stdout, stderr, built := newApp(t)

			code := a.Run(context.Background(), args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitUsage, stderr)
			}
			if !strings.Contains(stdout.String(), "version") {
				t.Errorf("stdout = %q, want the root help listing version", stdout)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if log := built.Log(); len(log) != 0 {
				t.Errorf("nodes built = %q, want none", log)
			}
		})
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, unreadableDuration)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"bogus"})

	if code != process.ExitUsage {
		t.Errorf("code = %d, want %d", code, process.ExitUsage)
	}
	if want := `docinfer: unknown command "bogus"`; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestRun_Version(t *testing.T) {
	t.Setenv(shutdownTimeout, unreadableDuration)
	a, stdout, stderr, built := newApp(t)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !versionLines.MatchString(stdout.String()) {
		t.Errorf("stdout = %q, want the command's version, (devel) under go test, and the library's, matching %s", stdout, versionLines)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestNew_IsCold(t *testing.T) {
	t.Setenv(shutdownTimeout, unreadableDuration)

	a, stdout, stderr, _ := newApp(t)

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("New() wrote %q, %q, want nothing", stdout, stderr)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Replace after New() panicked: %v; want the graph unbuilt", r)
		}
	}()
	apptest.Halt(a)
}

func TestRun_CommandBuildsTheLifecycleConfig(t *testing.T) {
	t.Setenv(shutdownTimeout, "")
	a, _, stderr, built := newApp(t)
	ran := probe(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !*ran {
		t.Error("probe did not run")
	}
	if want := []string{"lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

func TestRun_InvalidShutdownTimeoutFailsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, unreadableDuration)
	a, _, stderr, _ := newApp(t)
	ran := probe(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *ran {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "docinfer probe: lifecycle config: DOCINFER_SHUTDOWN_TIMEOUT"; !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
}

func TestRun_HaltStopsTheCommand(t *testing.T) {
	t.Setenv(shutdownTimeout, unreadableDuration)
	a, stdout, stderr, built := newApp(t)
	ran := probe(a)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"probe"})

	if code != process.ExitFailure {
		t.Errorf("code = %d, want %d", code, process.ExitFailure)
	}
	if *ran {
		t.Error("probe ran, want it stopped by the Build")
	}
	if want := "docinfer probe: lifecycle config: " + apptest.ErrHalted.Error(); !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to start with %q", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := []string{"lifecycle config"}; !slices.Equal(built.Log(), want) {
		t.Errorf("nodes built = %q, want %q", built.Log(), want)
	}
}

func TestRun_HaltLeavesVersion(t *testing.T) {
	a, stdout, stderr, built := newApp(t)
	apptest.Halt(a)

	code := a.Run(context.Background(), []string{"version"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d; stderr = %q", code, process.ExitOK, stderr)
	}
	if !versionLines.MatchString(stdout.String()) {
		t.Errorf("stdout = %q, want the command's version, (devel) under go test, and the library's, matching %s", stdout, versionLines)
	}
	if log := built.Log(); len(log) != 0 {
		t.Errorf("nodes built = %q, want none", log)
	}
}

func TestRun_HelpEndsWithItsExitStatus(t *testing.T) {
	for _, tc := range []struct {
		args []string
		says string
	}{
		{[]string{"--help"}, "2  a usage error, such as no command or an unknown one"},
		{[]string{"version", "--help"}, "0  both versions were printed"},
		{[]string{"run", "--help"}, "0  the run ended"},
		{[]string{"report", "--help"}, "1  nothing was rendered"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			a, stdout, stderr, _ := newApp(t)

			code := a.Run(context.Background(), tc.args)

			if code != process.ExitUsage {
				t.Errorf("code = %d, want %d", code, process.ExitUsage)
			}
			help := stdout.String() + stderr.String()
			_, footer, ok := strings.Cut(help, "\nExit status:\n")
			if !ok || !strings.Contains(footer, tc.says) {
				t.Fatalf("help = %q, want an exit status saying %q", help, tc.says)
			}
			for line := range strings.Lines(footer) {
				if !strings.HasPrefix(line, "  ") {
					t.Errorf("help's line %q follows its exit status, want the exit status last", line)
				}
			}
			for _, status := range []string{"  0  ", "  1  ", "  2  "} {
				if !strings.Contains(footer, status) {
					t.Errorf("exit status = %q, want a line for %q", footer, strings.TrimSpace(status))
				}
			}
		})
	}
}
