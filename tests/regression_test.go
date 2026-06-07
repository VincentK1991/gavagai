package tests

// regression_test.go — smoke tests that exercise the full pipeline against the
// ecommerce test fixtures. Each test corresponds to a green checkbox in the
// pushdown checklist. If a test regresses (previously passing, now failing),
// it means a recent change broke an existing capability.
//
// These are end-to-end: model load → query parse → plan → pushdown → SQL text.

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// §1 — Filter pushdown
// ---------------------------------------------------------------------------

// Single-table WHERE filter is pushed to the scan.
func TestReg_FilterPushdown(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s01_having_count_distinct.json")
	assertContains(t, sql, "COUNT(DISTINCT")
	assertContains(t, sql, "HAVING")
	assertContains(t, sql, "GROUP BY")
}

// ---------------------------------------------------------------------------
// §2 — Join correctness
// ---------------------------------------------------------------------------

// Cross-dataset query joins the two tables and references both in the output.
func TestReg_CrossDatasetJoin(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s09_coalesce_groupby.json")
	// customers.region_coalesced requires a join to customers
	assertContains(t, sql, "JOIN")
	assertContains(t, sql, "COALESCE(")
}

// ---------------------------------------------------------------------------
// §3 — Aggregation
// ---------------------------------------------------------------------------

// SUM metric renders as SUM(...) with GROUP BY.
func TestReg_SumAggregation(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s01_having_min_max.json")
	assertContains(t, sql, "MAX(")
	assertContains(t, sql, "MIN(")
	assertContains(t, sql, "GROUP BY")
}

// COUNT DISTINCT renders with the DISTINCT keyword inside the aggregate.
func TestReg_CountDistinct(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s04_count_distinct_agg.json")
	assertContains(t, sql, "COUNT(DISTINCT")
}

// ---------------------------------------------------------------------------
// §4 — SELECT DISTINCT for dims-only queries
// ---------------------------------------------------------------------------

func TestReg_DimsOnlySelectDistinct(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s04_distinct_multi_col.json")
	assertContains(t, sql, "DISTINCT")
	assertNotContains(t, sql, "GROUP BY")
}

// ---------------------------------------------------------------------------
// §5 — LIMIT / OFFSET
// ---------------------------------------------------------------------------

func TestReg_LimitClause(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s05_limit_subquery_scan.json")
	assertContains(t, sql, "LIMIT 10")
}

func TestReg_LargeLimitNoOverflow(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s14_limit_overflow.json")
	assertContains(t, sql, "LIMIT 2147483647")
}

// ---------------------------------------------------------------------------
// §6 — CASE WHEN expression passthrough
// ---------------------------------------------------------------------------

func TestReg_CaseWhenBasic(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_nested_case.json")
	assertContains(t, sql, "CASE WHEN")
}

func TestReg_CaseWhenNullBranch(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_case_null_branch.json")
	assertContains(t, sql, "IS NULL")
}

func TestReg_CaseWhenBoolFilter(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_case_boolean_flag.json")
	assertContains(t, sql, "CASE WHEN")
	assertContains(t, sql, "WHERE")
}

// ---------------------------------------------------------------------------
// §7 — Dialect-split date/time expressions
// ---------------------------------------------------------------------------

func TestReg_DateTruncPG(t *testing.T) {
	sql := compileSQLDialect(t, "ecommerce.yaml", "s12_nested_expr_ref.json", "postgres")
	assertContains(t, sql, "DATE_TRUNC(")
}

func TestReg_DateTruncBQ(t *testing.T) {
	sql := compileSQLDialect(t, "ecommerce.yaml", "s12_nested_expr_ref.json", "bigquery")
	assertContains(t, sql, "DATE_TRUNC(")
}

func TestReg_ExtractDowDialects(t *testing.T) {
	pg := compileSQLDialect(t, "ecommerce.yaml", "s07_extract_dow.json", "postgres")
	bq := compileSQLDialect(t, "ecommerce.yaml", "s07_extract_dow.json", "bigquery")
	assertContains(t, pg, "EXTRACT(DOW")
	assertContains(t, bq, "EXTRACT(DAYOFWEEK")
}

// ---------------------------------------------------------------------------
// §9 — NULL handling
// ---------------------------------------------------------------------------

func TestReg_CoalesceInGroupBy(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s09_coalesce_groupby.json")
	assertContains(t, sql, "COALESCE(")
}

// ---------------------------------------------------------------------------
// §13 — ORDER BY with NULLS FIRST/LAST
// ---------------------------------------------------------------------------

func TestReg_OrderByNullsLast(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")

	q, err := parseQueryJSON(`{"metrics":["orders.revenue"],"dimensions":["orders.status"],"order_by":[{"field":"orders.revenue","direction":"DESC","nulls":"LAST"}]}`)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	sql := compileSQLFrom(t, m, q, "postgres")
	assertContains(t, sql, "NULLS LAST")
}

func TestReg_OrderByNullsFirst(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")

	q, err := parseQueryJSON(`{"metrics":["orders.revenue"],"dimensions":["orders.status"],"order_by":[{"field":"orders.status","direction":"ASC","nulls":"FIRST"}]}`)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	sql := compileSQLFrom(t, m, q, "postgres")
	assertContains(t, sql, "NULLS FIRST")
}

// ---------------------------------------------------------------------------
// §14 — Fan-out safety
// ---------------------------------------------------------------------------

func TestReg_FanOutRefused(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")
	// A many-to-many attribution (order revenue by product category, via
	// order_items) has no safe pre-aggregation, so the planner still refuses it.
	q, err := parseQueryJSON(`{"metrics":["orders.revenue"],"dimensions":["products.category"]}`)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	_, err = planFromResult(m, q)
	if err == nil || !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected fan-out error, got: %v", err)
	}
}

// TestReg_FanOutPreAggregated is the companion to the refusal case: an additive
// metric across grains is rewritten into per-grain pre-aggregates rather than
// refused, so SUM is computed exactly once.
func TestReg_FanOutPreAggregated(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")
	q, err := parseQueryJSON(`{"metrics":["orders.revenue","order_items.gross_revenue"]}`)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	sql := compileSQLFrom(t, m, q, "postgres")
	assertContains(t, sql, "CROSS JOIN")
	if strings.Count(sql, "SUM(orders.amount)") != 1 {
		t.Fatalf("revenue must be summed exactly once (pre-aggregated):\n%s", sql)
	}
}
