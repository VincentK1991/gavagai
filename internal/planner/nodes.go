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

func (*ScanNode) planNode()      {}
func (*JoinNode) planNode()      {}
func (*FilterNode) planNode()    {}
func (*AggregateNode) planNode() {}
func (*HavingNode) planNode()    {}
func (*OrderNode) planNode()     {}
func (*LimitNode) planNode()     {}
