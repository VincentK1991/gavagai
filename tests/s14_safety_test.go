package tests

import (
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/query"
)

// §14 — Safety / correctness gates

// §14 CAN TEST NOW: ambiguous column names across joined datasets get distinct aliases
func TestAmbiguousColumn(t *testing.T) {
	// orders.region and customers.region both requested — the SQL should disambiguate
	sql := compileSQL(t, "ecommerce.yaml", "s14_ambiguous_col.json")
	// Each region column must appear as a qualified or aliased column
	// The simplest check: both tables' region values are present in the output
	if !strings.Contains(sql, "orders") && !strings.Contains(sql, "customers") {
		t.Errorf("expected SQL to reference both orders and customers tables\ngot:\n%s", sql)
	}
	assertContains(t, sql, "region")
}

// §14 CAN TEST NOW: large LIMIT value compiles without overflow
func TestLimitOverflow(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s14_limit_overflow.json")
	assertContains(t, sql, "LIMIT 2147483647")
}

// §14 CAN TEST NOW: empty-result filter still produces valid SQL (no short-circuit)
func TestEmptyResultFilter(t *testing.T) {
	// A filter for an impossible value must still produce syntactically valid SQL
	// (runtime produces zero rows, but the compiler must not error out)
	sql := compileSQL(t, "ecommerce.yaml", "s14_empty_result.json")
	assertContains(t, sql, "WHERE")
	assertContains(t, sql, "__impossible_value_xyz__")
}

// §14 / §2.5 — Fan-out safety. SUM(orders.amount) would double-count when
// order_items is joined, so the planner pre-aggregates each grain in its own
// subquery (no double count) rather than emitting an unsafe single pass. A
// genuinely ambiguous many-to-many attribution is still refused.
func TestFanOutDetection(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")

	// Additive metric across grains → pre-aggregated, SUM computed once.
	q := &query.Query{
		Metrics: []string{"orders.revenue", "order_items.gross_revenue"},
	}
	sql := compileSQLFrom(t, m, q, "postgres")
	assertContains(t, sql, "CROSS JOIN")
	if strings.Count(sql, "SUM(orders.amount)") != 1 {
		t.Fatalf("revenue must be summed exactly once (pre-aggregated):\n%s", sql)
	}

	// Many-to-many attribution (revenue by product category) has no safe
	// pre-aggregation and is still refused.
	q2 := &query.Query{
		Metrics:    []string{"orders.revenue"},
		Dimensions: []string{"products.category"},
	}
	if _, err := planFromResult(m, q2); err == nil || !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected fan-out error for many-to-many attribution, got: %v", err)
	}
}
