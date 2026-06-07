package tests

import "testing"

// §2 — Join behaviour

// §2.2 — Self-join (PENDING: self-join IR not implemented)
func TestSelfJoinBasic(t *testing.T) {
	pendingTest(t, "2.2", "self-join-basic", "self-join (from==to) not yet supported by the planner")
	_ = loadQueryRaw(t, "s02_self_join_basic.json")
}

func TestSelfJoinFilter(t *testing.T) {
	pendingTest(t, "2.2", "self-join-filter", "self-join (from==to) not yet supported by the planner")
	_ = loadQueryRaw(t, "s02_self_join_filter.json")
}

func TestSelfJoinFanout(t *testing.T) {
	pendingTest(t, "2.2", "self-join-fanout", "self-join fan-out detection not yet supported")
	_ = loadQueryRaw(t, "s02_self_join_fanout.json")
}

// §2.3 — Semi-join (PENDING: semi-join IR not implemented)
func TestSemiJoinExists(t *testing.T) {
	pendingTest(t, "2.3", "semi-join-exists", "semi-join (EXISTS) not yet expressible in query IR")
	_ = loadQueryRaw(t, "s02_semi_join_exists.json")
}

func TestSemiJoinIn(t *testing.T) {
	pendingTest(t, "2.3", "semi-join-in", "semi-join (IN) not yet expressible in query IR")
	_ = loadQueryRaw(t, "s02_semi_join_in.json")
}

func TestSemiJoinNoDup(t *testing.T) {
	pendingTest(t, "2.3", "semi-join-no-dup", "semi-join deduplication not yet supported")
	_ = loadQueryRaw(t, "s02_semi_join_no_dup.json")
}

// §2.4 — Anti-join (PENDING: anti-join IR not implemented)
func TestAntiJoinNotExists(t *testing.T) {
	pendingTest(t, "2.4", "anti-join-not-exists", "anti-join (NOT EXISTS) not yet expressible in query IR")
	_ = loadQueryRaw(t, "s02_anti_join_not_exists.json")
}

func TestAntiJoinNotIn(t *testing.T) {
	pendingTest(t, "2.4", "anti-join-not-in", "anti-join (NOT IN) not yet expressible in query IR")
	_ = loadQueryRaw(t, "s02_anti_join_not_in.json")
}

func TestAntiJoinNullSafe(t *testing.T) {
	pendingTest(t, "2.4", "anti-join-null-safe", "null-safe anti-join not yet supported")
	_ = loadQueryRaw(t, "s02_anti_join_null_safe.json")
}

// §2.5 — Pre-aggregation before join (PENDING: pre-agg IR not implemented)
func TestPreAggSum(t *testing.T) {
	pendingTest(t, "2.5", "pre-agg-sum", "pre-aggregation node not yet implemented in the planner")
	_ = loadQueryRaw(t, "s02_pre_agg_sum.json")
}

func TestPreAggAvg(t *testing.T) {
	pendingTest(t, "2.5", "pre-agg-avg", "pre-aggregation node not yet implemented in the planner")
	_ = loadQueryRaw(t, "s02_pre_agg_avg.json")
}
