package tests

import "testing"

// §1 — Filter / HAVING behaviour

// §1.4 — Subquery pushdown filter (PENDING: subquery codegen not implemented)
func TestCTEFilter(t *testing.T) {
	pendingTest(t, "1.4", "cte-filter", "CTE / WITH codegen not yet implemented")
	_ = loadQueryRaw(t, "s01_cte_filter.json")
}

func TestSubqueryFilter(t *testing.T) {
	pendingTest(t, "1.4", "subquery-filter", "inline subquery codegen not yet implemented")
	_ = loadQueryRaw(t, "s01_subquery_filter.json")
}

// §1.5 — HAVING with safe aggregate metrics (CAN TEST NOW)
func TestHavingCountDistinct(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s01_having_count_distinct.json")
	assertContains(t, sql, "HAVING")
	assertContains(t, sql, "COUNT(DISTINCT")
	assertContains(t, sql, "> 5")
}

func TestHavingMinMax(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s01_having_min_max.json")
	assertContains(t, sql, "HAVING")
	assertContains(t, sql, "MAX(")
	assertContains(t, sql, "> 1000")
}
