package cmd

import (
	"errors"
	"testing"
)

func TestCompileNotImplemented(t *testing.T) {
	_, err := executeArgs(t, "compile")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("compile: want ErrNotImplemented, got %v", err)
	}
}
