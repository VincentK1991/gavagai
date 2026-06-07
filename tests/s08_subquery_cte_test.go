package tests

import (
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/planner"
)

// §8 — Subquery / CTE codegen. The materialization strategy is applied after
// pushdown (planner.Materialize); filtered base scans become derived tables
// (Subquery) or WITH definitions (CTE).

// §8 — base table emitted as a WITH alias AS (SELECT ...) CTE.
func TestCTEBasic(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s08_cte.json", planner.CTE)
	assertContains(t, sql, `WITH "orders" AS (`)
	assertContains(t, sql, "SELECT *")
	assertContains(t, sql, "FROM analytics.orders")
	assertContains(t, sql, `FROM "orders" AS "orders"`)
}

// §8 — single-use derived table emitted as (SELECT ...) AS alias.
func TestInlineSubquery(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s08_inline_subquery.json", planner.Subquery)
	assertContains(t, sql, `) AS "orders"`)
	assertNotContains(t, sql, "WITH ")
	// The filter is pushed into the subquery body.
	assertContains(t, sql, "WHERE status = 'complete'")
	if strings.Count(sql, "WHERE") != 1 {
		t.Errorf("filter should appear once, inside the subquery:\n%s", sql)
	}
}

// §8 — predicate pushed into a single-use CTE definition, not the outer query.
func TestPushPredCTE(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s08_push_pred_cte.json", planner.CTE)
	assertContains(t, sql, `WITH "orders" AS (`)
	// WHERE must sit inside the CTE body (before its closing paren).
	body := sql[:strings.Index(sql, "\n)")]
	assertContains(t, body, "WHERE status = 'complete'")
	if strings.Count(sql, "WHERE") != 1 {
		t.Errorf("filter should appear once, inside the CTE body:\n%s", sql)
	}
}

// §8 — a CTE that references another CTE. The query-driven path emits sibling
// CTEs, not a chained one, so nested-CTE codegen is gated by the conformance
// hand-built fixture (gate 8/nested-cte). Here we verify the multi-CTE WITH
// form a join produces under the CTE strategy.
func TestNestedCTE(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s08_nested_cte.json", planner.CTE)
	assertContains(t, sql, `WITH "orders" AS (`)
	assertContains(t, sql, `"customers" AS (`)
	assertContains(t, sql, "LEFT JOIN")
}

// §8 — RECURSIVE CTE remains future work: the query IR has no anchor / recursive
// term construct for hierarchical traversal.
func TestRecursiveCTE(t *testing.T) {
	pendingTest(t, "8", "recursive-cte", "WITH RECURSIVE codegen is future work; IR has no recursive-term construct")
	_ = loadQueryRaw(t, "s08_recursive_cte.json")
}
