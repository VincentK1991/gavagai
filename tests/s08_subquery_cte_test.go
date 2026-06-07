package tests

import "testing"

// §8 — Subquery / CTE codegen (all PENDING: WITH / subquery emission not yet implemented)

func TestCTEBasic(t *testing.T) {
	pendingTest(t, "8", "cte-basic", "WITH clause (CTE) codegen not yet implemented")
	_ = loadQueryRaw(t, "s08_cte.json")
}

func TestInlineSubquery(t *testing.T) {
	pendingTest(t, "8", "inline-subquery", "inline subquery codegen not yet implemented")
	_ = loadQueryRaw(t, "s08_inline_subquery.json")
}

func TestNestedCTE(t *testing.T) {
	pendingTest(t, "8", "nested-cte", "nested CTE codegen not yet implemented")
	_ = loadQueryRaw(t, "s08_nested_cte.json")
}

func TestPushPredCTE(t *testing.T) {
	pendingTest(t, "8", "push-pred-cte", "predicate pushdown into CTE not yet implemented")
	_ = loadQueryRaw(t, "s08_push_pred_cte.json")
}

func TestRecursiveCTE(t *testing.T) {
	pendingTest(t, "8", "recursive-cte", "RECURSIVE CTE codegen not yet implemented")
	_ = loadQueryRaw(t, "s08_recursive_cte.json")
}
