package tests

import "testing"

// §3 — Aggregation variants

// §3.4 — ROLLUP / CUBE grouping (PENDING: ROLLUP/CUBE not in query IR)
func TestRollup(t *testing.T) {
	pendingTest(t, "3.4", "rollup", "ROLLUP grouping not yet expressible in query IR")
	_ = loadQueryRaw(t, "s03_rollup.json")
}

func TestGroupingSets(t *testing.T) {
	pendingTest(t, "3.4", "grouping-sets", "GROUPING SETS not yet expressible in query IR")
	_ = loadQueryRaw(t, "s03_grouping_sets.json")
}

// §3.5 — Nested aggregation / pre-agg (PENDING)
func TestNestedAgg(t *testing.T) {
	pendingTest(t, "3.5", "nested-agg", "nested aggregation (e.g. SUM of COUNTs) not yet supported")
	_ = loadQueryRaw(t, "s03_nested_agg.json")
}

func TestPreAggCountDistinct(t *testing.T) {
	pendingTest(t, "3.5", "pre-agg-count-distinct", "pre-aggregation for COUNT DISTINCT across joins not yet implemented")
	_ = loadQueryRaw(t, "s03_pre_agg_count_distinct.json")
}

func TestPreAggPartialSum(t *testing.T) {
	pendingTest(t, "3.5", "pre-agg-partial-sum", "partial-sum pre-aggregation (sum-of-sums) not yet implemented")
	_ = loadQueryRaw(t, "s03_pre_agg_partial_sum.json")
}
