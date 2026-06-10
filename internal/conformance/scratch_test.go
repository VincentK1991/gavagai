package conformance

import (
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// TestMetricFilterComposition covers the interaction of a metric filter with
// the rest of the pipeline: a regular filter on the outer dataset still pushes
// down to its scan, while the metric-filter predicate stays above the LEFT
// JOIN (it must see the join's NULLs to be null-safe).
func TestMetricFilterComposition(t *testing.T) {
	q := &query.Query{
		Metrics: []string{"customers.customer_count"},
		Filters: []query.Filter{
			{Metric: "orders.revenue", GroupBy: "customers.customer_id", Op: ">=", Value: raw(`1000`)},
			{Field: "customers.region", Op: "=", Value: raw(`"US"`)},
		},
	}
	plan := mustPlan(t, q)

	// The regular predicate is pushed to the customers scan; the metric-filter
	// predicate is residual above the join.
	var scanFilter, residual *planner.FilterNode
	for _, f := range nodesOf[*planner.FilterNode](plan) {
		switch f.Input.(type) {
		case *planner.ScanNode:
			scanFilter = f
		case *planner.JoinNode:
			residual = f
		}
	}
	if scanFilter == nil || len(scanFilter.Predicates) != 1 || scanFilter.Predicates[0].Field.Name != "region" {
		t.Errorf("regular filter should push down to the customers scan, got %+v", scanFilter)
	}
	if residual == nil || len(residual.Predicates) != 1 || !residual.Predicates[0].CoalesceZero {
		t.Errorf("metric-filter predicate should stay above the join, got %+v", residual)
	}

	sql := compilePostgres(t, q)
	if !strings.Contains(sql, `region = 'US'`) {
		t.Errorf("regular filter should render in WHERE:\n%s", sql)
	}
	if !strings.Contains(sql, `COALESCE("mf0_revenue"."revenue", 0) >= 1000`) {
		t.Errorf("metric filter should render as null-safe threshold:\n%s", sql)
	}
}
