package planner_test

import (
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// TestFanOut covers fan-out detection across join cardinalities. Each case is
// a query built against the shared e-commerce model; we assert whether Plan
// returns a fan-out error.
func TestFanOut(t *testing.T) {
	m := loadModel(t)

	cases := []struct {
		name        string
		query       *query.Query
		wantFanOut  bool
		explanation string
	}{
		{
			name: "additive metric on the one-side fans out",
			// revenue (SUM, sourced at orders) joined to order_items (many):
			// orders rows duplicate per line item -> double count.
			query: &query.Query{
				Metrics: []string{"orders.revenue", "order_items.total_items_sold"},
			},
			wantFanOut:  true,
			explanation: "SUM(orders.amount) duplicated across order_items join",
		},
		{
			name: "avg metric on the one-side fans out",
			query: &query.Query{
				Metrics: []string{"orders.avg_order_value", "order_items.total_items_sold"},
			},
			wantFanOut:  true,
			explanation: "AVG is not robust to row duplication",
		},
		{
			name: "count distinct on the one-side is safe",
			// order_count = COUNT(DISTINCT orders.order_id): immune to fan-out.
			query: &query.Query{
				Metrics: []string{"orders.order_count", "order_items.total_items_sold"},
			},
			wantFanOut:  false,
			explanation: "COUNT(DISTINCT order_id) dedupes duplicated rows",
		},
		{
			name: "metric on the many-side is safe",
			// total_items_sold sourced at order_items (many side); joining up to
			// orders (one side) does not duplicate item rows.
			query: &query.Query{
				Metrics:    []string{"order_items.total_items_sold"},
				Dimensions: []string{"orders.region"},
			},
			wantFanOut:  false,
			explanation: "many-to-one join does not duplicate the many side",
		},
		{
			name: "many-to-one dimension join is safe",
			// revenue sourced at orders joined to customers (one side): orders
			// is the many side, so its rows are not duplicated.
			query: &query.Query{
				Metrics:    []string{"orders.revenue"},
				Dimensions: []string{"customers.region"},
			},
			wantFanOut:  false,
			explanation: "orders is the many side of orders_to_customers",
		},
		{
			name: "single dataset never fans out",
			query: &query.Query{
				Metrics:    []string{"orders.revenue"},
				Dimensions: []string{"orders.region"},
			},
			wantFanOut:  false,
			explanation: "no joins at all",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planner.Plan(tc.query, m)

			if tc.wantFanOut {
				if err == nil {
					t.Fatalf("want fan-out error (%s), got nil", tc.explanation)
				}
				if !strings.Contains(err.Error(), "fan-out") {
					t.Errorf("error %q should contain 'fan-out' (%s)", err.Error(), tc.explanation)
				}
				var fe *planner.FanOutError
				if !asFanOut(err, &fe) {
					t.Errorf("error should be *FanOutError, got %T", err)
				}
			} else {
				if err != nil {
					t.Errorf("want no fan-out error (%s), got %v", tc.explanation, err)
				}
			}
		})
	}
}

// asFanOut reports whether err is a *planner.FanOutError, storing it in target.
func asFanOut(err error, target **planner.FanOutError) bool {
	fe, ok := err.(*planner.FanOutError)
	if ok {
		*target = fe
	}
	return ok
}
