package cmd

import (
	"strings"
	"testing"
)

// TestValidateValidModel checks a valid model exits 0 with a confirmation.
func TestValidateValidModel(t *testing.T) {
	stdout, _, err := executeSplit(t, "validate", "-m", pgModel)
	if err != nil {
		t.Fatalf("validate valid model: unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("expected a validity confirmation, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "ecommerce") {
		t.Errorf("confirmation should name the model, got:\n%s", stdout)
	}
}

// TestValidateInvalidModel checks an invalid model exits non-zero and the
// error names the offending field.
func TestValidateInvalidModel(t *testing.T) {
	m := writeTemp(t, "invalid.yaml", `
version: "0.2.0"
semantic_model:
  - name: ""
    datasets: []
`)
	_, _, err := executeSplit(t, "validate", "-m", m)
	if err == nil {
		t.Fatal("invalid model should error")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("error should describe the missing name, got: %v", err)
	}
}

// TestValidateMissingFlag checks the required --model flag is enforced.
func TestValidateMissingFlag(t *testing.T) {
	_, _, err := executeSplit(t, "validate")
	if err == nil {
		t.Fatal("missing --model should error")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("error should mention the model flag, got: %v", err)
	}
}
