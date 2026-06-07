package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/pretty"
)

// Fixture paths relative to the cmd package directory.
const (
	pgModel  = "../internal/model/testdata/ecommerce_postgres.yaml"
	bqModel  = "../internal/model/testdata/ecommerce_bigquery.yaml"
	simpleQ  = "../internal/query/testdata/simple.json"
	crossQ   = "../internal/query/testdata/cross_dataset.json"
	pgGolden = "../internal/codegen/postgres/testdata/golden/simple.sql"
	bqGolden = "../internal/codegen/bigquery/testdata/golden/simple.sql"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// executeSplit runs the root command capturing stdout and stderr separately.
func executeSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var out, errb strings.Builder
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errb.String(), err
}

// TestCompilePostgresPretty checks --pretty output matches the postgres golden.
func TestCompilePostgresPretty(t *testing.T) {
	stdout, _, err := executeSplit(t, "compile", "-m", pgModel, "-q", simpleQ, "-d", "postgres", "--pretty")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := strings.TrimRight(readFile(t, pgGolden), "\n")
	if strings.TrimRight(stdout, "\n") != want {
		t.Errorf("pretty SQL mismatch\n--- want ---\n%s\n--- got ---\n%s", want, stdout)
	}
}

// TestCompileBigQueryPretty checks the bigquery dialect dispatches and matches.
func TestCompileBigQueryPretty(t *testing.T) {
	stdout, _, err := executeSplit(t, "compile", "-m", bqModel, "-q", simpleQ, "-d", "bigquery", "--pretty")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := strings.TrimRight(readFile(t, bqGolden), "\n")
	if strings.TrimRight(stdout, "\n") != want {
		t.Errorf("bigquery SQL mismatch\n--- want ---\n%s\n--- got ---\n%s", want, stdout)
	}
}

// TestCompileDefaultCompact checks that without --pretty the SQL is one line.
func TestCompileDefaultCompact(t *testing.T) {
	stdout, _, err := executeSplit(t, "compile", "-m", pgModel, "-q", simpleQ, "-d", "postgres")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got := strings.TrimRight(stdout, "\n")
	if strings.Contains(got, "\n") {
		t.Errorf("default output should be a single compact line, got:\n%s", got)
	}
	want := pretty.Compact(readFile(t, pgGolden))
	if got != want {
		t.Errorf("compact SQL mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// TestCompileExplain checks --explain prints the plan to stderr, SQL to stdout.
func TestCompileExplain(t *testing.T) {
	stdout, stderr, err := executeSplit(t, "compile", "-m", pgModel, "-q", crossQ, "-d", "postgres", "--explain")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(stderr, "PLAN:") {
		t.Errorf("--explain should print PLAN to stderr, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Join(") {
		t.Errorf("cross-dataset plan should mention a Join, got:\n%s", stderr)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "SELECT") {
		t.Errorf("SQL should go to stdout, got:\n%s", stdout)
	}
}

// TestCompileMissingModel checks the required --model flag is enforced.
func TestCompileMissingModel(t *testing.T) {
	_, _, err := executeSplit(t, "compile", "-q", simpleQ, "-d", "postgres")
	if err == nil {
		t.Fatal("missing --model should error")
	}
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("error should mention the model flag, got: %v", err)
	}
}

// TestCompileInvalidDialect checks an unknown dialect is rejected.
func TestCompileInvalidDialect(t *testing.T) {
	_, _, err := executeSplit(t, "compile", "-m", pgModel, "-q", simpleQ, "-d", "oracle")
	if err == nil {
		t.Fatal("unknown dialect should error")
	}
	if !strings.Contains(err.Error(), "dialect") {
		t.Errorf("error should mention the dialect, got: %v", err)
	}
}

// TestCompileFanOut checks a fan-out query is refused with a clear message.
func TestCompileFanOut(t *testing.T) {
	q := writeTemp(t, "fanout.json", `{
		"metrics": ["orders.revenue"],
		"dimensions": ["products.category"]
	}`)
	_, _, err := executeSplit(t, "compile", "-m", pgModel, "-q", q, "-d", "postgres")
	if err == nil {
		t.Fatal("fan-out query should error")
	}
	if !strings.Contains(err.Error(), "fan-out") {
		t.Errorf("error should mention fan-out, got: %v", err)
	}
}

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp %s: %v", name, err)
	}
	return path
}
