// Package codegen renders a finished plan tree into SQL for a target dialect.
//
// The package is the seam between the dialect-independent planner and concrete
// SQL text. Per-dialect emitters register themselves via Register (typically
// from an init function); Compile dispatches to the registered emitter.
// SelectExpression is a shared helper used by every emitter to resolve the
// correct SQL fragment for a field or metric expression.
package codegen

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
)

// ErrNotImplemented is returned by Compile for a recognised dialect whose SQL
// emitter has not been registered yet. The conformance suite treats it as a
// "pending" signal and skips the corresponding checklist gate.
var ErrNotImplemented = errors.New("codegen: not implemented")

// Dialect is the SQL-emission contract implemented once per target dialect.
type Dialect interface {
	// Name returns the dialect identifier, e.g. "postgres" or "bigquery".
	Name() string
	// EmitSQL walks a plan tree and returns the SQL text for this dialect.
	EmitSQL(root planner.PlanNode) (string, error)
}

// SupportedDialects lists the dialect names Compile recognises. A dialect may
// be "supported" (listed here) but not yet registered if its emitter has not
// been linked into the binary. In that case Compile returns ErrNotImplemented.
var SupportedDialects = []string{"postgres", "bigquery"}

// registry holds the per-dialect emitters added by Register.
// Populated at init time from each dialect package; safe to read after init.
var registry = map[string]Dialect{}

// Register adds a dialect emitter to the dispatch table. Call it from an
// init() function in each dialect package so that importing the package is
// sufficient to enable the dialect.
func Register(d Dialect) {
	registry[strings.ToLower(d.Name())] = d
}

// Compile renders a plan tree to SQL for the named dialect.
// It dispatches to the registered Dialect implementation. If the dialect name
// is recognised but no emitter has been registered (its package was not
// imported), Compile returns ErrNotImplemented.
func Compile(root planner.PlanNode, dialect string) (string, error) {
	key := strings.ToLower(dialect)
	if d, ok := registry[key]; ok {
		return d.EmitSQL(root)
	}
	for _, known := range SupportedDialects {
		if strings.EqualFold(known, dialect) {
			return "", ErrNotImplemented
		}
	}
	return "", fmt.Errorf("codegen: unsupported dialect %q (supported: %s)",
		dialect, strings.Join(SupportedDialects, ", "))
}

// SelectExpression returns the SQL fragment for the requested dialect using the
// selection rule shared by every emitter:
//
//  1. exact match on the dialect tag (case-insensitive);
//  2. fallback to the ANSI_SQL entry;
//  3. error naming the field if neither is present.
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
