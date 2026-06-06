// Package model defines the Go representation of an OSI (Open Semantic
// Interchange) semantic model and the routines to parse and validate one.
//
// The struct shapes mirror the OSI core schema
// (github.com/open-semantic-interchange/OSI). A single set of `json` struct
// tags serves both JSON and YAML inputs because the YAML path converts to
// JSON before unmarshaling.
package model

import (
	"encoding/json"
	"fmt"
)

// Document is the top-level OSI file envelope: a version string and one or
// more semantic models under the `semantic_model` key.
type Document struct {
	Version string          `json:"version,omitempty"`
	Models  []SemanticModel `json:"semantic_model"`
}

// SemanticModel is a single OSI semantic model: the metrics, dimensions, and
// relationships that define what the underlying data means.
type SemanticModel struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	AIContext        *AIContext        `json:"ai_context,omitempty"`
	Datasets         []Dataset         `json:"datasets"`
	Relationships    []Relationship    `json:"relationships,omitempty"`
	Metrics          []Metric          `json:"metrics,omitempty"`
	CustomExtensions []CustomExtension `json:"custom_extensions,omitempty"`
}

// Dataset is a physical table or view binding plus the fields it exposes.
type Dataset struct {
	Name             string            `json:"name"`
	Source           string            `json:"source"`
	PrimaryKey       []string          `json:"primary_key,omitempty"`
	UniqueKeys       [][]string        `json:"unique_keys,omitempty"`
	Description      string            `json:"description,omitempty"`
	AIContext        *AIContext        `json:"ai_context,omitempty"`
	Fields           []Field           `json:"fields,omitempty"`
	CustomExtensions []CustomExtension `json:"custom_extensions,omitempty"`
}

// Field is a row-level attribute used for grouping, filtering, or as part of
// an expression. A Field with a non-nil Dimension is selectable as a
// dimension in a query.
type Field struct {
	Name             string            `json:"name"`
	Expression       Expression        `json:"expression"`
	Dimension        *Dimension        `json:"dimension,omitempty"`
	Label            string            `json:"label,omitempty"`
	Description      string            `json:"description,omitempty"`
	AIContext        *AIContext        `json:"ai_context,omitempty"`
	CustomExtensions []CustomExtension `json:"custom_extensions,omitempty"`
}

// Metric is a quantitative measure expressed as an aggregate.
type Metric struct {
	Name             string            `json:"name"`
	Expression       Expression        `json:"expression"`
	Description      string            `json:"description,omitempty"`
	AIContext        *AIContext        `json:"ai_context,omitempty"`
	CustomExtensions []CustomExtension `json:"custom_extensions,omitempty"`
}

// Relationship is a foreign-key join between two datasets. Column order in
// FromColumns and ToColumns is positional for composite keys.
type Relationship struct {
	Name             string            `json:"name"`
	From             string            `json:"from"`
	To               string            `json:"to"`
	FromColumns      []string          `json:"from_columns"`
	ToColumns        []string          `json:"to_columns"`
	AIContext        *AIContext        `json:"ai_context,omitempty"`
	CustomExtensions []CustomExtension `json:"custom_extensions,omitempty"`
}

// Expression holds the per-dialect SQL fragments for a field or metric.
type Expression struct {
	Dialects []DialectExpression `json:"dialects"`
}

// DialectExpression is a single SQL fragment tagged with the dialect it
// targets (e.g. ANSI_SQL, BIGQUERY, POSTGRES).
type DialectExpression struct {
	Dialect    string `json:"dialect"`
	Expression string `json:"expression"`
}

// Dimension marks a field as groupable/filterable and records whether it is a
// time dimension (which enables grain operations).
type Dimension struct {
	IsTime bool `json:"is_time,omitempty"`
}

// CustomExtension carries vendor-specific metadata that OSI does not model
// natively. It is opaque to the compiler.
type CustomExtension struct {
	VendorName string `json:"vendor_name"`
	Data       string `json:"data"`
}

// AIContext is OSI's polymorphic ai_context value: either a bare instructions
// string or an object with instructions/synonyms/examples. The bare-string
// form is stored in Text.
type AIContext struct {
	Text         string   `json:"-"`
	Instructions string   `json:"instructions,omitempty"`
	Synonyms     []string `json:"synonyms,omitempty"`
	Examples     []string `json:"examples,omitempty"`
}

// UnmarshalJSON accepts both the string and object forms of ai_context.
func (a *AIContext) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		a.Text = s
		return nil
	}

	// Use an alias to avoid recursing into this method.
	type alias AIContext
	var obj alias
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("ai_context: must be a string or object: %w", err)
	}
	*a = AIContext(obj)
	return nil
}
