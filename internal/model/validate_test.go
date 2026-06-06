package model_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/model"
)

// validModel returns a structurally valid in-memory model that individual
// test cases mutate to trigger a single validation failure.
func validModel() *model.SemanticModel {
	ansi := func(expr string) model.Expression {
		return model.Expression{Dialects: []model.DialectExpression{
			{Dialect: "ANSI_SQL", Expression: expr},
		}}
	}
	return &model.SemanticModel{
		Name: "m",
		Datasets: []model.Dataset{
			{
				Name:   "orders",
				Source: "analytics.public.orders",
				Fields: []model.Field{
					{Name: "region", Expression: ansi("region")},
				},
			},
		},
		Metrics: []model.Metric{
			{Name: "revenue", Expression: ansi("SUM(orders.amount)")},
		},
		Relationships: []model.Relationship{
			{
				Name: "r", From: "orders", To: "orders",
				FromColumns: []string{"a"}, ToColumns: []string{"b"},
			},
		},
	}
}

func TestValidateValidModel(t *testing.T) {
	if errs := model.Validate(validModel()); len(errs) != 0 {
		t.Fatalf("valid model: want no errors, got %v", errs)
	}
}

func TestValidateParsedSimpleModel(t *testing.T) {
	doc, err := model.ParseFile(filepath.Join("testdata", "simple.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if errs := model.Validate(&doc.Models[0]); len(errs) != 0 {
		t.Fatalf("parsed simple model: want no errors, got %v", errs)
	}
}

// TestValidateRules drives one validation rule per case by mutating an
// otherwise-valid model and asserting the resulting error mentions the field.
func TestValidateRules(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(m *model.SemanticModel)
		wantSub string // substring expected somewhere in the joined errors
	}{
		{
			name:    "missing model name",
			mutate:  func(m *model.SemanticModel) { m.Name = "" },
			wantSub: "name",
		},
		{
			name:    "no datasets",
			mutate:  func(m *model.SemanticModel) { m.Datasets = nil },
			wantSub: "dataset",
		},
		{
			name:    "dataset missing name",
			mutate:  func(m *model.SemanticModel) { m.Datasets[0].Name = "" },
			wantSub: "name",
		},
		{
			name:    "dataset missing source",
			mutate:  func(m *model.SemanticModel) { m.Datasets[0].Source = "" },
			wantSub: "source",
		},
		{
			name: "duplicate dataset name",
			mutate: func(m *model.SemanticModel) {
				dup := m.Datasets[0]
				m.Datasets = append(m.Datasets, dup)
			},
			wantSub: "duplicate",
		},
		{
			name:    "field missing name",
			mutate:  func(m *model.SemanticModel) { m.Datasets[0].Fields[0].Name = "" },
			wantSub: "name",
		},
		{
			name: "field missing expression",
			mutate: func(m *model.SemanticModel) {
				m.Datasets[0].Fields[0].Expression.Dialects = nil
			},
			wantSub: "expression",
		},
		{
			name:    "metric missing name",
			mutate:  func(m *model.SemanticModel) { m.Metrics[0].Name = "" },
			wantSub: "name",
		},
		{
			name: "metric missing expression",
			mutate: func(m *model.SemanticModel) {
				m.Metrics[0].Expression.Dialects = nil
			},
			wantSub: "expression",
		},
		{
			name:    "relationship missing from",
			mutate:  func(m *model.SemanticModel) { m.Relationships[0].From = "" },
			wantSub: "from",
		},
		{
			name: "relationship missing from_columns",
			mutate: func(m *model.SemanticModel) {
				m.Relationships[0].FromColumns = nil
			},
			wantSub: "from_columns",
		},
		{
			name: "relationship references unknown dataset",
			mutate: func(m *model.SemanticModel) {
				m.Relationships[0].To = "nonexistent"
			},
			wantSub: "nonexistent",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validModel()
			tc.mutate(m)

			errs := model.Validate(m)
			if len(errs) == 0 {
				t.Fatalf("want at least one validation error, got none")
			}

			var joined strings.Builder
			for _, e := range errs {
				joined.WriteString(e.Error())
				joined.WriteString("\n")
			}
			if !strings.Contains(joined.String(), tc.wantSub) {
				t.Errorf("errors do not mention %q:\n%s", tc.wantSub, joined.String())
			}
		})
	}
}
