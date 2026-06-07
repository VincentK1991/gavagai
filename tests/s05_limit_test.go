package tests

import "testing"

// §5 — LIMIT / OFFSET

// CAN TEST NOW: basic LIMIT renders correctly
func TestLimitBasic(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s05_limit_subquery_scan.json")
	assertContains(t, sql, "LIMIT 10")
}

// PENDING §5 — subquery / CTE wrapped around a LIMITed scan
// Once LIMIT + subquery composition is supported, the planner should push the
// LIMIT into an inner subquery and apply the outer aggregation on top of it.
func TestLimitSubqueryScan(t *testing.T) {
	pendingTest(t, "5", "limit-subquery-scan",
		"LIMIT pushdown into subquery scan not yet implemented in the planner")
	_ = loadQueryRaw(t, "s05_limit_subquery_scan.json")
}
