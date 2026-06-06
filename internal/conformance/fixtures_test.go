package conformance

import (
	"encoding/json"
	"testing"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// pending marks a checklist gate that cannot pass yet: either the rewrite /
// emitter is unimplemented, or the query IR cannot express the case. Delete
// the pending() call (and fill in the assertion) when the box is implemented.
func pending(t *testing.T, item, reason string) {
	t.Helper()
	t.Skipf("PENDING [%s]: %s", item, reason)
}

// raw is a terse json.RawMessage constructor for filter values.
func raw(s string) json.RawMessage { return json.RawMessage(s) }

// intp returns a pointer to n, for Query.Limit.
func intp(n int) *int { return &n }

// --- expression builders ---------------------------------------------------

// ax builds an ANSI_SQL-only expression.
func ax(expr string) model.Expression {
	return model.Expression{Dialects: []model.DialectExpression{{Dialect: "ANSI_SQL", Expression: expr}}}
}

// dx builds a multi-dialect expression from {dialect, expression} pairs.
func dx(pairs ...[2]string) model.Expression {
	ds := make([]model.DialectExpression, 0, len(pairs))
	for _, p := range pairs {
		ds = append(ds, model.DialectExpression{Dialect: p[0], Expression: p[1]})
	}
	return model.Expression{Dialects: ds}
}

// fld builds a field with an optional dimension annotation.
func fld(name string, e model.Expression, dim *model.Dimension) model.Field {
	return model.Field{Name: name, Expression: e, Dimension: dim}
}

func dim() *model.Dimension     { return &model.Dimension{} }
func timeDim() *model.Dimension { return &model.Dimension{IsTime: true} }

// --- the shared e-commerce model -------------------------------------------

// ecommerceModel builds the fixture used by most gates. Grain chain:
//
//	order_items (many) -> orders (one) -> customers (one)
//	order_items (many) -> products (one)
//
// orders sits on the "one" side of order_items_to_orders, so an unsafe
// aggregate over orders fans out when order_items is joined in.
func ecommerceModel(t *testing.T) *model.SemanticModel {
	t.Helper()
	return &model.SemanticModel{
		Name: "ecommerce",
		Datasets: []model.Dataset{
			{
				Name: "customers", Source: "analytics.customers",
				PrimaryKey: []string{"customer_id"},
				Fields: []model.Field{
					fld("customer_id", ax("customer_id"), dim()),
					fld("region", ax("region"), dim()),
				},
			},
			{
				Name: "orders", Source: "analytics.orders",
				PrimaryKey: []string{"order_id"},
				Fields: []model.Field{
					fld("order_id", ax("order_id"), dim()),
					fld("customer_id", ax("customer_id"), nil),
					fld("amount", ax("amount"), nil),
					// status carries ONLY ANSI_SQL -> exercises the dialect fallback.
					fld("status", ax("status"), dim()),
					fld("created_at", ax("created_at"), timeDim()),
					// order_month differs per dialect (DATE_TRUNC argument order).
					fld("order_month", dx(
						[2]string{"ANSI_SQL", "DATE_TRUNC('month', created_at)"},
						[2]string{"POSTGRES", "DATE_TRUNC('month', created_at)"},
						[2]string{"BIGQUERY", "DATE_TRUNC(created_at, MONTH)"},
					), timeDim()),
					// status_label is a CASE WHEN dimension.
					fld("status_label", ax("CASE WHEN status = 'complete' THEN 'done' ELSE 'pending' END"), dim()),
					// broken has no ANSI_SQL and no target dialect -> missing-expression error.
					fld("broken", dx([2]string{"SNOWFLAKE", "broken"}), dim()),
				},
			},
			{
				Name: "order_items", Source: "analytics.order_items",
				Fields: []model.Field{
					fld("order_id", ax("order_id"), nil),
					fld("product_id", ax("product_id"), nil),
					fld("quantity", ax("quantity"), nil),
				},
			},
			{
				Name: "products", Source: "analytics.products",
				PrimaryKey: []string{"product_id"},
				Fields: []model.Field{
					fld("product_id", ax("product_id"), dim()),
					fld("category", ax("category"), dim()),
				},
			},
		},
		Relationships: []model.Relationship{
			{Name: "orders_to_customers", From: "orders", To: "customers", FromColumns: []string{"customer_id"}, ToColumns: []string{"customer_id"}},
			{Name: "order_items_to_orders", From: "order_items", To: "orders", FromColumns: []string{"order_id"}, ToColumns: []string{"order_id"}},
			{Name: "order_items_to_products", From: "order_items", To: "products", FromColumns: []string{"product_id"}, ToColumns: []string{"product_id"}},
		},
		Metrics: []model.Metric{
			{Name: "revenue", Expression: ax("SUM(amount)")},                                                         // fan-out unsafe
			{Name: "order_count", Expression: ax("COUNT(DISTINCT order_id)")},                                        // fan-out safe
			{Name: "item_count", Expression: ax("COUNT(*)")},                                                         // fan-out unsafe
			{Name: "aov", Expression: ax("AVG(amount)")},                                                             // fan-out unsafe
			{Name: "max_amount", Expression: ax("MAX(amount)")},                                                      // fan-out safe
			{Name: "completed_revenue", Expression: ax("SUM(CASE WHEN status = 'complete' THEN amount ELSE 0 END)")}, // CASE in metric
		},
	}
}

// compositeKeyModel has a single relationship with a two-column key, for the
// composite-join-condition gate. a (many) -> b (one).
func compositeKeyModel() *model.SemanticModel {
	return &model.SemanticModel{
		Name: "composite",
		Datasets: []model.Dataset{
			{Name: "a", Source: "s.a", Fields: []model.Field{
				fld("k1", ax("k1"), dim()), fld("k2", ax("k2"), dim()),
			}},
			{Name: "b", Source: "s.b", Fields: []model.Field{
				fld("k1", ax("k1"), dim()), fld("k2", ax("k2"), dim()),
			}},
		},
		Relationships: []model.Relationship{
			{Name: "a_to_b", From: "a", To: "b", FromColumns: []string{"k1", "k2"}, ToColumns: []string{"k1", "k2"}},
		},
		Metrics: []model.Metric{{Name: "cnt", Expression: ax("COUNT(*)")}},
	}
}

// disconnectedModel has two datasets and no relationship between them.
func disconnectedModel() *model.SemanticModel {
	return &model.SemanticModel{
		Name: "disconnected",
		Datasets: []model.Dataset{
			{Name: "a", Source: "s.a", Fields: []model.Field{fld("x", ax("x"), dim())}},
			{Name: "b", Source: "s.b", Fields: []model.Field{fld("y", ax("y"), dim())}},
		},
		Metrics: []model.Metric{{Name: "cnt", Expression: ax("COUNT(*)")}},
	}
}

// cyclicModel has a relationship cycle a -> b -> c -> a to prove BFS join
// resolution terminates. The metric is fan-out safe so any tree is valid.
func cyclicModel() *model.SemanticModel {
	d := func(name string) model.Dataset {
		return model.Dataset{Name: name, Source: "s." + name, Fields: []model.Field{
			fld("id", ax("id"), nil),
			fld(map[string]string{"a": "x", "b": "x", "c": "y"}[name], ax("col"), dim()),
		}}
	}
	return &model.SemanticModel{
		Name:     "cyclic",
		Datasets: []model.Dataset{d("a"), d("b"), d("c")},
		Relationships: []model.Relationship{
			{Name: "a_b", From: "a", To: "b", FromColumns: []string{"id"}, ToColumns: []string{"id"}},
			{Name: "b_c", From: "b", To: "c", FromColumns: []string{"id"}, ToColumns: []string{"id"}},
			{Name: "c_a", From: "c", To: "a", FromColumns: []string{"id"}, ToColumns: []string{"id"}},
		},
		Metrics: []model.Metric{{Name: "cnt", Expression: ax("COUNT(DISTINCT id)")}},
	}
}

// --- plan helpers ----------------------------------------------------------

// mustPlan plans q against the e-commerce model and fails on error.
func mustPlan(t *testing.T, q *query.Query) planner.PlanNode {
	t.Helper()
	p, err := planner.Plan(q, ecommerceModel(t))
	if err != nil {
		t.Fatalf("Plan: unexpected error: %v", err)
	}
	return planner.PushDown(p)
}

// planErr plans q and returns the error (or nil).
func planErr(t *testing.T, q *query.Query) error {
	t.Helper()
	_, err := planner.Plan(q, ecommerceModel(t))
	return err
}

// --- plan tree walking -----------------------------------------------------

// walk visits every node in the plan tree depth-first.
func walk(n planner.PlanNode, fn func(planner.PlanNode)) {
	if n == nil {
		return
	}
	fn(n)
	switch t := n.(type) {
	case *planner.JoinNode:
		walk(t.Left, fn)
		walk(t.Right, fn)
	case *planner.FilterNode:
		walk(t.Input, fn)
	case *planner.AggregateNode:
		walk(t.Input, fn)
	case *planner.HavingNode:
		walk(t.Input, fn)
	case *planner.OrderNode:
		walk(t.Input, fn)
	case *planner.LimitNode:
		walk(t.Input, fn)
	case *planner.ScanNode:
		// leaf
	}
}

// nodesOf returns every node of type T in the plan tree.
func nodesOf[T planner.PlanNode](root planner.PlanNode) []T {
	var out []T
	walk(root, func(n planner.PlanNode) {
		if v, ok := n.(T); ok {
			out = append(out, v)
		}
	})
	return out
}

// scanAliases returns the set of dataset aliases scanned by the plan.
func scanAliases(root planner.PlanNode) map[string]bool {
	out := map[string]bool{}
	for _, s := range nodesOf[*planner.ScanNode](root) {
		out[s.Alias] = true
	}
	return out
}
