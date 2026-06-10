// Package query defines the query intermediate representation (IR): the
// structured object that a caller (agent, BI tool, or human) submits to the
// gavagai compiler to receive SQL.
//
// References to metrics and dimensions use dot-qualified names of the form
// "dataset.name". For metrics, the dataset qualifier identifies the primary
// source dataset and is used by the planner for join resolution. For
// dimensions, it identifies the dataset whose field is being selected.
package query

import "encoding/json"

// Query is the top-level IR submitted to the compiler.
type Query struct {
	// Metrics is a list of "dataset.metric_name" references. At least one of
	// Metrics or Dimensions must be non-empty.
	Metrics []string `json:"metrics,omitempty"`

	// Dimensions is a list of "dataset.field_name" references. The referenced
	// fields must carry a Dimension annotation in the semantic model.
	Dimensions []string `json:"dimensions,omitempty"`

	// Filters are predicate conditions pushed into the WHERE clause (or
	// deeper into sub-queries when predicate pushdown applies).
	Filters []Filter `json:"filters,omitempty"`

	// Having constraints are applied after aggregation (HAVING clause).
	Having []Having `json:"having,omitempty"`

	// OrderBy defines the sort order of the result set.
	OrderBy []OrderItem `json:"order_by,omitempty"`

	// Limit caps the number of rows returned. Nil means no LIMIT clause.
	Limit *int `json:"limit,omitempty"`

	// Offset skips the first N rows of the result. Nil means no OFFSET clause.
	Offset *int `json:"offset,omitempty"`
}

// Filter is a predicate over a single dimension field, a metric filter (when
// Metric is set), or — when Or is populated — a disjunction (OR) of leaf
// predicates.
type Filter struct {
	// Field is a "dataset.field_name" reference. Ignored when Or or Metric is
	// set.
	Field string `json:"field,omitempty"`

	// Op is the comparison operator. Valid values: =, !=, >, >=, <, <=, IN,
	// NOT IN, IS NULL, IS NOT NULL, IS NOT DISTINCT FROM (null-safe equality).
	// Metric filters accept only the comparison subset (=, !=, >, >=, <, <=).
	// Ignored when Or is set.
	Op string `json:"op,omitempty"`

	// Value is the right-hand side of the predicate. It is nil for IS NULL and
	// IS NOT NULL. For IN / NOT IN it should be a JSON array. For metric
	// filters it must be a number.
	// The raw JSON is preserved so the codegen layer can render it correctly
	// for each dialect without re-serializing. Ignored when Or is set.
	Value json.RawMessage `json:"value,omitempty"`

	// Metric, when set, makes this a metric filter (dbt MetricFlow's
	// `Metric('m', group_by=['entity'])` pattern): the named metric is
	// aggregated per GroupBy entity in a grouped subquery, LEFT JOINed onto the
	// outer query on that entity, and the aggregated value is compared with
	// Op/Value. Rows of the GroupBy entity with no contributing metric rows
	// compare as 0 (COALESCE), so `> 0` expresses a semi-join ("entities WITH
	// related rows") and `= 0` an anti-join ("entities WITHOUT related rows").
	// Field is ignored when Metric is set; Metric is a "dataset.metric_name"
	// reference.
	Metric string `json:"metric,omitempty"`

	// GroupBy names the entity the metric is aggregated to and joined back on,
	// as a "dataset.field_name" reference to a field of the OUTER dataset being
	// filtered (e.g. "customers.customer_id"). Required when Metric is set;
	// ignored otherwise.
	GroupBy string `json:"group_by,omitempty"`

	// Or, when non-empty, makes this filter a disjunction group: the condition
	// is the OR of every sub-filter, and Field/Op/Value are ignored. Sub-filters
	// must be leaf predicates (no further nesting, no metric filters).
	// Top-level filters remain AND-combined, so a group nested in the list
	// yields `(a OR b) AND c`.
	Or []Filter `json:"or,omitempty"`
}

// Having is a post-aggregation predicate over a metric.
type Having struct {
	// Metric is a "dataset.metric_name" reference.
	Metric string `json:"metric"`

	// Op is the comparison operator. Valid values: =, !=, >, >=, <, <=.
	Op string `json:"op"`

	// Value is the numeric threshold.
	Value float64 `json:"value"`
}

// OrderItem specifies one field (metric or dimension) in the ORDER BY clause.
type OrderItem struct {
	// Field is a "dataset.field_or_metric_name" reference.
	Field string `json:"field"`

	// Direction is ASC or DESC. Empty string is treated as ASC.
	Direction string `json:"direction,omitempty"`

	// Nulls controls null ordering: FIRST, LAST, or empty for the dialect
	// default. Rendered as NULLS FIRST / NULLS LAST.
	Nulls string `json:"nulls,omitempty"`
}
