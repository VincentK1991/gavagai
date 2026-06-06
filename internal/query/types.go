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
}

// Filter is a predicate over a single dimension field.
type Filter struct {
	// Field is a "dataset.field_name" reference.
	Field string `json:"field"`

	// Op is the comparison operator. Valid values: =, !=, >, >=, <, <=, IN,
	// NOT IN, IS NULL, IS NOT NULL.
	Op string `json:"op"`

	// Value is the right-hand side of the predicate. It is nil for IS NULL and
	// IS NOT NULL. For IN / NOT IN it should be a JSON array.
	// The raw JSON is preserved so the codegen layer can render it correctly
	// for each dialect without re-serializing.
	Value json.RawMessage `json:"value,omitempty"`
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
}
