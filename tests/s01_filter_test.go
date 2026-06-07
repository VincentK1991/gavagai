package tests

import (
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/planner"
)

// §1 — Filter / HAVING behaviour

// §1.4 — filter pushed into a CTE definition (referenced once).
func TestCTEFilter(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s01_cte_filter.json", planner.CTE)
	assertContains(t, sql, `WITH "orders" AS (`)
	body := sql[:strings.Index(sql, "\n)")]
	assertContains(t, body, "WHERE status = 'complete'")
}

// §1.4 — filter pushed into an inline subquery body, not the outer query.
func TestSubqueryFilter(t *testing.T) {
	sql := compileSQLStrategy(t, "ecommerce.yaml", "s01_subquery_filter.json", planner.Subquery)
	assertContains(t, sql, `) AS "orders"`)
	assertContains(t, sql, "WHERE status = 'complete'")
	if strings.Count(sql, "WHERE") != 1 {
		t.Errorf("filter should appear once, inside the subquery:\n%s", sql)
	}
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
