package planner

import (
	"encoding/json"
	"fmt"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/query"
)

// mfKeyAlias is the output alias of the entity column inside every metric
// filter subquery. A fixed name keeps the subquery's projection from colliding
// with bare columns of the outer query (each subquery has its own namespace,
// so a constant is safe).
const mfKeyAlias = "mf_key"

// resolvedMetricFilter is a metric filter after reference resolution: the
// metric to aggregate, the entity to aggregate it to (a field on the outer
// dataset being filtered), and the comparison applied to the aggregated value.
type resolvedMetricFilter struct {
	Metric      MetricExpr
	EntityDS    string
	EntityField *model.Field
	Op          string
	Value       json.RawMessage
}

// resolveMetricFilters resolves every metric filter (Filter.Metric != "") in
// filters against the model index. Plain field filters are ignored here; they
// are handled by resolveFilters.
func resolveMetricFilters(filters []query.Filter, idx *index) ([]resolvedMetricFilter, error) {
	var out []resolvedMetricFilter
	for _, f := range filters {
		if f.Metric == "" {
			continue
		}
		ds, name, ok := splitRef(f.Metric)
		if !ok {
			return nil, fmt.Errorf("plan: invalid metric filter reference %q", f.Metric)
		}
		mt, found := idx.metrics[name]
		if !found {
			return nil, fmt.Errorf("plan: unknown metric %q in metric filter", f.Metric)
		}
		gds, gname, gok := splitRef(f.GroupBy)
		if !gok {
			return nil, fmt.Errorf("plan: invalid metric filter group_by %q", f.GroupBy)
		}
		field, err := idx.field(gds, gname)
		if err != nil {
			return nil, fmt.Errorf("plan: metric filter group_by: %w", err)
		}
		out = append(out, resolvedMetricFilter{
			Metric:      MetricExpr{Ref: f.Metric, Dataset: ds, Metric: mt},
			EntityDS:    gds,
			EntityField: field,
			Op:          f.Op,
			Value:       f.Value,
		})
	}
	return out, nil
}

// applyMetricFilter rewrites base to evaluate one metric filter, following the
// pattern dbt MetricFlow generates for `Metric('m', group_by=['entity'])`
// where-filters:
//
//  1. Build a grouped subquery that aggregates the metric to the entity grain:
//     SELECT entity AS mf_key, <metric expr> AS <metric> FROM ... GROUP BY entity
//  2. LEFT JOIN it onto the outer tree on the entity column.
//  3. Compare COALESCE(sub.<metric>, 0) with the filter's op/value in the
//     outer WHERE (returned as a Predicate for the caller's FilterNode).
//
// Because the subquery groups by the entity, it has exactly one row per
// entity: the join can never duplicate outer rows (semi-join safety), and the
// COALESCE makes entities with no related rows compare as 0, so `= 0` is a
// null-safe anti-join. i distinguishes multiple metric filters in one query.
func applyMetricFilter(base PlanNode, mf resolvedMetricFilter, i int, m *model.SemanticModel) (PlanNode, Predicate, error) {
	alias := fmt.Sprintf("mf%d_%s", i, mf.Metric.Metric.Name)

	// The subquery scans the metric's dataset and joins up to the entity's
	// dataset when they differ (resolveJoins finds the relationship path).
	refs := []string{mf.Metric.Dataset}
	if mf.EntityDS != mf.Metric.Dataset {
		refs = append(refs, mf.EntityDS)
	}
	inner, edges, err := resolveJoins(refs, m)
	if err != nil {
		return nil, Predicate{}, fmt.Errorf("plan: metric filter %q: %w", mf.Metric.Ref, err)
	}
	// The metric must not fan out inside its own subquery (e.g. an additive
	// metric whose entity path duplicates the metric's grain).
	if err := detectFanOut([]MetricExpr{mf.Metric}, edges); err != nil {
		return nil, Predicate{}, fmt.Errorf("plan: metric filter %q: %w", mf.Metric.Ref, err)
	}

	// The entity column is exposed under the fixed mf_key alias; qualify it
	// first in case both joined datasets carry the bare column.
	entityDim := DimensionExpr{
		Ref:     mf.EntityDS + "." + mf.EntityField.Name,
		Dataset: mf.EntityDS,
		Field:   mf.EntityField,
	}
	dims, _ := qualifyAmbiguousColumns([]DimensionExpr{entityDim}, nil, scanDatasets(inner))
	dims[0].OutAlias = mfKeyAlias

	sub := &SubqueryNode{
		Input: &AggregateNode{Input: inner, GroupBy: dims, Aggregates: []MetricExpr{mf.Metric}},
		Alias: alias,
	}

	// Join the subquery onto the outer tree on the entity's physical column.
	col, ok := bareColumn(mf.EntityField.Expression)
	if !ok {
		return nil, Predicate{}, fmt.Errorf(
			"plan: metric filter group_by %q.%q must be a plain column, not an expression",
			mf.EntityDS, mf.EntityField.Name)
	}
	joined := &JoinNode{
		Left:  base,
		Right: sub,
		On: []JoinCondition{{
			Left:  ColumnRef{Dataset: mf.EntityDS, Column: col},
			Right: ColumnRef{Dataset: alias, Column: mfKeyAlias},
		}},
		Kind: LeftJoin,
	}

	pred := Predicate{
		Dataset:       alias,
		QualifyColumn: mf.Metric.Metric.Name,
		Op:            mf.Op,
		Value:         mf.Value,
		CoalesceZero:  true,
	}
	return joined, pred, nil
}
