package cmd

import (
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	out, err := executeArgs(t, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	if !strings.Contains(out, version) {
		t.Errorf("version output %q does not contain %q", out, version)
	}
}
