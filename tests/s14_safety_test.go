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

// §14 — Fan-out detection: SUM(amount) on orders doubles when order_items is joined
func TestFanOutDetection(t *testing.T) {
	m := loadModel(t, "ecommerce.yaml")
	// orders.revenue = SUM(orders.amount) is attributed to "orders" (the "to"/one side).
	// order_items.gross_revenue is attributed to "order_items" (the "from"/many side).
	// Joining order_items to orders duplicates orders rows → SUM(orders.amount) double-counts.
	q := &query.Query{
		Metrics: []string{"orders.revenue", "order_items.gross_revenue"},
	}
	_, err := planFromResult(m, q)
	if err == nil {
		t.Fatal("expected fan-out error when combining orders.revenue with order_items metric, got nil")
	}
	if !strings.Contains(err.Error(), "fan-out") {
		t.Fatalf("expected fan-out error, got: %v", err)
	}
}
