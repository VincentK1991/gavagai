package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/codegen"
	_ "github.com/vincentk1991/gavagai/internal/codegen/bigquery"
	_ "github.com/vincentk1991/gavagai/internal/codegen/postgres"
	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

const (
	modelsDir  = "testdata/models"
	queriesDir = "testdata/queries"
)

func loadModel(t *testing.T, name string) *model.SemanticModel {
	t.Helper()
	path := filepath.Join(modelsDir, name)
	doc, err := model.ParseFile(path)
	if err != nil {
		t.Fatalf("loadModel %s: %v", name, err)
	}
	if len(doc.Models) == 0 {
		t.Fatalf("loadModel %s: no models in document", name)
	}
	return &doc.Models[0]
}

func loadQuery(t *testing.T, name string) *query.Query {
	t.Helper()
	path := filepath.Join(queriesDir, name)
	q, err := query.ParseFile(path)
	if err != nil {
		t.Fatalf("loadQuery %s: %v", name, err)
	}
	return q
}

func loadQueryRaw(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(queriesDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadQueryRaw %s: %v", name, err)
	}
	return data
}

func compileSQL(t *testing.T, modelFile, queryFile string) string {
	t.Helper()
	return compileSQLDialect(t, modelFile, queryFile, "postgres")
}

func compileSQLDialect(t *testing.T, modelFile, queryFile, dialect string) string {
	t.Helper()
	m := loadModel(t, modelFile)
	q := loadQuery(t, queryFile)
	return compileSQLFrom(t, m, q, dialect)
}

func compileSQLFrom(t *testing.T, m *model.SemanticModel, q *query.Query, dialect string) string {
	t.Helper()
	plan, err := planner.Plan(q, m)
	if err != nil {
		t.Fatalf("planner.Plan: %v", err)
	}
	plan = planner.PushDown(plan)
	sql, err := codegen.Compile(plan, dialect)
	if err != nil {
		t.Fatalf("codegen.Compile(%s): %v", dialect, err)
	}
	return sql
}

// planFromResult plans without fataling, returning the error to the caller.
func planFromResult(m *model.SemanticModel, q *query.Query) (planner.PlanNode, error) {
	plan, err := planner.Plan(q, m)
	if err != nil {
		return nil, err
	}
	return planner.PushDown(plan), nil
}

// parseQueryJSON parses a JSON string into a Query without fataling.
func parseQueryJSON(data string) (*query.Query, error) {
	return query.Parse([]byte(data))
}

func assertContains(t *testing.T, sql, substr string) {
	t.Helper()
	if !strings.Contains(sql, substr) {
		t.Errorf("expected SQL to contain %q\ngot:\n%s", substr, sql)
	}
}

func assertNotContains(t *testing.T, sql, substr string) {
	t.Helper()
	if strings.Contains(sql, substr) {
		t.Errorf("expected SQL NOT to contain %q\ngot:\n%s", substr, sql)
	}
}

func pendingTest(t *testing.T, section, id, reason string) {
	t.Helper()
	t.Skipf("PENDING [§%s/%s]: %s", section, id, reason)
}
