// Package planner resolves a validated (query, semantic model) pair into a
// dialect-independent relational-algebra plan tree. It owns join resolution
// and fan-out detection — the correctness core of gavagai. No node in this
// package is aware of any SQL dialect; rendering happens in internal/codegen.
package planner

import (
	"encoding/json"

	"github.com/vincentk1991/gavagai/internal/model"
)

// PlanNode is a node in the relational-algebra plan tree. The unexported
// marker method keeps the set of node types closed to this package.
type PlanNode interface {
	planNode()
}

// JoinKind enumerates the supported SQL join types.
type JoinKind string

const (
	// InnerJoin drops rows with no match on either side.
	InnerJoin JoinKind = "INNER"
	// LeftJoin preserves all rows from the left (fact) input. gavagai uses
	// LEFT joins by default so dimension lookups never drop fact rows.
	LeftJoin JoinKind = "LEFT"
	// CrossJoin combines every row of each side. It carries no ON condition and
	// is used to combine independent single-row pre-aggregates that share no
	// grouping dimension.
	CrossJoin JoinKind = "CROSS"
)

// ColumnRef is a reference to a physical column within a dataset (by its
// logical/alias name).
type ColumnRef struct {
	Dataset string
	Column  string
}

// JoinCondition is a single equality between two columns in a join.
type JoinCondition struct {
	Left  ColumnRef
	Right ColumnRef
}

// MetricExpr is a metric selected in the aggregate list. Dataset is the source
// dataset qualifier from the query reference (e.g. "orders" in "orders.revenue");
// it determines the metric's grain for fan-out analysis.
type MetricExpr struct {
	Ref     string
	Dataset string
	Metric  *model.Metric
}

// DimensionExpr is a dimension selected for GROUP BY / projection.
type DimensionExpr struct {
	Ref     string
	Dataset string
	Field   *model.Field

	// QualifyColumn, when non-empty, forces the dimension to render as the
	// qualified reference Dataset.QualifyColumn instead of its raw expression.
	// The planner sets it when a bare column would be ambiguous across joined
	// datasets. OutAlias, when non-empty, overrides the output column alias so
	// columns that share a name stay distinct.
	QualifyColumn string
	OutAlias      string
}

// Predicate is a resolved filter condition over a dimension field. Value is
// the raw JSON from the query IR, preserved so the emitter can render it per
// dialect. It is empty for IS NULL / IS NOT NULL.
type Predicate struct {
	Dataset string
	Field   *model.Field
	Op      string
	Value   json.RawMessage

	// Or, when non-empty, makes this a disjunction: the condition is
	// (Or[0] OR Or[1] OR ...) and Field/Op/Value are unused. Dataset is set to
	// the common dataset shared by every disjunct (so the group can be pushed
	// to that scan), or "" when the disjuncts span multiple datasets — in which
	// case the group stays above the join as a residual filter.
	Or []Predicate

	// QualifyColumn, when non-empty, forces the predicate's left-hand side to
	// render as the qualified reference Dataset.QualifyColumn instead of the
	// field's raw expression — set by the planner when a bare column would be
	// ambiguous across joined datasets, and by metric filters to reference the
	// aggregated column of their grouped subquery.
	QualifyColumn string

	// CoalesceZero wraps the left-hand side in COALESCE(expr, 0). Metric
	// filters set it so entities with no contributing rows (NULL from the LEFT
	// JOIN to the grouped subquery) compare as 0 — this is what makes `= 0` a
	// null-safe anti-join.
	CoalesceZero bool
}

// HavingPredicate is a resolved post-aggregation condition over a metric.
type HavingPredicate struct {
	Dataset string
	Metric  *model.Metric
	Op      string
	Value   float64
}

// OrderExpr is one entry in the ORDER BY clause. Ref is the dot-qualified
// metric or dimension reference; Direction is normalised to ASC or DESC.
type OrderExpr struct {
	Ref       string
	Direction string
	// Nulls is FIRST, LAST, or "" for the dialect default null ordering.
	Nulls string
}

// ScanNode reads all rows from a single dataset.
type ScanNode struct {
	Dataset *model.Dataset
	Alias   string
}

// JoinNode joins two inputs on a set of equality conditions. Relationship
// records the OSI relationship that produced the join, used for diagnostics
// and fan-out analysis.
type JoinNode struct {
	Left         PlanNode
	Right        PlanNode
	On           []JoinCondition
	Kind         JoinKind
	Relationship *model.Relationship
}

// FilterNode applies WHERE predicates (AND-combined) to its input. Predicates
// sit above the join pre-pushdown; Phase 4 relocates them to the lowest scope
// that exposes their columns.
type FilterNode struct {
	Input      PlanNode
	Predicates []Predicate
}

// AggregateNode groups by the dimension expressions and computes the metric
// aggregates. With no aggregates it is a SELECT DISTINCT over the group keys.
type AggregateNode struct {
	Input      PlanNode
	GroupBy    []DimensionExpr
	Aggregates []MetricExpr
}

// HavingNode applies post-aggregation predicates (HAVING).
type HavingNode struct {
	Input      PlanNode
	Predicates []HavingPredicate
}

// OrderNode sorts its input.
type OrderNode struct {
	Input PlanNode
	Items []OrderExpr
}

// LimitNode caps the number of output rows and/or skips a prefix. Count is the
// LIMIT value and is meaningful only when HasLimit is true; Offset is the
// OFFSET value, with 0 meaning no OFFSET. The node is present whenever the
// query carries a LIMIT, an OFFSET, or both.
type LimitNode struct {
	Input    PlanNode
	Count    int
	HasLimit bool
	Offset   int
}

// SubqueryNode renders Input as a parenthesised derived table aliased as Alias
// and used as a FROM/JOIN source: (SELECT ... ) AS alias. Because PushDown has
// already relocated a dataset's predicates directly above its ScanNode,
// wrapping that filtered scan in a SubqueryNode evaluates the predicate inside
// the subquery rather than the outer query — the predicate-pushdown-into-
// subquery rewrite.
type SubqueryNode struct {
	Input PlanNode
	Alias string
}

// CTERef references a named common table expression as a table source, rendered
// as `cte AS alias`. The CTE body lives in the enclosing WithNode.
type CTERef struct {
	Name  string
	Alias string
}

// CTEDef is one named common table expression: Name AS (Query).
type CTEDef struct {
	Name  string
	Query PlanNode
}

// WithNode prepends one or more CTE definitions to Body and renders as
// `WITH name AS (...), ... <Body>`. CTEs are emitted in slice order, so a later
// definition may reference an earlier one by CTERef (nested CTEs).
type WithNode struct {
	CTEs []CTEDef
	Body PlanNode
}

// ProjectItem is one output column of a ProjectNode: it selects Column from the
// input source aliased Source, exposing it as Alias.
type ProjectItem struct {
	Source string // input source alias (a SubqueryNode alias)
	Column string // column name within that source
	Alias  string // output column name
}

// ProjectNode selects pre-computed columns from its input by qualified
// reference (source.column AS alias), without re-aggregating. It is the
// outermost node of a pre-aggregated plan: the per-grain aggregates live in the
// input subqueries, and ProjectNode assembles their already-aggregated columns
// into the final row shape.
type ProjectNode struct {
	Input PlanNode
	Items []ProjectItem
}

func (*ScanNode) planNode()      {}
func (*JoinNode) planNode()      {}
func (*FilterNode) planNode()    {}
func (*AggregateNode) planNode() {}
func (*HavingNode) planNode()    {}
func (*OrderNode) planNode()     {}
func (*LimitNode) planNode()     {}
func (*SubqueryNode) planNode()  {}
func (*CTERef) planNode()        {}
func (*WithNode) planNode()      {}
func (*ProjectNode) planNode()   {}
