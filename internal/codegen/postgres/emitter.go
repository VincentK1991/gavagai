// Package postgres implements the PostgreSQL SQL emitter for gavagai.
//
// Importing this package registers the "postgres" dialect with
// codegen.Compile via the package-level init function, so the CLI and tests
// only need a blank import to activate it:
//
//	import _ "github.com/vincentk1991/gavagai/internal/codegen/postgres"
//
// The SQL clause structure is shared across dialects in codegen.EmitSelect;
// this package supplies only the PostgreSQL-specific quoting and dialect tag
// via the renderer type.
package postgres

import (
	"strings"

	"github.com/vincentk1991/gavagai/internal/codegen"
	"github.com/vincentk1991/gavagai/internal/planner"
)

func init() {
	codegen.Register(&PostgresDialect{})
}

// PostgresDialect implements codegen.Dialect for PostgreSQL.
type PostgresDialect struct{}

// New returns a new PostgresDialect. The dialect is also registered globally
// by init; use New only when you need a local instance (e.g. in tests).
func New() *PostgresDialect { return &PostgresDialect{} }

func (d *PostgresDialect) Name() string { return "postgres" }

// EmitSQL renders a plan tree as PostgreSQL SQL.
func (d *PostgresDialect) EmitSQL(root planner.PlanNode) (string, error) {
	return codegen.EmitSelect(root, renderer{})
}

// renderer supplies PostgreSQL-specific quoting and the dialect expression tag.
type renderer struct{}

// DialectTag selects POSTGRES-tagged OSI expressions, falling back to ANSI_SQL.
func (renderer) DialectTag() string { return "POSTGRES" }

// QuoteIdent wraps an identifier in double quotes, doubling embedded quotes.
func (renderer) QuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// QuoteTable emits a PostgreSQL schema-qualified table path unquoted, e.g.
// analytics.orders.
func (renderer) QuoteTable(source string) string { return source }

// NullSafeEq uses PostgreSQL's native IS NOT DISTINCT FROM operator.
func (renderer) NullSafeEq(expr, lit string) string {
	return expr + " IS NOT DISTINCT FROM " + lit
}
