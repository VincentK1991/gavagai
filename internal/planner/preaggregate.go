package planner

import (
	"fmt"

	"github.com/vincentk1991/gavagai/internal/model"
)

// planPreAggregated rewrites a fan-out-prone query into a fan-out-safe plan by
// computing each metric on its own grain inside an isolated aggregate subquery
// and combining the per-grain results. Because no metric is aggregated across
// the fan-out join, SUM / AVG / COUNT are each computed correctly over their
// natural grain — the AVG case needs no numerator/denominator split.
//
// It handles the tractable shapes: every grouping dimension must live on a
// dataset reachable from each metric's grain without fanning that grain out (a
// parent / one-side lookup). Genuinely ambiguous many-to-many shapes, and
// queries whose HAVING references a pre-aggregated metric, are declined by
// returning an error so the caller surfaces the original FanOutError.
func planPreAggregated(
	m *model.SemanticModel,
	metrics []MetricExpr,
	dims []DimensionExpr,
	preds []Predicate,
	havings []HavingPredicate,
) (PlanNode, error) {
	if len(havings) > 0 {
		return nil, fmt.Errorf("pre-aggregation: HAVING over a pre-aggregated metric is not supported")
	}
	if len(metrics) == 0 {
		return nil, fmt.Errorf("pre-aggregation: query has no metrics to pre-aggregate")
	}

	dimDS := dimensionDatasets(dims)

	// Partition metrics by their grain (source dataset), preserving first-seen
	// order for deterministic output.
	type grain struct {
		dataset string
		metrics []MetricExpr
		sub     *SubqueryNode
	}
	var grains []*grain
	byDS := map[string]*grain{}
	for _, me := range metrics {
		g := byDS[me.Dataset]
		if g == nil {
			g = &grain{dataset: me.Dataset}
			byDS[me.Dataset] = g
			grains = append(grains, g)
		}
		g.metrics = append(g.metrics, me)
	}

	// Build one aggregate subquery per grain. Each scans its grain dataset
	// (joining up to any dimension datasets), groups by the shared dimensions,
	// and computes only that grain's metrics.
	placed := make([]bool, len(preds))
	for _, g := range grains {
		refs := grainRefs(g.dataset, dimDS)
		refSet := make(map[string]bool, len(refs))
		for _, r := range refs {
			refSet[r] = true
		}

		base, edges, err := resolveJoins(refs, m)
		if err != nil {
			return nil, err
		}
		// The grain is the root of its own sub-join, so its metrics must not fan
		// out within the subquery; if a dimension forces a many-to-many path,
		// detectFanOut trips and we decline the rewrite.
		if err := detectFanOut(g.metrics, edges); err != nil {
			return nil, err
		}

		var gp []Predicate
		for i, p := range preds {
			if refSet[p.Dataset] {
				gp = append(gp, p)
				placed[i] = true
			}
		}
		input := base
		if len(gp) > 0 {
			input = &FilterNode{Input: input, Predicates: gp}
		}
		g.sub = &SubqueryNode{
			Input: &AggregateNode{Input: input, GroupBy: dims, Aggregates: g.metrics},
			Alias: g.dataset,
		}
	}

	for i := range preds {
		if !placed[i] {
			return nil, fmt.Errorf("pre-aggregation: filter on dataset %q has no home grain", preds[i].Dataset)
		}
	}

	// Combine the per-grain subqueries: join on the shared dimensions, or cross-
	// join the single-row aggregates when the query has no dimensions.
	firstAlias := grains[0].sub.Alias
	combined := PlanNode(grains[0].sub)
	for _, g := range grains[1:] {
		if len(dims) == 0 {
			combined = &JoinNode{Kind: CrossJoin, Left: combined, Right: g.sub}
			continue
		}
		on := make([]JoinCondition, 0, len(dims))
		for _, d := range dims {
			on = append(on, JoinCondition{
				Left:  ColumnRef{Dataset: firstAlias, Column: d.Field.Name},
				Right: ColumnRef{Dataset: g.sub.Alias, Column: d.Field.Name},
			})
		}
		combined = &JoinNode{Kind: LeftJoin, Left: combined, Right: g.sub, On: on}
	}

	// Project the already-aggregated columns: dimensions from the first grain,
	// then each metric from its own grain.
	items := make([]ProjectItem, 0, len(dims)+len(metrics))
	for _, d := range dims {
		items = append(items, ProjectItem{Source: firstAlias, Column: d.Field.Name, Alias: d.Field.Name})
	}
	for _, me := range metrics {
		items = append(items, ProjectItem{Source: me.Dataset, Column: me.Metric.Name, Alias: me.Metric.Name})
	}
	return &ProjectNode{Input: combined, Items: items}, nil
}

// dimensionDatasets returns the datasets referenced by dims, de-duplicated and
// in first-seen order.
func dimensionDatasets(dims []DimensionExpr) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dims {
		if !seen[d.Dataset] {
			seen[d.Dataset] = true
			out = append(out, d.Dataset)
		}
	}
	return out
}

// grainRefs returns the datasets a grain's subquery must reference: the grain
// itself (as the join root) followed by every dimension dataset.
func grainRefs(grain string, dimDatasets []string) []string {
	refs := []string{grain}
	for _, d := range dimDatasets {
		if d != grain {
			refs = append(refs, d)
		}
	}
	return refs
}
