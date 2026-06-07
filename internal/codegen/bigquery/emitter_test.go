package bigquery_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/codegen"
	_ "github.com/vincentk1991/gavagai/internal/codegen/bigquery" // register dialect
	_ "github.com/vincentk1991/gavagai/internal/codegen/postgres" // for cross-dialect divergence test
	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

var update = flag.Bool("update", false, "overwrite golden files with current output")

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

	sql, err := codegen.Compile(plan, "bigquery")
	if err != nil {
		t.Fatalf("Compile(%s, %s): %v", modelFile, queryFile, err)
	}
	return sql
}

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

// TestEmitSQL verifies the full pipeline against golden BigQuery SQL files for
// the same query patterns as the PostgreSQL suite.
func TestEmitSQL(t *testing.T) {
	cases := []struct {
		name  string
		model string
		query string
	}{
		{"simple", "ecommerce_bigquery.yaml", "simple.json"},
		{"by_status_filtered", "ecommerce_bigquery.yaml", "by_status_filtered.json"},
		{"with_having", "ecommerce_bigquery.yaml", "with_having.json"},
		{"with_order_limit", "ecommerce_bigquery.yaml", "with_order_limit.json"},
		{"cross_dataset", "ecommerce_bigquery.yaml", "cross_dataset.json"},
		{"multi_metric", "ecommerce_bigquery.yaml", "multi_metric.json"},
		{"null_filter", "ecommerce_bigquery.yaml", "null_filter.json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := compile(t, tc.model, tc.query)
			checkGolden(t, tc.name+".sql", got)
		})
	}
}

// TestBackquoteQuoting verifies BigQuery uses backtick identifier quoting and
// whole-path table references, distinct from PostgreSQL's double quotes.
func TestBackquoteQuoting(t *testing.T) {
	sql := compile(t, "ecommerce_bigquery.yaml", "cross_dataset.json")
	if !strings.Contains(sql, "`orders`.`customer_id` = `customers`.`customer_id`") {
		t.Errorf("expected backtick-quoted ON condition:\n%s", sql)
	}
	if !strings.Contains(sql, "FROM `my_project.analytics.orders`") {
		t.Errorf("expected backtick-wrapped table path:\n%s", sql)
	}
	if strings.Contains(sql, `"orders"`) {
		t.Errorf("BigQuery output must not contain double-quoted identifiers:\n%s", sql)
	}
}

// TestDialectDivergence compiles the same plan for both dialects and asserts
// the outputs differ where the syntax differs (identifier quoting at minimum).
func TestDialectDivergence(t *testing.T) {
	bqDoc, err := model.ParseFile(filepath.Join(modelDir, "ecommerce_bigquery.yaml"))
	if err != nil {
		t.Fatalf("parse bigquery model: %v", err)
	}
	pgDoc, err := model.ParseFile(filepath.Join(modelDir, "ecommerce_postgres.yaml"))
	if err != nil {
		t.Fatalf("parse postgres model: %v", err)
	}
	q := &query.Query{
		Metrics:    []string{"orders.order_count"},
		Dimensions: []string{"customers.region"},
	}

	bqPlan, err := planner.Plan(q, &bqDoc.Models[0])
	if err != nil {
		t.Fatalf("plan bigquery: %v", err)
	}
	pgPlan, err := planner.Plan(q, &pgDoc.Models[0])
	if err != nil {
		t.Fatalf("plan postgres: %v", err)
	}

	bqSQL, err := codegen.Compile(planner.PushDown(bqPlan), "bigquery")
	if err != nil {
		t.Fatalf("compile bigquery: %v", err)
	}
	pgSQL, err := codegen.Compile(planner.PushDown(pgPlan), "postgres")
	if err != nil {
		t.Fatalf("compile postgres: %v", err)
	}

	if bqSQL == pgSQL {
		t.Errorf("bigquery and postgres output should differ in quoting\nbq:\n%s\npg:\n%s", bqSQL, pgSQL)
	}
	if !strings.Contains(bqSQL, "`") {
		t.Errorf("bigquery output should contain backticks:\n%s", bqSQL)
	}
	if !strings.Contains(pgSQL, `"`) {
		t.Errorf("postgres output should contain double quotes:\n%s", pgSQL)
	}
}
