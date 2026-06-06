package cmd

import (
	"errors"
	"testing"
)

func TestValidateNotImplemented(t *testing.T) {
	_, err := executeArgs(t, "validate")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("validate: want ErrNotImplemented, got %v", err)
	}
}
