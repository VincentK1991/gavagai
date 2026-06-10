package query

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/model"
)

// ValidationError describes one problem found while validating a query
// against a semantic model.
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// validFilterOps is the complete set of operators accepted in a Filter.
// IS NOT DISTINCT FROM is null-safe equality: rendered natively on PostgreSQL
// and as the expanded (a = b OR (a IS NULL AND b IS NULL)) form on BigQuery.
var validFilterOps = map[string]bool{
	"=": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true,
	"IN": true, "NOT IN": true,
	"IS NULL": true, "IS NOT NULL": true,
	"IS NOT DISTINCT FROM": true,
}

// validHavingOps is the set of operators accepted in a Having clause.
var validHavingOps = map[string]bool{
	"=": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true,
}

// validDirections is the set of accepted ORDER BY directions (empty = ASC).
var validDirections = map[string]bool{"": true, "ASC": true, "DESC": true}

// validNulls is the set of accepted NULLS placements (empty = dialect default).
var validNulls = map[string]bool{"": true, "FIRST": true, "LAST": true}

// Validate checks a Query against a SemanticModel and returns every problem
// found. An empty slice means the query is valid against this model.
//
// Rules enforced:
//   - At least one metric or dimension must be selected.
//   - Every "dataset.metric_name" in Metrics must have a known dataset and a
//     known metric name in the model.
//   - Every "dataset.field_name" in Dimensions must have a known dataset, a
//     known field in that dataset, and a non-nil Dimension annotation.
//   - Every Filter field must reference a known "dataset.field_name".
//   - Every Filter Op must be one of the recognised operators.
//   - IS NULL / IS NOT NULL filters must carry no Value.
//   - Every Having metric must reference a known "dataset.metric_name".
//   - Every Having Op must be a recognised comparison operator.
//   - Every OrderBy field must reference a known metric or dimension name.
//   - Every OrderBy Direction must be ASC, DESC, or empty.
func Validate(q *Query, m *model.SemanticModel) []ValidationError {
	var errs []ValidationError
	add := func(path, msg string) {
		errs = append(errs, ValidationError{Path: path, Message: msg})
	}

	if len(q.Metrics) == 0 && len(q.Dimensions) == 0 {
		add("query", "at least one metric or dimension must be selected")
	}

	// Build lookup indices from the model.
	datasets := indexDatasets(m)
	metrics := indexMetrics(m)

	// Validate metrics.
	for i, ref := range q.Metrics {
		path := fmt.Sprintf("metrics[%d]", i)
		ds, name, ok := parseRef(ref)
		if !ok {
			add(path, fmt.Sprintf("invalid reference %q: must be dataset.metric_name", ref))
			continue
		}
		if _, exists := datasets[ds]; !exists {
			add(path, fmt.Sprintf("unknown dataset %q in reference %q", ds, ref))
		}
		if !metrics[name] {
			add(path, fmt.Sprintf("unknown metric %q (not found in model %q)", name, m.Name))
		}
	}

	// Validate dimensions.
	for i, ref := range q.Dimensions {
		path := fmt.Sprintf("dimensions[%d]", i)
		ds, name, ok := parseRef(ref)
		if !ok {
			add(path, fmt.Sprintf("invalid reference %q: must be dataset.field_name", ref))
			continue
		}
		dsFields, exists := datasets[ds]
		if !exists {
			add(path, fmt.Sprintf("unknown dataset %q in reference %q", ds, ref))
			continue
		}
		f, found := dsFields[name]
		if !found {
			add(path, fmt.Sprintf("unknown field %q in dataset %q", name, ds))
			continue
		}
		if f.Dimension == nil {
			add(path, fmt.Sprintf("field %q in dataset %q has no dimension annotation and cannot be used as a dimension", name, ds))
		}
	}

	// Validate filters.
	for i, f := range q.Filters {
		path := fmt.Sprintf("filters[%d]", i)
		if len(f.Or) > 0 {
			if f.Metric != "" {
				add(path, "a filter cannot set both metric and or")
			}
			for j, sub := range f.Or {
				subPath := fmt.Sprintf("%s.or[%d]", path, j)
				if len(sub.Or) > 0 {
					add(subPath, "nested OR groups are not supported")
					continue
				}
				if sub.Metric != "" {
					add(subPath, "metric filters are not allowed inside OR groups")
					continue
				}
				validateLeafFilter(add, subPath, sub, datasets)
			}
			continue
		}
		if f.Metric != "" {
			validateMetricFilter(add, path, f, datasets, metrics)
			continue
		}
		validateLeafFilter(add, path, f, datasets)
	}

	// Validate HAVING.
	for i, h := range q.Having {
		path := fmt.Sprintf("having[%d]", i)
		ds, name, ok := parseRef(h.Metric)
		if !ok {
			add(path, fmt.Sprintf("invalid metric reference %q: must be dataset.metric_name", h.Metric))
		} else {
			if _, exists := datasets[ds]; !exists {
				add(path, fmt.Sprintf("unknown dataset %q in having metric %q", ds, h.Metric))
			}
			if !metrics[name] {
				add(path, fmt.Sprintf("unknown metric %q in having clause", name))
			}
		}
		if !validHavingOps[h.Op] {
			add(path, fmt.Sprintf("invalid having operator %q; valid operators: %s", h.Op, sortedKeys(validHavingOps)))
		}
	}

	// Validate ORDER BY.
	for i, ob := range q.OrderBy {
		path := fmt.Sprintf("order_by[%d]", i)
		// An order_by field may reference either a dimension or a metric.
		ds, name, ok := parseRef(ob.Field)
		if !ok {
			add(path, fmt.Sprintf("invalid field reference %q: must be dataset.field_name", ob.Field))
		} else {
			dsFields, dsExists := datasets[ds]
			metricExists := metrics[name]
			fieldExists := dsExists && dsFields[name].Name != ""
			if !dsExists {
				add(path, fmt.Sprintf("unknown dataset %q in order_by field %q", ds, ob.Field))
			} else if !fieldExists && !metricExists {
				add(path, fmt.Sprintf("order_by field %q is neither a known field in dataset %q nor a known metric", name, ds))
			}
		}
		if !validDirections[ob.Direction] {
			add(path, fmt.Sprintf("invalid direction %q; must be ASC, DESC, or empty", ob.Direction))
		}
		if !validNulls[ob.Nulls] {
			add(path, fmt.Sprintf("invalid nulls %q; must be FIRST, LAST, or empty", ob.Nulls))
		}
	}

	// Validate LIMIT / OFFSET.
	if q.Limit != nil && *q.Limit < 0 {
		add("limit", fmt.Sprintf("must be >= 0, got %d", *q.Limit))
	}
	if q.Offset != nil && *q.Offset < 0 {
		add("offset", fmt.Sprintf("must be >= 0, got %d", *q.Offset))
	}

	return errs
}

// validateMetricFilter checks a metric filter: metric and group_by must be
// resolvable references, the operator must be a numeric comparison, and the
// value must be a number. Field must not be set alongside Metric.
func validateMetricFilter(add func(path, msg string), path string, f Filter,
	datasets map[string]map[string]model.Field, metrics map[string]bool) {

	if f.Field != "" {
		add(path, "a filter cannot set both field and metric")
	}

	ds, name, ok := parseRef(f.Metric)
	if !ok {
		add(path, fmt.Sprintf("invalid metric reference %q: must be dataset.metric_name", f.Metric))
	} else {
		if _, exists := datasets[ds]; !exists {
			add(path, fmt.Sprintf("unknown dataset %q in metric filter %q", ds, f.Metric))
		}
		if !metrics[name] {
			add(path, fmt.Sprintf("unknown metric %q in metric filter", name))
		}
	}

	if f.GroupBy == "" {
		add(path, "metric filter requires group_by (the entity field to aggregate to)")
	} else {
		gds, gname, gok := parseRef(f.GroupBy)
		if !gok {
			add(path, fmt.Sprintf("invalid group_by reference %q: must be dataset.field_name", f.GroupBy))
		} else {
			dsFields, exists := datasets[gds]
			if !exists {
				add(path, fmt.Sprintf("unknown dataset %q in group_by %q", gds, f.GroupBy))
			} else if _, found := dsFields[gname]; !found {
				add(path, fmt.Sprintf("unknown field %q in dataset %q (group_by)", gname, gds))
			}
		}
	}

	if !validHavingOps[f.Op] {
		add(path, fmt.Sprintf("invalid metric filter operator %q; valid operators: %s", f.Op, sortedKeys(validHavingOps)))
	}

	if len(f.Value) == 0 {
		add(path, "metric filter requires a numeric value")
	} else {
		var n json.Number
		if err := json.Unmarshal(f.Value, &n); err != nil {
			add(path, fmt.Sprintf("metric filter value must be a number, got: %s", f.Value))
		}
	}
}

// validateLeafFilter checks one leaf predicate (field reference, operator, and
// value placement). It is used both for top-level filters and for the members
// of an OR group.
func validateLeafFilter(add func(path, msg string), path string, f Filter, datasets map[string]map[string]model.Field) {
	validateFilterField(add, path, f.Field, datasets)
	if !validFilterOps[f.Op] {
		add(path, fmt.Sprintf("invalid operator %q; valid operators: %s", f.Op, sortedKeys(validFilterOps)))
	}
	isNullOp := f.Op == "IS NULL" || f.Op == "IS NOT NULL"
	if isNullOp && len(f.Value) > 0 {
		add(path, fmt.Sprintf("operator %q must have no value", f.Op))
	}
	if !isNullOp && f.Op != "" && len(f.Value) == 0 {
		add(path, fmt.Sprintf("operator %q requires a value", f.Op))
	}
}

// parseRef splits a "dataset.name" reference. It returns (dataset, name, true)
// on success or ("", "", false) when the format is invalid.
func parseRef(ref string) (dataset, name string, ok bool) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// validateFilterField checks that a filter's field reference points to a known
// dataset and field.
func validateFilterField(add func(path, msg string), path, ref string, datasets map[string]map[string]model.Field) {
	ds, name, ok := parseRef(ref)
	if !ok {
		add(path, fmt.Sprintf("invalid field reference %q: must be dataset.field_name", ref))
		return
	}
	dsFields, exists := datasets[ds]
	if !exists {
		add(path, fmt.Sprintf("unknown dataset %q in filter field %q", ds, ref))
		return
	}
	if _, found := dsFields[name]; !found {
		add(path, fmt.Sprintf("unknown field %q in dataset %q", name, ds))
	}
}

// indexDatasets builds a map[datasetName]map[fieldName]Field from the model.
func indexDatasets(m *model.SemanticModel) map[string]map[string]model.Field {
	idx := make(map[string]map[string]model.Field, len(m.Datasets))
	for _, ds := range m.Datasets {
		fields := make(map[string]model.Field, len(ds.Fields))
		for _, f := range ds.Fields {
			fields[f.Name] = f
		}
		idx[ds.Name] = fields
	}
	return idx
}

// indexMetrics builds a set of metric names from the model.
func indexMetrics(m *model.SemanticModel) map[string]bool {
	idx := make(map[string]bool, len(m.Metrics))
	for _, mt := range m.Metrics {
		idx[mt.Name] = true
	}
	return idx
}

// sortedKeys returns the keys of a bool map as a sorted, comma-joined string.
func sortedKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort — map is small and this is error-path only.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return strings.Join(keys, ", ")
}
