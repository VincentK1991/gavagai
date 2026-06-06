// Package codegen renders a finished plan tree into SQL for a target dialect.
//
// The package is the seam between the dialect-independent planner and concrete
// SQL text. Per-dialect emitters (PostgreSQL in phase 5, BigQuery in phase 6)
// implement the Dialect interface; Compile dispatches to them. Until an emitter
// lands, Compile returns ErrNotImplemented for recognised dialects so callers
// (and the conformance suite) can distinguish "pending" from "unsupported".
//
// SelectExpression is already implemented: it is the dialect-expression
// selection rule shared by every emitter (exact match, then ANSI_SQL fallback,
// then error) and is exercised directly by the conformance gates.
package codegen

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
)

// ErrNotImplemented is returned by Compile for a recognised dialect whose SQL
// emitter has not been written yet. The conformance suite treats it as a
// "pending" signal and skips the corresponding checklist gate.
var ErrNotImplemented = errors.New("codegen: not implemented")

// Dialect is the SQL-emission contract implemented once per target dialect.
type Dialect interface {
	// Name is the dialect identifier, e.g. "postgres" or "bigquery".
	Name() string
	// EmitSQL walks a plan tree and returns the SQL text for this dialect.
	EmitSQL(root planner.PlanNode) (string, error)
}

// SupportedDialects lists the dialect names Compile recognises.
var SupportedDialects = []string{"postgres", "bigquery"}

// Compile renders a plan tree to SQL for the named dialect.
//
// It currently returns ErrNotImplemented for recognised dialects (the emitters
// arrive in phases 5–6) and a descriptive error for unknown dialect names. The
// dialect dispatch itself is live and tested.
func Compile(root planner.PlanNode, dialect string) (string, error) {
	switch strings.ToLower(dialect) {
	case "postgres", "bigquery":
		return "", ErrNotImplemented
	default:
		return "", fmt.Errorf("codegen: unsupported dialect %q (want one of %v)", dialect, SupportedDialects)
	}
}

// SelectExpression returns the SQL fragment for the requested dialect using the
// selection rule shared by every emitter:
//
//  1. exact match on the dialect tag (case-insensitive, e.g. "postgres"
//     matches an OSI "POSTGRES" entry);
//  2. fall back to the ANSI_SQL entry;
//  3. otherwise return an error naming the dialect.
//
// It is a pure function so emitters and the conformance suite can share it.
func SelectExpression(e model.Expression, dialect string) (string, error) {
	for _, d := range e.Dialects {
		if strings.EqualFold(d.Dialect, dialect) {
			return d.Expression, nil
		}
	}
	for _, d := range e.Dialects {
		if strings.EqualFold(d.Dialect, "ANSI_SQL") {
			return d.Expression, nil
		}
	}
	return "", fmt.Errorf("codegen: no expression for dialect %q and no ANSI_SQL fallback", dialect)
}
