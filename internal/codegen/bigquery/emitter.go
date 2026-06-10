// Package bigquery implements the BigQuery SQL emitter for gavagai.
//
// Importing this package registers the "bigquery" dialect with
// codegen.Compile via the package-level init function:
//
//	import _ "github.com/vincentk1991/gavagai/internal/codegen/bigquery"
//
// The SQL clause structure is shared across dialects in codegen.EmitSelect;
// this package supplies only the BigQuery-specific quoting and dialect tag.
// BigQuery differs from PostgreSQL in three ways: backtick identifier quoting,
// whole-path backtick-quoted table references (`project.dataset.table`), and
// the BIGQUERY OSI expression tag. Functions whose argument order differs
// (e.g. DATE_TRUNC) are handled in the semantic model's per-dialect expression
// entries, not here.
package bigquery

import (
	"strings"

	"github.com/vincentk1991/gavagai/internal/codegen"
	"github.com/vincentk1991/gavagai/internal/planner"
)

func init() {
	codegen.Register(&BigQueryDialect{})
}

// BigQueryDialect implements codegen.Dialect for Google BigQuery.
type BigQueryDialect struct{}

// New returns a new BigQueryDialect. The dialect is also registered globally
// by init; use New only when you need a local instance (e.g. in tests).
func New() *BigQueryDialect { return &BigQueryDialect{} }

func (d *BigQueryDialect) Name() string { return "bigquery" }

// EmitSQL renders a plan tree as BigQuery Standard SQL.
func (d *BigQueryDialect) EmitSQL(root planner.PlanNode) (string, error) {
	return codegen.EmitSelect(root, renderer{})
}

// renderer supplies BigQuery-specific quoting and the dialect expression tag.
type renderer struct{}

// DialectTag selects BIGQUERY-tagged OSI expressions, falling back to ANSI_SQL.
func (renderer) DialectTag() string { return "BIGQUERY" }

// QuoteIdent wraps an identifier in backticks. BigQuery identifiers cannot
// themselves contain backticks, so no escaping is required.
func (renderer) QuoteIdent(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "") + "`"
}

// QuoteTable wraps a fully-qualified table path (project.dataset.table) in a
// single pair of backticks, the BigQuery convention.
func (renderer) QuoteTable(source string) string {
	return "`" + strings.ReplaceAll(source, "`", "") + "`"
}

// NullSafeEq expands the null-safe equality: BigQuery has no
// IS NOT DISTINCT FROM operator, so the general form is emitted.
func (renderer) NullSafeEq(expr, lit string) string {
	return "(" + expr + " = " + lit + " OR (" + expr + " IS NULL AND " + lit + " IS NULL))"
}
