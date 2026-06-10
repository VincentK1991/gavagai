package tests

import "testing"

// §9 — NULL handling

// §9 CAN TEST NOW: COALESCE expression in GROUP BY renders correctly
func TestCoalesceGroupby(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s09_coalesce_groupby.json")
	assertContains(t, sql, "COALESCE(")
	assertContains(t, sql, "GROUP BY")
}

// §9 — null-safe equality filter: native IS NOT DISTINCT FROM on PostgreSQL,
// expanded (a = b OR (a IS NULL AND b IS NULL)) on BigQuery.
func TestNullSafeEq(t *testing.T) {
	pgSQL := compileSQLDialect(t, "ecommerce.yaml", "s09_null_safe_eq.json", "postgres")
	assertContains(t, pgSQL, "status IS NOT DISTINCT FROM 'complete'")

	bqSQL := compileSQLDialect(t, "ecommerce.yaml", "s09_null_safe_eq.json", "bigquery")
	assertContains(t, bqSQL, "(status = 'complete' OR (status IS NULL AND 'complete' IS NULL))")
}

// §9 — anti-join via the NULL-generating outer join: customers with no orders
// get NULL from the LEFT JOIN to the grouped subquery, and the null-safe
// COALESCE(metric, 0) = 0 check realizes the `right.id IS NULL` pattern.
func TestAntiJoinNull(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s09_anti_join_null.json")
	assertContains(t, sql, "LEFT JOIN (")
	assertContains(t, sql, `COALESCE("mf0_order_count"."order_count", 0) = 0`)
}
