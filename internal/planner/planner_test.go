package planner_test

import (
	"path/filepath"
	"testing"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// loadModel loads the shared e-commerce postgres fixture.
func loadModel(t *testing.T) *model.SemanticModel {
	t.Helper()
	doc, err := model.ParseFile(filepath.Join("..", "model", "testdata", "ecommerce_postgres.yaml"))
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	return &doc.Models[0]
}

// loadQuery parses a query fixture from the query package's testdata.
func loadQuery(t *testing.T, file string) *query.Query {
	t.Helper()
	q, err := query.ParseFile(filepath.Join("..", "query", "testdata", file))
	if err != nil {
		t.Fatalf("load query %s: %v", file, err)
	}
	return q
}

// findJoin walks the single-input chain of a plan looking for a JoinNode.
func findJoin(n planner.PlanNode) *planner.JoinNode {
	switch t := n.(type) {
	case *planner.JoinNode:
		return t
	case *planner.LimitNode:
		return findJoin(t.Input)
	case *planner.OrderNode:
		return findJoin(t.Input)
	case *planner.HavingNode:
		return findJoin(t.Input)
	case *planner.AggregateNode:
		return findJoin(t.Input)
	case *planner.FilterNode:
		return findJoin(t.Input)
	default:
		return nil
	}
}

// TestPlanShape asserts the exact node stack for representative queries.
func TestPlanShape(t *testing.T) {
	m := loadModel(t)

	cases := []struct {
		file string
		want string
	}{
		{
			file: "simple.json",
			want: "Limit(Aggregate(Scan(orders)))",
		},
		{
			file: "with_order_limit.json",
			want: "Limit(Order(Aggregate(Filter(Scan(orders)))))",
		},
		{
			file: "with_having.json",
			want: "Order(Having(Aggregate(Scan(orders))))",
		},
		{
			file: "cross_dataset.json",
			want: "Limit(Order(Having(Aggregate(Filter(Join(Scan(orders), Scan(customers)))))))",
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			q := loadQuery(t, tc.file)
			plan, err := planner.Plan(q, m)
			if err != nil {
				t.Fatalf("Plan(%s): unexpected error: %v", tc.file, err)
			}
			if got := planner.Describe(plan); got != tc.want {
				t.Errorf("plan shape:\n  want %s\n  got  %s", tc.want, got)
			}
		})
	}
}

// TestPlanJoinCondition checks the join is built with the correct on-condition
// derived from the OSI relationship.
func TestPlanJoinCondition(t *testing.T) {
	m := loadModel(t)
	q := loadQuery(t, "cross_dataset.json")

	plan, err := planner.Plan(q, m)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	jn := findJoin(plan)
	if jn == nil {
		t.Fatal("expected a JoinNode in the plan")
	}
	if jn.Kind != planner.LeftJoin {
		t.Errorf("join kind: want LEFT, got %q", jn.Kind)
	}
	if len(jn.On) != 1 {
		t.Fatalf("join conditions: want 1, got %d", len(jn.On))
	}
	c := jn.On[0]
	if c.Left.Dataset != "orders" || c.Left.Column != "customer_id" {
		t.Errorf("join left column: got %s.%s", c.Left.Dataset, c.Left.Column)
	}
	if c.Right.Dataset != "customers" || c.Right.Column != "customer_id" {
		t.Errorf("join right column: got %s.%s", c.Right.Dataset, c.Right.Column)
	}
	if jn.Relationship == nil || jn.Relationship.Name != "orders_to_customers" {
		t.Errorf("join relationship: got %+v", jn.Relationship)
	}
}

// TestPlanOrderByNormalisation checks the default ASC direction.
func TestPlanOrderByNormalisation(t *testing.T) {
	m := loadModel(t)
	q := &query.Query{
		Metrics: []string{"orders.revenue"},
		OrderBy: []query.OrderItem{{Field: "orders.revenue"}}, // no direction
	}

	plan, err := planner.Plan(q, m)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	on, ok := plan.(*planner.OrderNode)
	if !ok {
		t.Fatalf("top node: want *OrderNode, got %T", plan)
	}
	if on.Items[0].Direction != "ASC" {
		t.Errorf("empty direction should normalise to ASC, got %q", on.Items[0].Direction)
	}
}

// TestPlanNoRelationship checks that two unconnected datasets produce an error.
func TestPlanNoRelationship(t *testing.T) {
	ansi := func(e string) model.Expression {
		return model.Expression{Dialects: []model.DialectExpression{{Dialect: "ANSI_SQL", Expression: e}}}
	}
	m := &model.SemanticModel{
		Name: "disconnected",
		Datasets: []model.Dataset{
			{Name: "a", Source: "a", Fields: []model.Field{{Name: "x", Expression: ansi("x"), Dimension: &model.Dimension{}}}},
			{Name: "b", Source: "b", Fields: []model.Field{{Name: "y", Expression: ansi("y"), Dimension: &model.Dimension{}}}},
		},
		Metrics: []model.Metric{{Name: "cnt", Expression: ansi("COUNT(1)")}},
		// No relationships connecting a and b.
	}
	q := &query.Query{Metrics: []string{"a.cnt"}, Dimensions: []string{"b.y"}}

	_, err := planner.Plan(q, m)
	if err == nil {
		t.Fatal("want error for unconnected datasets, got nil")
	}
	if !containsSub(err.Error(), "no relationship") {
		t.Errorf("error %q should mention 'no relationship'", err.Error())
	}
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
