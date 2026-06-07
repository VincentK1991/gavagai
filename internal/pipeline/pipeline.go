// Package pipeline wires the gavagai stages — parse, validate, plan, push
// down, emit — into the two operations the CLI exposes: compiling a query to
// SQL and validating a semantic model. Keeping this orchestration out of the
// cmd package lets it be unit-tested without a Cobra command tree.
//
// Importing this package registers every supported SQL dialect (the blank
// imports below), so callers do not need to import the dialect packages
// themselves.
package pipeline

import (
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/codegen"
	_ "github.com/vincentk1991/gavagai/internal/codegen/bigquery" // register bigquery dialect
	_ "github.com/vincentk1991/gavagai/internal/codegen/postgres" // register postgres dialect
	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// Options configures a Compile run.
type Options struct {
	ModelPath string
	QueryPath string
	Dialect   string
}

// Result is the output of a successful Compile.
type Result struct {
	// SQL is the emitted query for the requested dialect, multi-line formatted.
	SQL string
	// Plan is the relational-algebra plan shape (planner.Describe form), for
	// the CLI's --explain flag.
	Plan string
}

// Compile runs the full model+query → SQL pipeline. Errors from any stage are
// returned with enough context to act on: model/query validation failures list
// every problem, and a fan-out error carries the planner's detailed message.
func Compile(opts Options) (*Result, error) {
	m, err := loadModel(opts.ModelPath)
	if err != nil {
		return nil, err
	}

	q, err := query.ParseFile(opts.QueryPath)
	if err != nil {
		return nil, fmt.Errorf("parse query %q: %w", opts.QueryPath, err)
	}
	if verrs := query.Validate(q, m); len(verrs) > 0 {
		return nil, fmt.Errorf("invalid query %q:\n%s", opts.QueryPath, joinErrors(verrs))
	}

	plan, err := planner.Plan(q, m)
	if err != nil {
		return nil, err // FanOutError and join errors are already descriptive
	}
	plan = planner.PushDown(plan)

	sql, err := codegen.Compile(plan, opts.Dialect)
	if err != nil {
		return nil, err
	}

	return &Result{SQL: sql, Plan: planner.Describe(plan)}, nil
}

// LoadAndValidateModel parses and structurally validates a semantic model,
// returning the model on success or an aggregated error listing every problem.
func LoadAndValidateModel(path string) (*model.SemanticModel, error) {
	return loadModel(path)
}

// loadModel parses a model file, selects the first semantic model, and runs
// structural validation.
func loadModel(path string) (*model.SemanticModel, error) {
	doc, err := model.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse model %q: %w", path, err)
	}
	if len(doc.Models) == 0 {
		return nil, fmt.Errorf("model file %q contains no semantic_model entries", path)
	}
	m := &doc.Models[0]
	if verrs := model.Validate(m); len(verrs) > 0 {
		return nil, fmt.Errorf("invalid model %q:\n%s", path, joinErrors(verrs))
	}
	return m, nil
}

// joinErrors renders a slice of validation errors as an indented bullet list.
func joinErrors[E error](errs []E) string {
	var b strings.Builder
	for i, e := range errs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("  - " + e.Error())
	}
	return b.String()
}
