package postgres_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/vincentk1991/gavagai/internal/codegen"
	_ "github.com/vincentk1991/gavagai/internal/codegen/postgres" // register dialect
	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

var update = flag.Bool("update", false, "overwrite golden files with current output")

// modelDir and queryDir are relative to this test file's location.
const (
	modelDir = "../../model/testdata"
	queryDir = "../../query/testdata"
)

// compile runs the full pipeline for the named fixture pair and returns SQL.
func compile(t *testing.T, modelFile, queryFile string) string {
	t.Helper()

	doc, err := model.ParseFile(filepath.Join(modelDir, modelFile))
	if err != nil {
		t.Fatalf("parse model %s: %v", modelFile, err)
	}
	m := &doc.Models[0]

	q, err := query.ParseFile(filepath.Join(queryDir, queryFile))
	if err != nil {
		t.Fatalf("parse query %s: %v", queryFile, err)
	}

	plan, err := planner.Plan(q, m)
	if err != nil {
		t.Fatalf("Plan(%s, %s): %v", modelFile, queryFile, err)
	}
	plan = planner.PushDown(plan)

	sql, err := codegen.Compile(plan, "postgres")
	if err != nil {
		t.Fatalf("Compile(%s, %s): %v", modelFile, queryFile, err)
	}
	return sql
}

// checkGolden compares got against the golden file at testdata/golden/<name>.
// Run with -update to regenerate golden files.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `go test -update` to create)", path, err)
	}
	if got != string(want) {
		t.Errorf("SQL output does not match golden file %s\n--- want ---\n%s\n--- got ---\n%s",
			name, want, got)
	}
}

// TestEmitSQL verifies the full parse→plan→pushdown→emit pipeline against
// golden SQL files for a representative set of query patterns.
func TestEmitSQL(t *testing.T) {
	cases := []struct {
		name  string
		model string
		query string
	}{
		{"simple", "ecommerce_postgres.yaml", "simple.json"},
		{"by_status_filtered", "ecommerce_postgres.yaml", "by_status_filtered.json"},
		{"with_having", "ecommerce_postgres.yaml", "with_having.json"},
		{"with_order_limit", "ecommerce_postgres.yaml", "with_order_limit.json"},
		{"cross_dataset", "ecommerce_postgres.yaml", "cross_dataset.json"},
		{"multi_metric", "ecommerce_postgres.yaml", "multi_metric.json"},
		{"null_filter", "ecommerce_postgres.yaml", "null_filter.json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compile(t, tc.model, tc.query)
			checkGolden(t, tc.name+".sql", got)
		})
	}
}

// TestEmitDialects verifies that the same plan compiled for both recognised
// dialects produces valid SQL for postgres and ErrNotImplemented for bigquery
// (which is not yet registered). This will flip when Phase 6 is implemented.
func TestEmitDialects(t *testing.T) {
	doc, err := model.ParseFile(filepath.Join(modelDir, "ecommerce_postgres.yaml"))
	if err != nil {
		t.Fatalf("parse model: %v", err)
	}
	q := &query.Query{
		Metrics:    []string{"orders.revenue"},
		Dimensions: []string{"orders.region"},
	}
	plan, err := planner.Plan(q, &doc.Models[0])
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan = planner.PushDown(plan)

	sql, err := codegen.Compile(plan, "postgres")
	if err != nil {
		t.Errorf("postgres should compile successfully, got %v", err)
	}
	if sql == "" {
		t.Error("postgres compile returned empty SQL")
	}

	_, err = codegen.Compile(plan, "bigquery")
	if err == nil {
		t.Error("bigquery should not yet compile (pending Phase 6)")
	}
}

// TestEmitMissingExpression verifies that the emitter returns an error when a
// field has no POSTGRES or ANSI_SQL expression entry.
func TestEmitMissingExpression(t *testing.T) {
	ansi := func(e string) model.Expression {
		return model.Expression{Dialects: []model.DialectExpression{{Dialect: "ANSI_SQL", Expression: e}}}
	}
	snowflakeOnly := model.Expression{Dialects: []model.DialectExpression{{Dialect: "SNOWFLAKE", Expression: "broken"}}}

	m := &model.SemanticModel{
		Name: "broken",
		Datasets: []model.Dataset{{
			Name: "t", Source: "s.t",
			Fields: []model.Field{
				{Name: "dim", Expression: snowflakeOnly, Dimension: &model.Dimension{}},
			},
		}},
		Metrics: []model.Metric{{Name: "cnt", Expression: ansi("COUNT(*)")}},
	}
	q := &query.Query{Metrics: []string{"t.cnt"}, Dimensions: []string{"t.dim"}}

	plan, err := planner.Plan(q, m)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	_, err = codegen.Compile(plan, "postgres")
	if err == nil {
		t.Fatal("want error for SNOWFLAKE-only field with postgres dialect, got nil")
	}
}

// TestEmitSelectDistinct verifies that a dimensions-only query (no metrics)
// is rendered as SELECT DISTINCT rather than GROUP BY.
func TestEmitSelectDistinct(t *testing.T) {
	doc, err := model.ParseFile(filepath.Join(modelDir, "ecommerce_postgres.yaml"))
	if err != nil {
		t.Fatalf("parse model: %v", err)
	}
	q := &query.Query{Dimensions: []string{"orders.status"}}
	plan, err := planner.Plan(q, &doc.Models[0])
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	plan = planner.PushDown(plan)

	sql, err := codegen.Compile(plan, "postgres")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !containsStr(sql, "SELECT DISTINCT") {
		t.Errorf("dimensions-only query should use SELECT DISTINCT:\n%s", sql)
	}
	if containsStr(sql, "GROUP BY") {
		t.Errorf("dimensions-only query must not have GROUP BY:\n%s", sql)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
