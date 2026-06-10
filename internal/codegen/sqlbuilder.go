package codegen

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/vincentk1991/gavagai/internal/planner"
)

// Renderer supplies the dialect-specific pieces of SQL generation. The clause
// structure (SELECT/FROM/JOIN/WHERE/GROUP BY/HAVING/ORDER BY/LIMIT), value
// literal rendering, and predicate shapes are identical across the SQL dialects
// gavagai targets, so EmitSelect implements them once and defers only quoting,
// table-path formatting, and dialect-tag selection to the Renderer.
type Renderer interface {
	// DialectTag is the OSI expression tag tried first by SelectExpression
	// before the ANSI_SQL fallback, e.g. "POSTGRES" or "BIGQUERY".
	DialectTag() string
	// QuoteIdent quotes a single identifier (column or alias), e.g.
	//   postgres: name -> "name"
	//   bigquery: name -> `name`
	QuoteIdent(ident string) string
	// QuoteTable formats a dataset's physical source path, e.g.
	//   postgres: analytics.orders -> analytics.orders
	//   bigquery: my_project.analytics.orders -> `my_project.analytics.orders`
	QuoteTable(source string) string
	// NullSafeEq renders the null-safe equality `expr IS NOT DISTINCT FROM lit`
	// in the dialect's form: PostgreSQL supports the operator natively, while
	// BigQuery needs the expanded `(expr = lit OR (expr IS NULL AND lit IS NULL))`.
	NullSafeEq(expr, lit string) string
}

// EmitSelect renders a plan tree into a SELECT statement using r for the
// dialect-specific quoting and tagging. The plan must have passed through
// planner.PushDown so that FilterNodes sit directly above ScanNodes.
func EmitSelect(root planner.PlanNode, r Renderer) (string, error) {
	b := &builder{r: r}
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
	r        Renderer
	distinct bool
	// star, when true, makes render() emit `SELECT *` if no explicit select
	// items were collected. It is set for subquery / CTE bodies, which project
	// every column of their single base scan.
	star    bool
	selects []string
	from    string
	joins   []joinClause
	where   []string
	groupBy []string
	having  []string
	orderBy []string
	limit   *int
	offset  int
	// ctes holds rendered CTE definitions from a WithNode, prepended as a WITH
	// clause by render().
	ctes []renderedCTE
}

// renderedCTE is a CTE definition already rendered to SQL: `name AS (body)`.
type renderedCTE struct {
	name string // already-quoted identifier
	body string // the inner SELECT, not yet indented
}

// build is the recursive walker. It collects every SQL clause into the builder
// fields in a single top-down pass; render() assembles them into final SQL.
func (b *builder) build(n planner.PlanNode) error {
	switch t := n.(type) {

	case *planner.LimitNode:
		if t.HasLimit {
			c := t.Count
			b.limit = &c
		}
		b.offset = t.Offset
		return b.build(t.Input)

	case *planner.OrderNode:
		for _, item := range t.Items {
			_, name, _ := splitRef(item.Ref)
			clause := b.r.QuoteIdent(name) + " " + item.Direction
			if item.Nulls != "" {
				clause += " NULLS " + item.Nulls
			}
			b.orderBy = append(b.orderBy, clause)
		}
		return b.build(t.Input)

	case *planner.HavingNode:
		for _, h := range t.Predicates {
			expr, err := SelectExpression(h.Metric.Expression, b.r.DialectTag())
			if err != nil {
				return fmt.Errorf("codegen: having metric %q: %w", h.Metric.Name, err)
			}
			b.having = append(b.having, fmt.Sprintf("%s %s %s", expr, h.Op, formatFloat(h.Value)))
		}
		return b.build(t.Input)

	case *planner.AggregateNode:
		for _, dim := range t.GroupBy {
			var expr string
			if dim.QualifyColumn != "" {
				// The planner flagged this bare column as ambiguous across joined
				// datasets; pin it to its own dataset.
				expr = b.r.QuoteIdent(dim.Dataset) + "." + b.r.QuoteIdent(dim.QualifyColumn)
			} else {
				e, err := SelectExpression(dim.Field.Expression, b.r.DialectTag())
				if err != nil {
					return fmt.Errorf("codegen: dimension %q: %w", dim.Field.Name, err)
				}
				expr = e
			}
			alias := dim.Field.Name
			if dim.OutAlias != "" {
				alias = dim.OutAlias
			}
			b.selects = append(b.selects, expr+" AS "+b.r.QuoteIdent(alias))
			b.groupBy = append(b.groupBy, expr)
		}
		for _, met := range t.Aggregates {
			expr, err := SelectExpression(met.Metric.Expression, b.r.DialectTag())
			if err != nil {
				return fmt.Errorf("codegen: metric %q: %w", met.Metric.Name, err)
			}
			b.selects = append(b.selects, expr+" AS "+b.r.QuoteIdent(met.Metric.Name))
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
		return b.appendWhere(t.Predicates, t.Input)

	case *planner.JoinNode:
		// The left subtree builds the FROM clause (and any preceding JOINs for
		// multi-hop chains). The right side resolves to a single source: a Scan
		// (possibly wrapped in a FilterNode by PushDown), a SubqueryNode, or a
		// CTERef when materialized.
		if err := b.build(t.Left); err != nil {
			return err
		}
		table, preds, err := b.joinSourceSQL(t.Right)
		if err != nil {
			return err
		}
		// Pushed-down predicates for the right dataset still go into WHERE.
		if err := b.renderPredicatesInto(preds); err != nil {
			return err
		}
		// Build the ON condition from the OSI relationship's column lists.
		ons := make([]string, len(t.On))
		for i, c := range t.On {
			ons[i] = b.r.QuoteIdent(c.Left.Dataset) + "." + b.r.QuoteIdent(c.Left.Column) +
				" = " +
				b.r.QuoteIdent(c.Right.Dataset) + "." + b.r.QuoteIdent(c.Right.Column)
		}
		b.joins = append(b.joins, joinClause{
			kind:  string(t.Kind),
			table: table,
			on:    strings.Join(ons, " AND "),
		})
		return nil

	case *planner.ProjectNode:
		// Select already-aggregated columns by qualified reference, without re-
		// aggregating. Used as the outermost node of a pre-aggregated plan.
		for _, it := range t.Items {
			b.selects = append(b.selects,
				b.r.QuoteIdent(it.Source)+"."+b.r.QuoteIdent(it.Column)+" AS "+b.r.QuoteIdent(it.Alias))
		}
		return b.build(t.Input)

	case *planner.ScanNode:
		b.from = b.r.QuoteTable(t.Dataset.Source) + " AS " + b.r.QuoteIdent(t.Alias)
		return nil

	case *planner.SubqueryNode:
		sub, err := emitSubBlock(t.Input, b.r)
		if err != nil {
			return err
		}
		b.from = "(\n" + indent(sub) + "\n) AS " + b.r.QuoteIdent(t.Alias)
		return nil

	case *planner.CTERef:
		b.from = b.r.QuoteIdent(t.Name) + " AS " + b.r.QuoteIdent(t.Alias)
		return nil

	case *planner.WithNode:
		for _, def := range t.CTEs {
			sub, err := emitSubBlock(def.Query, b.r)
			if err != nil {
				return err
			}
			b.ctes = append(b.ctes, renderedCTE{name: b.r.QuoteIdent(def.Name), body: sub})
		}
		return b.build(t.Body)

	default:
		return fmt.Errorf("codegen: unsupported plan node %T", n)
	}
}

// joinSourceSQL renders a join's right-hand source to its FROM-fragment and
// returns any predicates that must move into the outer WHERE. A ScanNode (flat
// strategy) keeps its pushed-down filter as WHERE; a SubqueryNode or CTERef
// already carries its filter inside the nested block, so it contributes none.
func (b *builder) joinSourceSQL(n planner.PlanNode) (string, []planner.Predicate, error) {
	switch t := n.(type) {
	case *planner.SubqueryNode:
		sub, err := emitSubBlock(t.Input, b.r)
		if err != nil {
			return "", nil, err
		}
		return "(\n" + indent(sub) + "\n) AS " + b.r.QuoteIdent(t.Alias), nil, nil
	case *planner.CTERef:
		return b.r.QuoteIdent(t.Name) + " AS " + b.r.QuoteIdent(t.Alias), nil, nil
	default:
		scan, preds, err := extractScan(n)
		if err != nil {
			return "", nil, err
		}
		return b.r.QuoteTable(scan.Dataset.Source) + " AS " + b.r.QuoteIdent(scan.Alias), preds, nil
	}
}

// emitSubBlock renders an inner plan (a subquery or CTE body) to a standalone
// SELECT using a fresh builder. The body projects every column of its base
// scan, so star is set to emit `SELECT *` when the block carries no aggregate.
func emitSubBlock(n planner.PlanNode, r Renderer) (string, error) {
	sb := &builder{r: r, star: true}
	if err := sb.build(n); err != nil {
		return "", err
	}
	return sb.render(), nil
}

// indent prefixes every non-empty line of s with two spaces, for nesting a
// rendered sub-block inside parentheses or a WITH clause.
func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = "  " + ln
		}
	}
	return strings.Join(lines, "\n")
}

// appendWhere renders preds into the WHERE list then recurses into input.
func (b *builder) appendWhere(preds []planner.Predicate, input planner.PlanNode) error {
	if err := b.renderPredicatesInto(preds); err != nil {
		return err
	}
	return b.build(input)
}

// renderPredicatesInto resolves each predicate and appends the rendered SQL
// condition to the WHERE list.
func (b *builder) renderPredicatesInto(preds []planner.Predicate) error {
	for _, pred := range preds {
		clause, err := b.renderOnePredicate(pred)
		if err != nil {
			return err
		}
		b.where = append(b.where, clause)
	}
	return nil
}

// renderOnePredicate renders a single predicate to a SQL boolean expression. A
// disjunction (Or non-empty) is rendered as a parenthesised OR of its members;
// a leaf resolves its field expression and renders the comparison.
func (b *builder) renderOnePredicate(pred planner.Predicate) (string, error) {
	if len(pred.Or) > 0 {
		parts := make([]string, 0, len(pred.Or))
		for _, sub := range pred.Or {
			c, err := b.renderOnePredicate(sub)
			if err != nil {
				return "", err
			}
			parts = append(parts, c)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	}
	var expr string
	if pred.QualifyColumn != "" {
		// Ambiguous bare column pinned to its dataset by the planner, or a
		// metric filter's aggregated column referenced through its subquery.
		expr = b.r.QuoteIdent(pred.Dataset) + "." + b.r.QuoteIdent(pred.QualifyColumn)
	} else {
		e, err := SelectExpression(pred.Field.Expression, b.r.DialectTag())
		if err != nil {
			return "", fmt.Errorf("codegen: filter field %q: %w", pred.Field.Name, err)
		}
		expr = e
	}
	if pred.CoalesceZero {
		// Metric filter null-safety: entities with no contributing rows (NULL
		// from the LEFT JOIN) compare as 0, making `= 0` a null-safe anti-join.
		expr = "COALESCE(" + expr + ", 0)"
	}
	// Null-safe equality is the one predicate whose surface form diverges per
	// dialect; defer it to the Renderer.
	if pred.Op == "IS NOT DISTINCT FROM" {
		lit, err := renderScalar(pred.Value)
		if err != nil {
			return "", fmt.Errorf("codegen: rendering IS NOT DISTINCT FROM value: %w", err)
		}
		return b.r.NullSafeEq(expr, lit), nil
	}
	return renderPredicate(expr, pred.Op, pred.Value)
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
				"codegen: expected ScanNode on join right side, got %T", n)
		}
	}
}

// render assembles the collected clauses into a single SELECT statement.
// Clauses absent from the query are omitted.
func (b *builder) render() string {
	var sb strings.Builder

	// WITH prefix for hoisted CTE definitions (WithNode).
	if len(b.ctes) > 0 {
		sb.WriteString("WITH ")
		for i, c := range b.ctes {
			if i > 0 {
				sb.WriteString(",\n")
			}
			sb.WriteString(c.name + " AS (\n" + indent(c.body) + "\n)")
		}
		sb.WriteString("\n")
	}

	switch {
	case b.distinct:
		sb.WriteString("SELECT DISTINCT\n")
	case len(b.selects) == 0 && b.star:
		// Subquery / CTE body over a single base scan: project all columns.
		sb.WriteString("SELECT *")
	default:
		sb.WriteString("SELECT\n")
	}
	for i, sel := range b.selects {
		if i > 0 {
			sb.WriteString(",\n")
		}
		sb.WriteString("  " + sel)
	}

	sb.WriteString("\nFROM " + b.from)

	for _, j := range b.joins {
		sb.WriteString("\n" + j.kind + " JOIN " + j.table)
		if j.on != "" {
			sb.WriteString("\n  ON " + j.on)
		}
	}

	if len(b.where) > 0 {
		sb.WriteString("\nWHERE " + b.where[0])
		for _, w := range b.where[1:] {
			sb.WriteString("\n  AND " + w)
		}
	}

	if len(b.groupBy) > 0 {
		sb.WriteString("\nGROUP BY " + strings.Join(b.groupBy, ", "))
	}

	if len(b.having) > 0 {
		sb.WriteString("\nHAVING " + b.having[0])
		for _, h := range b.having[1:] {
			sb.WriteString("\n  AND " + h)
		}
	}

	if len(b.orderBy) > 0 {
		sb.WriteString("\nORDER BY " + strings.Join(b.orderBy, ", "))
	}

	if b.limit != nil {
		sb.WriteString(fmt.Sprintf("\nLIMIT %d", *b.limit))
	}

	if b.offset > 0 {
		sb.WriteString(fmt.Sprintf("\nOFFSET %d", b.offset))
	}

	sb.WriteString("\n")
	return sb.String()
}

// ---- value & predicate rendering (dialect-independent) ---------------------

// renderPredicate converts a filter predicate to a SQL boolean expression.
// expr is the already-resolved field SQL fragment; val is nil for IS NULL /
// IS NOT NULL operators.
func renderPredicate(expr, op string, val json.RawMessage) (string, error) {
	switch op {
	case "IS NULL":
		return expr + " IS NULL", nil
	case "IS NOT NULL":
		return expr + " IS NOT NULL", nil
	case "IN", "NOT IN":
		lit, err := renderArray(val)
		if err != nil {
			return "", fmt.Errorf("codegen: rendering %s value: %w", op, err)
		}
		return fmt.Sprintf("%s %s (%s)", expr, op, lit), nil
	default:
		lit, err := renderScalar(val)
		if err != nil {
			return "", fmt.Errorf("codegen: rendering %s value: %w", op, err)
		}
		return fmt.Sprintf("%s %s %s", expr, op, lit), nil
	}
}

// renderScalar converts a single JSON value to a SQL literal. String, number,
// and boolean literals are identical across the dialects gavagai targets.
func renderScalar(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "NULL", nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
	}

	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}

	var bo bool
	if err := json.Unmarshal(raw, &bo); err == nil {
		if bo {
			return "TRUE", nil
		}
		return "FALSE", nil
	}

	return "", fmt.Errorf("codegen: cannot render scalar JSON value: %s", raw)
}

// renderArray converts a JSON array to a comma-separated list of SQL literals
// for use inside an IN / NOT IN clause.
func renderArray(raw json.RawMessage) (string, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return "", fmt.Errorf("expected a JSON array, got: %s", raw)
	}
	parts := make([]string, 0, len(arr))
	for _, el := range arr {
		lit, err := renderScalar(el)
		if err != nil {
			return "", err
		}
		parts = append(parts, lit)
	}
	return strings.Join(parts, ", "), nil
}

// formatFloat renders a float64 HAVING threshold as a SQL number literal.
// Whole-number values are rendered without a decimal point.
func formatFloat(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// splitRef splits a "dataset.name" reference into its parts.
func splitRef(ref string) (dataset, name string, ok bool) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
