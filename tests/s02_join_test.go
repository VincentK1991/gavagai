package tests

import (
	"strings"
	"testing"
)

// §2 — Join behaviour

// §2.2 — Self-join via a role dataset (LookML's `from:` pattern): `managers`
// is a second logical dataset over the same source as `employees`, so the
// compiler emits an ordinary join with distinct aliases over one table.
func TestSelfJoinBasic(t *testing.T) {
	sql := compileSQL(t, "hierarchy.yaml", "s02_self_join_basic.json")
	assertContains(t, sql, `hr.employees AS "employees"`)
	assertContains(t, sql, `LEFT JOIN hr.employees AS "managers"`)
	assertContains(t, sql, `ON "employees"."manager_id" = "managers"."employee_id"`)
	// `name` exists on both roles, so the selected dimension is qualified.
	assertContains(t, sql, `"managers"."name"`)
}

// §2.2 — filters distinguish the two roles: each is qualified to its own
// alias and pushed to its own scan.
func TestSelfJoinFilter(t *testing.T) {
	sql := compileSQL(t, "hierarchy.yaml", "s02_self_join_filter.json")
	assertContains(t, sql, `"employees"."department" = 'Engineering'`)
	assertContains(t, sql, `"managers"."department" = 'Sales'`)
}

// §2.2 — an additive metric at the managers grain (the "one" side of the
// self-join) fans out per report row and is refused.
func TestSelfJoinFanout(t *testing.T) {
	m := loadModel(t, "hierarchy.yaml")
	q := loadQuery(t, "s02_self_join_fanout.json")
	_, err := planFromResult(m, q)
	if err == nil || !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected fan-out error for managers-grain SUM across self-join, got: %v", err)
	}
}

// §2.3 — Semi-join via metric filter (MetricFlow's Metric() pattern):
// `order_count > 0` per customer renders as a grouped subquery LEFT JOIN.
func TestSemiJoinExists(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s02_semi_join_exists.json")
	assertContains(t, sql, "LEFT JOIN (")
	assertContains(t, sql, `GROUP BY "customers"."customer_id"`)
	assertContains(t, sql, `WHERE COALESCE("mf0_order_count"."order_count", 0) > 0`)
}

// §2.3 — the same construct with a value threshold: customers whose total
// revenue meets a floor (the semantic-layer form of an IN-subquery filter).
func TestSemiJoinIn(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s02_semi_join_in.json")
	assertContains(t, sql, "LEFT JOIN (")
	assertContains(t, sql, `WHERE COALESCE("mf0_revenue"."revenue", 0) >= 1000`)
}

// §2.3 — the subquery groups by the entity, so the join is one row per
// customer and can never duplicate outer rows.
func TestSemiJoinNoDup(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s02_semi_join_no_dup.json")
	assertContains(t, sql, `GROUP BY "customers"."customer_id"`)
	assertContains(t, sql, `ON "customers"."customer_id" = "mf0_order_count"."mf_key"`)
}

// §2.4 — Anti-join is the metric filter with `= 0`: customers with no orders.
func TestAntiJoinNotExists(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s02_anti_join_not_exists.json")
	assertContains(t, sql, "LEFT JOIN (")
	assertContains(t, sql, `WHERE COALESCE("mf0_order_count"."order_count", 0) = 0`)
}

// §2.4 — no NOT IN variant is needed: the single null-safe pattern serves
// both dialects (and sidesteps NOT IN's NULL trap entirely).
func TestAntiJoinNotIn(t *testing.T) {
	for _, dialect := range []string{"postgres", "bigquery"} {
		sql := compileSQLDialect(t, "ecommerce.yaml", "s02_anti_join_not_in.json", dialect)
		assertContains(t, sql, "COALESCE(")
		assertNotContains(t, sql, "NOT IN (SELECT")
	}
}

// §2.4 — null-safety: customers absent from the subquery get NULL from the
// LEFT JOIN; COALESCE(metric, 0) makes them compare as 0.
func TestAntiJoinNullSafe(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s02_anti_join_null_safe.json")
	assertContains(t, sql, "LEFT JOIN (")
	assertContains(t, sql, "COALESCE(")
	assertContains(t, sql, "= 0")
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
