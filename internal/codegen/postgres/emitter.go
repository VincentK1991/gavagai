// Package postgres implements the PostgreSQL SQL emitter for gavagai.
//
// Importing this package registers the "postgres" dialect with
// codegen.Compile via the package-level init function, so the CLI and tests
// only need a blank import to activate it:
//
//	import _ "github.com/vincentk1991/gavagai/internal/codegen/postgres"
package postgres

import (
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/codegen"
	"github.com/vincentk1991/gavagai/internal/planner"
)

// dialectTag is the OSI expression tag used when selecting PostgreSQL-specific
// SQL fragments. The SelectExpression fallback chain tries POSTGRES first,
// then ANSI_SQL.
const dialectTag = "POSTGRES"

func init() {
	codegen.Register(&PostgresDialect{})
}

// PostgresDialect implements codegen.Dialect for PostgreSQL.
type PostgresDialect struct{}

// New returns a new PostgresDialect. The dialect is also registered globally
// by init; use New only when you need a local instance (e.g. in tests).
func New() *PostgresDialect { return &PostgresDialect{} }

func (d *PostgresDialect) Name() string { return "postgres" }

// EmitSQL walks root and returns a syntactically correct PostgreSQL SELECT
// statement. The plan must have passed through planner.PushDown so that
// FilterNodes sit directly above ScanNodes rather than above JoinNodes.
func (d *PostgresDialect) EmitSQL(root planner.PlanNode) (string, error) {
	b := &builder{}
	if err := b.build(root); err != nil {
		return "", err
	}
	return b.render(), nil
}

// ---- builder ---------------------------------------------------------------

type joinClause struct {
	kind  string // "LEFT", "INNER", …
	table string // e.g. `analytics.customers AS "customers"`
	on    string // e.g. `"orders"."customer_id" = "customers"."customer_id"`
}

type builder struct {
	distinct bool
	selects  []string
	from     string
	joins    []joinClause
	where    []string
	groupBy  []string
	having   []string
	orderBy  []string
	limit    *int
}

// build is the recursive walker. It collects every SQL clause into the builder
// fields in a single top-down pass; render() assembles them into final SQL.
func (b *builder) build(n planner.PlanNode) error {
	switch t := n.(type) {

	case *planner.LimitNode:
		b.limit = &t.Count
		return b.build(t.Input)

	case *planner.OrderNode:
		for _, item := range t.Items {
			_, name, _ := splitRef(item.Ref)
			b.orderBy = append(b.orderBy, quoteIdent(name)+" "+item.Direction)
		}
		return b.build(t.Input)

	case *planner.HavingNode:
		for _, h := range t.Predicates {
			expr, err := codegen.SelectExpression(h.Metric.Expression, dialectTag)
			if err != nil {
				return fmt.Errorf("postgres: having metric %q: %w", h.Metric.Name, err)
			}
			b.having = append(b.having, fmt.Sprintf("%s %s %s", expr, h.Op, formatFloat(h.Value)))
		}
		return b.build(t.Input)

	case *planner.AggregateNode:
		for _, dim := range t.GroupBy {
			expr, err := codegen.SelectExpression(dim.Field.Expression, dialectTag)
			if err != nil {
				return fmt.Errorf("postgres: dimension %q: %w", dim.Field.Name, err)
			}
			b.selects = append(b.selects, expr+" AS "+quoteIdent(dim.Field.Name))
			b.groupBy = append(b.groupBy, expr)
		}
		for _, met := range t.Aggregates {
			expr, err := codegen.SelectExpression(met.Metric.Expression, dialectTag)
			if err != nil {
				return fmt.Errorf("postgres: metric %q: %w", met.Metric.Name, err)
			}
			b.selects = append(b.selects, expr+" AS "+quoteIdent(met.Metric.Name))
		}
		// A measure-less aggregate is a SELECT DISTINCT (deduplication query).
		if len(t.Aggregates) == 0 {
			b.distinct = true
			b.groupBy = nil // DISTINCT replaces GROUP BY
		}
		return b.build(t.Input)

	case *planner.FilterNode:
		// Filters appear both in the "vertical" clause stack (above Aggregate,
		// pre-pushdown) and at the scan level (post-PushDown). Both become WHERE.
		for _, pred := range t.Predicates {
			expr, err := codegen.SelectExpression(pred.Field.Expression, dialectTag)
			if err != nil {
				return fmt.Errorf("postgres: filter field %q: %w", pred.Field.Name, err)
			}
			clause, err := renderPredicate(expr, pred.Op, pred.Value)
			if err != nil {
				return err
			}
			b.where = append(b.where, clause)
		}
		return b.build(t.Input)

	case *planner.JoinNode:
		// The left subtree builds the FROM clause (and any preceding JOINs for
		// multi-hop chains). The right side always resolves to a single Scan,
		// possibly wrapped in a FilterNode from PushDown.
		if err := b.build(t.Left); err != nil {
			return err
		}
		scan, preds, err := extractScan(t.Right)
		if err != nil {
			return err
		}
		// Pushed-down predicates for the right dataset still go into WHERE.
		for _, pred := range preds {
			expr, err := codegen.SelectExpression(pred.Field.Expression, dialectTag)
			if err != nil {
				return fmt.Errorf("postgres: filter field %q: %w", pred.Field.Name, err)
			}
			clause, err := renderPredicate(expr, pred.Op, pred.Value)
			if err != nil {
				return err
			}
			b.where = append(b.where, clause)
		}
		// Build the ON condition from the OSI relationship's column lists.
		ons := make([]string, len(t.On))
		for i, c := range t.On {
			ons[i] = quoteIdent(c.Left.Dataset) + "." + quoteIdent(c.Left.Column) +
				" = " +
				quoteIdent(c.Right.Dataset) + "." + quoteIdent(c.Right.Column)
		}
		b.joins = append(b.joins, joinClause{
			kind:  string(t.Kind),
			table: scan.Dataset.Source + " AS " + quoteIdent(scan.Alias),
			on:    strings.Join(ons, " AND "),
		})
		return nil

	case *planner.ScanNode:
		b.from = t.Dataset.Source + " AS " + quoteIdent(t.Alias)
		return nil

	default:
		return fmt.Errorf("postgres: unsupported plan node %T", n)
	}
}

// extractScan walks through any stack of FilterNodes wrapping n, collecting
// their predicates, until it reaches the underlying ScanNode. The right side
// of every JoinNode produced by the planner is always a ScanNode (possibly
// wrapped in one FilterNode by PushDown).
func extractScan(n planner.PlanNode) (*planner.ScanNode, []planner.Predicate, error) {
	var preds []planner.Predicate
	for {
		switch t := n.(type) {
		case *planner.ScanNode:
			return t, preds, nil
		case *planner.FilterNode:
			preds = append(preds, t.Predicates...)
			n = t.Input
		default:
			return nil, nil, fmt.Errorf(
				"postgres: expected ScanNode on join right side, got %T", n)
		}
	}
}

// render assembles the collected clauses into a single PostgreSQL SELECT
// statement. Clauses absent from the query are omitted.
func (b *builder) render() string {
	var sb strings.Builder

	// SELECT
	if b.distinct {
		sb.WriteString("SELECT DISTINCT\n")
	} else {
		sb.WriteString("SELECT\n")
	}
	for i, sel := range b.selects {
		if i > 0 {
			sb.WriteString(",\n")
		}
		sb.WriteString("  " + sel)
	}

	// FROM
	sb.WriteString("\nFROM " + b.from)

	// JOIN(s)
	for _, j := range b.joins {
		sb.WriteString("\n" + j.kind + " JOIN " + j.table + "\n  ON " + j.on)
	}

	// WHERE
	if len(b.where) > 0 {
		sb.WriteString("\nWHERE " + b.where[0])
		for _, w := range b.where[1:] {
			sb.WriteString("\n  AND " + w)
		}
	}

	// GROUP BY
	if len(b.groupBy) > 0 {
		sb.WriteString("\nGROUP BY " + strings.Join(b.groupBy, ", "))
	}

	// HAVING
	if len(b.having) > 0 {
		sb.WriteString("\nHAVING " + b.having[0])
		for _, h := range b.having[1:] {
			sb.WriteString("\n  AND " + h)
		}
	}

	// ORDER BY
	if len(b.orderBy) > 0 {
		sb.WriteString("\nORDER BY " + strings.Join(b.orderBy, ", "))
	}

	// LIMIT
	if b.limit != nil {
		sb.WriteString(fmt.Sprintf("\nLIMIT %d", *b.limit))
	}

	sb.WriteString("\n")
	return sb.String()
}

// splitRef splits a "dataset.name" reference. Identical to planner.splitRef
// but local to avoid a cross-package dependency on an unexported helper.
func splitRef(ref string) (dataset, name string, ok bool) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
