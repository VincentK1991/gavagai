package tests

import "testing"

// §9 — NULL handling

// §9 CAN TEST NOW: COALESCE expression in GROUP BY renders correctly
func TestCoalesceGroupby(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s09_coalesce_groupby.json")
	assertContains(t, sql, "COALESCE(")
	assertContains(t, sql, "GROUP BY")
}

// PENDING §9 — null-safe equality filter (IS NOT DISTINCT FROM)
func TestNullSafeEq(t *testing.T) {
	pendingTest(t, "9", "null-safe-eq",
		"IS NOT DISTINCT FROM / null-safe equality operator not yet in the query IR")
	_ = loadQueryRaw(t, "s09_null_safe_eq.json")
}

// PENDING §9 — anti-join via NULL-generating outer join
func TestAntiJoinNull(t *testing.T) {
	pendingTest(t, "9", "anti-join-null",
		"NULL-generating anti-join pattern not yet supported by the planner")
	_ = loadQueryRaw(t, "s09_anti_join_null.json")
}
