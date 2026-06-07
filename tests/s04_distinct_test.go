package tests

import "testing"

// §4 — DISTINCT / deduplication

// CAN TEST NOW: COUNT DISTINCT aggregate renders correctly
func TestCountDistinctAgg(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s04_count_distinct_agg.json")
	assertContains(t, sql, "COUNT(DISTINCT")
	assertContains(t, sql, "GROUP BY")
}

// CAN TEST NOW: multiple dimension columns render correctly (SELECT DISTINCT for dims-only query)
func TestDistinctMultiCol(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s04_distinct_multi_col.json")
	// dims-only query uses SELECT DISTINCT rather than GROUP BY
	assertContains(t, sql, "DISTINCT")
	assertContains(t, sql, "status")
	assertContains(t, sql, "region")
}

// CAN TEST NOW: dimension-only query with no explicit GROUP BY keyword check
func TestDistinctNoGroupby(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s04_distinct_no_groupby.json")
	// revenue requires an aggregation context
	assertContains(t, sql, "SUM(")
	assertContains(t, sql, "GROUP BY")
}

// PENDING §4 — DISTINCT below join deduplication
func TestDistinctBelowJoin(t *testing.T) {
	pendingTest(t, "4", "distinct-below-join", "DISTINCT-before-join (pre-deduplication) node not yet implemented")
	_ = loadQueryRaw(t, "s04_distinct_below_join.json")
}
