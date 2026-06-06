package model_test

import (
	"path/filepath"
	"testing"

	"github.com/vincentk1991/gavagai/internal/model"
)

// firstModel parses a fixture, asserts exactly one model is present, and
// returns it. Any parse error or unexpected model count fails the test.
func firstModel(t *testing.T, fixture string) *model.SemanticModel {
	t.Helper()

	doc, err := model.ParseFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("ParseFile(%s) returned error: %v", fixture, err)
	}
	if got := len(doc.Models); got != 1 {
		t.Fatalf("ParseFile(%s): want 1 model, got %d", fixture, got)
	}
	return &doc.Models[0]
}

func TestParseSimpleYAML(t *testing.T) {
	doc, err := model.ParseFile(filepath.Join("testdata", "simple.yaml"))
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	if doc.Version != "0.2.0.dev0" {
		t.Errorf("version: want 0.2.0.dev0, got %q", doc.Version)
	}
	if len(doc.Models) != 1 {
		t.Fatalf("want 1 model, got %d", len(doc.Models))
	}

	m := doc.Models[0]
	if m.Name != "simple_sales" {
		t.Errorf("model name: want simple_sales, got %q", m.Name)
	}

	// Dataset and field.
	if len(m.Datasets) != 1 {
		t.Fatalf("want 1 dataset, got %d", len(m.Datasets))
	}
	ds := m.Datasets[0]
	if ds.Name != "orders" || ds.Source != "analytics.public.orders" {
		t.Errorf("dataset: got name=%q source=%q", ds.Name, ds.Source)
	}
	if len(ds.PrimaryKey) != 1 || ds.PrimaryKey[0] != "order_id" {
		t.Errorf("primary_key: got %v", ds.PrimaryKey)
	}
	if len(ds.Fields) != 1 {
		t.Fatalf("want 1 field, got %d", len(ds.Fields))
	}
	f := ds.Fields[0]
	if f.Name != "region" {
		t.Errorf("field name: want region, got %q", f.Name)
	}
	if len(f.Expression.Dialects) != 1 {
		t.Fatalf("field expression: want 1 dialect, got %d", len(f.Expression.Dialects))
	}
	if de := f.Expression.Dialects[0]; de.Dialect != "ANSI_SQL" || de.Expression != "region" {
		t.Errorf("dialect expression: got dialect=%q expr=%q", de.Dialect, de.Expression)
	}
	if f.Dimension == nil {
		t.Fatalf("field dimension: want non-nil")
	}
	if f.Dimension.IsTime {
		t.Errorf("field dimension is_time: want false")
	}

	// ai_context in both string and object form.
	if f.AIContext == nil || f.AIContext.Text != "Geographic sales region." {
		t.Errorf("field ai_context (string form): got %+v", f.AIContext)
	}
	if m.AIContext == nil || m.AIContext.Instructions != "Use this model for revenue-by-region questions." {
		t.Errorf("model ai_context (object form): got %+v", m.AIContext)
	}

	// Metric.
	if len(m.Metrics) != 1 {
		t.Fatalf("want 1 metric, got %d", len(m.Metrics))
	}
	if m.Metrics[0].Name != "revenue" {
		t.Errorf("metric name: want revenue, got %q", m.Metrics[0].Name)
	}
}

func TestParseSimpleJSON(t *testing.T) {
	m := firstModel(t, "simple.json")

	if m.Name != "simple_sales" {
		t.Errorf("model name: want simple_sales, got %q", m.Name)
	}
	if len(m.Datasets) != 1 || m.Datasets[0].Name != "orders" {
		t.Errorf("datasets: got %+v", m.Datasets)
	}
	if len(m.Datasets[0].Fields) != 1 || m.Datasets[0].Fields[0].Name != "region" {
		t.Errorf("fields: got %+v", m.Datasets[0].Fields)
	}
	if len(m.Metrics) != 1 || m.Metrics[0].Name != "revenue" {
		t.Errorf("metrics: got %+v", m.Metrics)
	}
}

func TestParseJoinedRelationship(t *testing.T) {
	m := firstModel(t, "joined.yaml")

	if len(m.Datasets) != 2 {
		t.Fatalf("want 2 datasets, got %d", len(m.Datasets))
	}
	if len(m.Relationships) != 1 {
		t.Fatalf("want 1 relationship, got %d", len(m.Relationships))
	}
	r := m.Relationships[0]
	if r.Name != "orders_to_customers" {
		t.Errorf("relationship name: got %q", r.Name)
	}
	if r.From != "orders" || r.To != "customers" {
		t.Errorf("relationship from/to: got from=%q to=%q", r.From, r.To)
	}
	if len(r.FromColumns) != 1 || r.FromColumns[0] != "customer_id" {
		t.Errorf("from_columns: got %v", r.FromColumns)
	}
	if len(r.ToColumns) != 1 || r.ToColumns[0] != "id" {
		t.Errorf("to_columns: got %v", r.ToColumns)
	}
}

func TestParseMissingFile(t *testing.T) {
	_, err := model.ParseFile(filepath.Join("testdata", "does_not_exist.yaml"))
	if err == nil {
		t.Fatal("ParseFile on missing path: want error, got nil")
	}
}

func TestParseUnsupportedExtension(t *testing.T) {
	_, err := model.ParseFile(filepath.Join("testdata", "simple.txt"))
	if err == nil {
		t.Fatal("ParseFile on .txt: want error, got nil")
	}
}

func TestParseMalformedYAML(t *testing.T) {
	_, err := model.Parse([]byte("semantic_model: [this is : not valid"), "bad.yaml")
	if err == nil {
		t.Fatal("Parse on malformed YAML: want error, got nil")
	}
}
