package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// executeArgs runs the root command with the given args, capturing stdout and
// stderr into a single buffer, and returns the buffer contents and any error.
func executeArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err := root.Execute()
	return buf.String(), err
}

func TestRootHelp(t *testing.T) {
	out, err := executeArgs(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(out, "gavagai") {
		t.Errorf("help output missing program name; got:\n%s", out)
	}
	for _, sub := range []string{"compile", "validate", "version"} {
		if !strings.Contains(out, sub) {
			t.Errorf("help output missing %q subcommand; got:\n%s", sub, out)
		}
	}
}
