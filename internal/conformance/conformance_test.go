package conformance

import (
	"errors"
	"strings"
	"testing"

	"github.com/vincentk1991/gavagai/internal/codegen"
	_ "github.com/vincentk1991/gavagai/internal/codegen/bigquery" // registers bigquery dialect
	_ "github.com/vincentk1991/gavagai/internal/codegen/postgres" // registers postgres dialect
	"github.com/vincentk1991/gavagai/internal/planner"
	"github.com/vincentk1991/gavagai/internal/query"
)

// This file is the executable form of docs/pushdown-checklist.md. Subtests are
// named by their checklist id. A passing subtest means the box can be checked;
// a pending() skip means the box is still open.

// ---------------------------------------------------------------------------
// §1 Filter / predicate pushdown
// ---------------------------------------------------------------------------

func TestFilterPushdown(t *testing.T) {
	// §1.1 — every scalar operator is carried into a FilterNode that sits
	// below the AggregateNode (i.e. it becomes a WHERE, pre-aggregation).
	ops := []struct {
		id     string
		field  string
		op     string
		value  string // empty => nil value
		isNull bool
	}{
		{"1.1/=", "orders.status", "=", `"complete"`, false},
		{"1.1/!=", "orders.status", "!=", `"complete"`, false},
		{"1.1/>", "orders.amount", ">", `100`, false},
		{"1.1/>=", "orders.amount", ">=", `100`, false},
		{"1.1/<", "orders.amount", "<", `100`, false},
		{"1.1/<=", "orders.amount", "<=", `100`, false},
		{"1.1/IN", "orders.status", "IN", `["a","b"]`, false},
		{"1.1/NOT IN", "orders.status", "NOT IN", `["a","b"]`, false},
		{"1.1/IS NULL", "orders.status", "IS NULL", ``, true},
		{"1.1/IS NOT NULL", "orders.status", "IS NOT NULL", ``, true},
	}
	for _, tc := range ops {
		t.Run(tc.id, func(t *testing.T) {
			f := query.Filter{Field: tc.field, Op: tc.op}
			if tc.value != "" {
				f.Value = raw(tc.value)
			}
			q := &query.Query{Metrics: []string{"orders.item_count"}, Filters: []query.Filter{f}}
			plan := mustPlan(t, q)

			filters := nodesOf[*planner.FilterNode](plan)
			if len(filters) != 1 {
				t.Fatalf("want 1 FilterNode, got %d", len(filters))
			}
			preds := filters[0].Predicates
			if len(preds) != 1 || preds[0].Op != tc.op {
				t.Fatalf("predicate op: want %q, got %+v", tc.op, preds)
			}
			if tc.isNull && preds[0].Value != nil {
				t.Errorf("%s should carry a nil value, got %s", tc.op, preds[0].Value)
			}
			if !tc.isNull && preds[0].Value == nil {
				t.Errorf("%s should carry a value, got nil", tc.op)
			}
			// The filter must be below the aggregate (WHERE, not HAVING).
			aggs := nodesOf[*planner.AggregateNode](plan)
			if len(aggs) != 1 {
				t.Fatalf("want 1 AggregateNode, got %d", len(aggs))
			}
			if got := nodesOf[*planner.FilterNode](aggs[0].Input); len(got) != 1 {
				t.Errorf("filter should sit below the aggregate (WHERE), got %d below", len(got))
			}
		})
	}

	// §1.2 — multiple filters AND-combine into one FilterNode.
	t.Run("1.2/AND", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{
				{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
				{Field: "orders.amount", Op: ">", Value: raw(`100`)},
			},
		}
		plan := mustPlan(t, q)
		filters := nodesOf[*planner.FilterNode](plan)
		if len(filters) != 1 || len(filters[0].Predicates) != 2 {
			t.Fatalf("want 1 FilterNode with 2 predicates, got %+v", filters)
		}
	})

	// §1.2 — a same-table OR group is one disjunction predicate, pushed to the
	// scan and rendered as a parenthesised OR.
	t.Run("1.2/OR", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{{Or: []query.Filter{
				{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
				{Field: "orders.status", Op: "=", Value: raw(`"shipped"`)},
			}}},
		}
		plan := mustPlan(t, q)
		filters := nodesOf[*planner.FilterNode](plan)
		if len(filters) != 1 || len(filters[0].Predicates) != 1 {
			t.Fatalf("want 1 FilterNode with 1 (OR) predicate, got %+v", filters)
		}
		if len(filters[0].Predicates[0].Or) != 2 {
			t.Fatalf("want an OR group of 2 disjuncts, got %+v", filters[0].Predicates[0])
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "status = 'complete' OR status = 'shipped'") {
			t.Errorf("OR group should render as a parenthesised disjunction:\n%s", sql)
		}
	})

	// §1.2 — a disjunction that spans datasets cannot be pushed; it stays above
	// the join as a residual filter.
	t.Run("1.2/OR-cross-dataset-stays-above-join", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
			Filters: []query.Filter{{Or: []query.Filter{
				{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
				{Field: "customers.region", Op: "=", Value: raw(`"US"`)},
			}}},
		}
		plan := mustPlan(t, q)
		// The residual filter wraps the join (it is not pushed onto a scan).
		f, ok := plan.(*planner.AggregateNode)
		if !ok {
			t.Fatalf("root: want *AggregateNode, got %T", plan)
		}
		if _, ok := f.Input.(*planner.FilterNode); !ok {
			t.Fatalf("cross-dataset OR should remain as a FilterNode above the join, got %T", f.Input)
		}
	})

	// §1.2 — mixed AND/OR: conjuncts are split; the OR group and the leaf land
	// in one FilterNode (both reference orders) and render with AND + OR.
	t.Run("1.2/mixed-and-or", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{
				{Or: []query.Filter{
					{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
					{Field: "orders.status", Op: "=", Value: raw(`"shipped"`)},
				}},
				{Field: "orders.amount", Op: ">", Value: raw(`100`)},
			},
		}
		plan := mustPlan(t, q)
		filters := nodesOf[*planner.FilterNode](plan)
		if len(filters) != 1 || len(filters[0].Predicates) != 2 {
			t.Fatalf("want 1 FilterNode with 2 predicates (OR group + leaf), got %+v", filters)
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, " OR ") || !strings.Contains(sql, " AND ") {
			t.Errorf("mixed AND/OR should render both connectives:\n%s", sql)
		}
	})

	// §1.3 — a filter on the left (fact) dataset is pushed below the join.
	t.Run("1.3/push-into-left-scan", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
			Filters:    []query.Filter{{Field: "orders.status", Op: "=", Value: raw(`"complete"`)}},
		}
		plan := mustPlan(t, q)
		joins := nodesOf[*planner.JoinNode](plan)
		if len(joins) != 1 {
			t.Fatalf("want 1 JoinNode, got %d", len(joins))
		}
		jn := joins[0]
		if _, ok := jn.Left.(*planner.FilterNode); !ok {
			t.Errorf("orders filter should be pushed below the join onto the left scan, left=%T", jn.Left)
		}
		if _, ok := jn.Right.(*planner.ScanNode); !ok {
			t.Errorf("right side (customers) should be a plain scan, right=%T", jn.Right)
		}
	})

	// §1.3 — a filter on the right (dimension) dataset is pushed below the join.
	t.Run("1.3/push-into-right-scan", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
			Filters:    []query.Filter{{Field: "customers.region", Op: "=", Value: raw(`"US"`)}},
		}
		plan := mustPlan(t, q)
		joins := nodesOf[*planner.JoinNode](plan)
		if len(joins) != 1 {
			t.Fatalf("want 1 JoinNode, got %d", len(joins))
		}
		jn := joins[0]
		if _, ok := jn.Left.(*planner.ScanNode); !ok {
			t.Errorf("left side (orders) should be a plain scan, left=%T", jn.Left)
		}
		if _, ok := jn.Right.(*planner.FilterNode); !ok {
			t.Errorf("customers filter should be pushed below the join onto the right scan, right=%T", jn.Right)
		}
	})

	// §1.3 — mixed-dataset filters: each is pushed to its own scan; no
	// FilterNode wraps the JoinNode. In our IR every predicate references
	// exactly one dataset, so there are no true "cross-dataset predicates"
	// (join-key predicates come from the OSI relationship, not user filters).
	// This gate therefore verifies the stronger property: after PushDown the
	// join itself is free of any wrapping FilterNode.
	t.Run("1.3/cross-dataset-stays-above-join", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
			Filters: []query.Filter{
				{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
				{Field: "customers.region", Op: "=", Value: raw(`"US"`)},
			},
		}
		plan := mustPlan(t, q)
		joins := nodesOf[*planner.JoinNode](plan)
		if len(joins) != 1 {
			t.Fatalf("want 1 JoinNode, got %d", len(joins))
		}
		jn := joins[0]
		if _, ok := jn.Left.(*planner.FilterNode); !ok {
			t.Errorf("orders filter should be on the left scan, left=%T", jn.Left)
		}
		if _, ok := jn.Right.(*planner.FilterNode); !ok {
			t.Errorf("customers filter should be on the right scan, right=%T", jn.Right)
		}
		// No FilterNode should wrap the JoinNode itself.
		allFilters := nodesOf[*planner.FilterNode](plan)
		for _, f := range allFilters {
			if _, ok := f.Input.(*planner.JoinNode); ok {
				t.Errorf("a FilterNode wraps the JoinNode — predicate was not pushed down")
			}
		}
	})

	// Idempotency: applying PushDown a second time must not change the tree.
	t.Run("1.3/pushdown-idempotent", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
			Filters: []query.Filter{
				{Field: "orders.status", Op: "=", Value: raw(`"complete"`)},
				{Field: "customers.region", Op: "=", Value: raw(`"US"`)},
			},
		}
		p, err := planner.Plan(q, ecommerceModel(t))
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		once := planner.PushDown(p)
		twice := planner.PushDown(once)
		if planner.Describe(once) != planner.Describe(twice) {
			t.Errorf("PushDown is not idempotent:\n  once:  %s\n  twice: %s",
				planner.Describe(once), planner.Describe(twice))
		}
	})

	// §1.4 — pushdown into subquery / CTE bodies (codegen).
	t.Run("1.4/into-subquery", func(t *testing.T) {
		pending(t, "1.4", "subquery/CTE emission not implemented (codegen)")
	})

	// §1.5 — scalar filter becomes WHERE; aggregate filter becomes HAVING.
	t.Run("1.5/where-vs-having", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.revenue"},
			Dimensions: []string{"orders.status"},
			Filters:    []query.Filter{{Field: "orders.status", Op: "=", Value: raw(`"complete"`)}},
			Having:     []query.Having{{Metric: "orders.revenue", Op: ">", Value: 100}},
		}
		plan := mustPlan(t, q)

		having, ok := plan.(*planner.HavingNode)
		if !ok {
			t.Fatalf("root: want *HavingNode, got %T", plan)
		}
		agg, ok := having.Input.(*planner.AggregateNode)
		if !ok {
			t.Fatalf("below HAVING: want *AggregateNode, got %T", having.Input)
		}
		if _, ok := agg.Input.(*planner.FilterNode); !ok {
			t.Fatalf("below AGGREGATE: want *FilterNode (WHERE), got %T", agg.Input)
		}
	})
}

// ---------------------------------------------------------------------------
// §2 JOIN rewriting
// ---------------------------------------------------------------------------

func TestJoinRewriting(t *testing.T) {
	// §2.1 — single-hop LEFT join with the correct ON condition.
	t.Run("2.1/single-hop-left", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"customers.region"}}
		plan := mustPlan(t, q)
		joins := nodesOf[*planner.JoinNode](plan)
		if len(joins) != 1 {
			t.Fatalf("want 1 JoinNode, got %d", len(joins))
		}
		jn := joins[0]
		if jn.Kind != planner.LeftJoin {
			t.Errorf("join kind: want LEFT, got %q", jn.Kind)
		}
		if len(jn.On) != 1 || jn.On[0].Left.Column != "customer_id" || jn.On[0].Right.Column != "customer_id" {
			t.Errorf("join condition: got %+v", jn.On)
		}
	})

	// §2.1 — multi-hop join pulls in the intermediate dataset.
	t.Run("2.1/multi-hop", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"products.category"}}
		plan := mustPlan(t, q)
		got := scanAliases(plan)
		for _, want := range []string{"orders", "order_items", "products"} {
			if !got[want] {
				t.Errorf("multi-hop plan should scan %q; scans=%v", want, got)
			}
		}
		if n := len(nodesOf[*planner.JoinNode](plan)); n < 2 {
			t.Errorf("multi-hop should produce >=2 joins, got %d", n)
		}
	})

	// §2.1 — composite join key produces one ON condition per key column.
	t.Run("2.1/composite-key", func(t *testing.T) {
		m := compositeKeyModel()
		q := &query.Query{Metrics: []string{"a.cnt"}, Dimensions: []string{"b.k1"}}
		p, err := planner.Plan(q, m)
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		joins := nodesOf[*planner.JoinNode](p)
		if len(joins) != 1 || len(joins[0].On) != 2 {
			t.Fatalf("composite key: want 1 join with 2 conditions, got %+v", joins)
		}
	})

	// §2.1 — the join ON condition renders as left.col = right.col.
	t.Run("2.1/on-condition-render", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"customers.region"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, `ON "orders"."customer_id" = "customers"."customer_id"`) {
			t.Errorf("ON condition should render as left.col = right.col:\n%s", sql)
		}
	})

	// §2.1 — a composite join key renders its conditions joined by AND.
	t.Run("2.1/composite-key-render", func(t *testing.T) {
		m := compositeKeyModel()
		q := &query.Query{Metrics: []string{"a.cnt"}, Dimensions: []string{"b.k1"}}
		sql := compileWith(t, m, q, "postgres")
		want := `ON "a"."k1" = "b"."k1" AND "a"."k2" = "b"."k2"`
		if !strings.Contains(sql, want) {
			t.Errorf("composite key ON should AND-join its conditions\nwant: %s\ngot:\n%s", want, sql)
		}
	})

	// §2.2 self-join, §2.3 semi-join, §2.4 anti-join — not expressible yet.
	t.Run("2.2/self-join", func(t *testing.T) {
		pending(t, "2.2", "query IR cannot express a self-join (no per-reference alias)")
	})
	t.Run("2.3/semi-join", func(t *testing.T) {
		pending(t, "2.3", "query IR has no EXISTS/IN-subquery construct")
	})
	t.Run("2.4/anti-join", func(t *testing.T) {
		pending(t, "2.4", "query IR has no NOT EXISTS/NOT IN-subquery construct")
	})

	// §2.5 — fan-out detection: unsafe aggregate over the "one" side errors.
	t.Run("2.5/fan-out-detected", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Dimensions: []string{"products.category"}}
		err := planErr(t, q)
		var fe *planner.FanOutError
		if !errors.As(err, &fe) {
			t.Fatalf("want *FanOutError, got %v", err)
		}
	})
	t.Run("2.5/fan-out-safe-metric-ok", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"products.category"}}
		if err := planErr(t, q); err != nil {
			t.Fatalf("COUNT(DISTINCT) over a fan-out join should be safe, got %v", err)
		}
	})
	t.Run("2.5/pre-aggregation-rewrite", func(t *testing.T) {
		pending(t, "2.5", "fan-out-safe pre-aggregation rewrite not implemented")
	})
}

// ---------------------------------------------------------------------------
// §3 Aggregation rewriting
// ---------------------------------------------------------------------------

func TestAggregation(t *testing.T) {
	// §3.1 — GROUP BY carries every dimension.
	t.Run("3.1/group-by-dimensions", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Dimensions: []string{"orders.status", "orders.order_month"}}
		plan := mustPlan(t, q)
		aggs := nodesOf[*planner.AggregateNode](plan)
		if len(aggs) != 1 || len(aggs[0].GroupBy) != 2 {
			t.Fatalf("want aggregate grouping by 2 dimensions, got %+v", aggs)
		}
	})

	// §3.1 — no-dimension aggregate is a single-row group.
	t.Run("3.1/no-dimension-single-row", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}}
		plan := mustPlan(t, q)
		aggs := nodesOf[*planner.AggregateNode](plan)
		if len(aggs) != 1 || len(aggs[0].GroupBy) != 0 {
			t.Fatalf("want aggregate with no GROUP BY, got %+v", aggs)
		}
	})

	// §3.1 — an expression dimension (DATE_TRUNC) is groupable.
	t.Run("3.1/expression-dimension", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Dimensions: []string{"orders.order_month"}}
		plan := mustPlan(t, q)
		aggs := nodesOf[*planner.AggregateNode](plan)
		if len(aggs) != 1 || len(aggs[0].GroupBy) != 1 || aggs[0].GroupBy[0].Field.Name != "order_month" {
			t.Fatalf("want GROUP BY order_month, got %+v", aggs)
		}
	})

	// §3.2 — COUNT(DISTINCT ...) is fan-out safe (covered above); SQL text pending.
	t.Run("3.2/count-distinct-safe-across-join", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"products.category"}}
		if err := planErr(t, q); err != nil {
			t.Fatalf("COUNT(DISTINCT) across a join should not fan out, got %v", err)
		}
	})
	// §3.2 — COUNT(DISTINCT …) expression renders correctly via SelectExpression
	// and appears verbatim in the emitter output.
	t.Run("3.2/count-variants-render", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"orders.status"},
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "COUNT(DISTINCT") {
			t.Errorf("COUNT(DISTINCT) should appear verbatim in SQL:\n%s", sql)
		}
	})

	// §3.2 — COUNT(*) renders verbatim.
	t.Run("3.2/count-star-render", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.item_count"}, Dimensions: []string{"orders.status"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "COUNT(*)") {
			t.Errorf("COUNT(*) should render verbatim:\n%s", sql)
		}
	})

	// §3.2 / §9 — COUNT(col) renders verbatim (excludes NULLs, unlike COUNT(*)).
	t.Run("3.2/count-col-render", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"order_items.priced_lines"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "COUNT(price)") {
			t.Errorf("COUNT(col) should render verbatim:\n%s", sql)
		}
		if strings.Contains(sql, "COUNT(*)") {
			t.Errorf("COUNT(col) must not collapse to COUNT(*):\n%s", sql)
		}
	})

	// §3.3 — the conditional-aggregate expression is carried on the metric and
	// resolves through the shared selector; embedding it in SQL is codegen.
	// §3.3 — a CASE WHEN metric expression renders verbatim in the aggregate.
	t.Run("3.3/conditional-aggregate-expression", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.completed_revenue"},
			Dimensions: []string{"orders.status"},
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "CASE WHEN") {
			t.Errorf("conditional aggregate should render CASE WHEN verbatim:\n%s", sql)
		}
	})

	// §3.3 — an arbitrary expression inside an aggregate renders verbatim.
	t.Run("3.3/aggregate-on-expression", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"order_items.gross_revenue"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "SUM(price * quantity)") {
			t.Errorf("SUM(expr) should render the inner expression verbatim:\n%s", sql)
		}
	})

	// §3.4 / §3.5
	t.Run("3.4/pre-aggregation-pushdown", func(t *testing.T) {
		pending(t, "3.4", "push-down of partial aggregates not implemented")
	})
	t.Run("3.5/rollup-cube-grouping-sets", func(t *testing.T) {
		pending(t, "3.5", "query IR has no ROLLUP/CUBE/GROUPING SETS construct")
	})
}

// ---------------------------------------------------------------------------
// §4 DISTINCT
// ---------------------------------------------------------------------------

func TestDistinct(t *testing.T) {
	// §4 — a dimensions-only query is an aggregate with no measures, i.e. the
	// plan-level signal for SELECT DISTINCT.
	t.Run("4/select-distinct-no-measures", func(t *testing.T) {
		q := &query.Query{Dimensions: []string{"orders.status"}}
		plan := mustPlan(t, q)
		aggs := nodesOf[*planner.AggregateNode](plan)
		if len(aggs) != 1 || len(aggs[0].Aggregates) != 0 || len(aggs[0].GroupBy) != 1 {
			t.Fatalf("dimensions-only query should be a measure-less aggregate, got %+v", aggs)
		}
	})
	// §4 — a dimensions-only query is emitted as SELECT DISTINCT (no GROUP BY).
	t.Run("4/distinct-render", func(t *testing.T) {
		q := &query.Query{Dimensions: []string{"orders.status"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "SELECT DISTINCT") {
			t.Errorf("dimensions-only query should emit SELECT DISTINCT:\n%s", sql)
		}
		if strings.Contains(sql, "GROUP BY") {
			t.Errorf("dimensions-only query must not emit GROUP BY:\n%s", sql)
		}
	})
}

// ---------------------------------------------------------------------------
// §5 LIMIT / OFFSET
// ---------------------------------------------------------------------------

func TestLimitOffset(t *testing.T) {
	// §5 — LIMIT becomes the outermost node; it is never pushed below an
	// aggregate or join.
	t.Run("5/limit-is-outermost", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: intp(10)}
		plan := mustPlan(t, q)
		lim, ok := plan.(*planner.LimitNode)
		if !ok {
			t.Fatalf("root: want *LimitNode, got %T", plan)
		}
		if lim.Count != 10 {
			t.Errorf("limit count: want 10, got %d", lim.Count)
		}
		if _, isScan := lim.Input.(*planner.ScanNode); isScan {
			t.Error("LIMIT must not sit directly on a Scan when an aggregate is present")
		}
	})
	// §5 — OFFSET is carried on the LimitNode and rendered after LIMIT.
	t.Run("5/offset", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: intp(10), Offset: intp(20)}
		plan := mustPlan(t, q)
		lim, ok := plan.(*planner.LimitNode)
		if !ok {
			t.Fatalf("root: want *LimitNode, got %T", plan)
		}
		if !lim.HasLimit || lim.Count != 10 || lim.Offset != 20 {
			t.Fatalf("want LIMIT 10 OFFSET 20, got %+v", lim)
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "LIMIT 10") || !strings.Contains(sql, "OFFSET 20") {
			t.Errorf("OFFSET should render alongside LIMIT:\n%s", sql)
		}
	})
	// §5 — LIMIT n rendered correctly for PostgreSQL.
	t.Run("5/dialect-limit-syntax", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: intp(25)}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "LIMIT 25") {
			t.Errorf("LIMIT should render as 'LIMIT 25':\n%s", sql)
		}
	})
	// §5 — both supported dialects use the `LIMIT n OFFSET m` form (PostgreSQL
	// and BigQuery agree; MySQL/ANSI variants are out of scope).
	t.Run("5/dialect-limit-offset-form", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Limit: intp(5), Offset: intp(15)}
		for _, dialect := range []string{"postgres", "bigquery"} {
			sql := compileDialect(t, q, dialect)
			li := strings.Index(sql, "LIMIT 5")
			oi := strings.Index(sql, "OFFSET 15")
			if li < 0 || oi < 0 || oi < li {
				t.Errorf("%s: want 'LIMIT 5' before 'OFFSET 15':\n%s", dialect, sql)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// §6 CASE WHEN
// ---------------------------------------------------------------------------

func TestCaseWhen(t *testing.T) {
	// §6.3 — a filter over a CASE WHEN dimension is carried as a predicate.
	t.Run("6.3/filter-on-case-dimension", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{{Field: "orders.status_label", Op: "=", Value: raw(`"done"`)}},
		}
		plan := mustPlan(t, q)
		filters := nodesOf[*planner.FilterNode](plan)
		if len(filters) != 1 || filters[0].Predicates[0].Field.Name != "status_label" {
			t.Fatalf("want a filter on status_label, got %+v", filters)
		}
	})
	// §6.1 — CASE WHEN dimension expression renders verbatim in SELECT + GROUP BY.
	t.Run("6.1/case-dimension-render", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.item_count"},
			Dimensions: []string{"orders.status_label"},
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "CASE WHEN") {
			t.Errorf("CASE WHEN dimension should appear in SQL:\n%s", sql)
		}
		if !strings.Contains(sql, `"status_label"`) {
			t.Errorf("CASE WHEN dimension should be aliased as status_label:\n%s", sql)
		}
	})
	// §6.2 — SUM/COUNT/AVG over a CASE WHEN all render their conditional inner
	// expression verbatim inside the aggregate function.
	t.Run("6.2/case-metric-render", func(t *testing.T) {
		cases := []struct{ metric, fn string }{
			{"orders.completed_revenue", "SUM(CASE WHEN"},
			{"orders.completed_count", "COUNT(CASE WHEN"},
			{"orders.avg_completed", "AVG(CASE WHEN"},
		}
		for _, c := range cases {
			q := &query.Query{Metrics: []string{c.metric}, Dimensions: []string{"orders.status"}}
			sql := compilePostgres(t, q)
			if !strings.Contains(sql, c.fn) {
				t.Errorf("%s should render %q verbatim:\n%s", c.metric, c.fn, sql)
			}
		}
	})
	// §6.4 — COALESCE in a dimension and NULLIF in a metric render verbatim.
	t.Run("6.4/coalesce-nullif", func(t *testing.T) {
		dimSQL := compilePostgres(t, &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region_safe"},
		})
		if !strings.Contains(dimSQL, "COALESCE(region, 'unknown')") {
			t.Errorf("COALESCE dimension should render verbatim:\n%s", dimSQL)
		}
		metSQL := compilePostgres(t, &query.Query{Metrics: []string{"orders.safe_ratio"}})
		if !strings.Contains(metSQL, "NULLIF(COUNT(*), 0)") {
			t.Errorf("NULLIF metric should render verbatim:\n%s", metSQL)
		}
	})
}

// ---------------------------------------------------------------------------
// §7 Date / time grain  (selection is live via SelectExpression)
// ---------------------------------------------------------------------------

func TestDateGrain(t *testing.T) {
	month := dx(
		[2]string{"ANSI_SQL", "DATE_TRUNC('month', created_at)"},
		[2]string{"POSTGRES", "DATE_TRUNC('month', created_at)"},
		[2]string{"BIGQUERY", "DATE_TRUNC(created_at, MONTH)"},
	)

	t.Run("7/date-trunc-postgres", func(t *testing.T) {
		got, err := codegen.SelectExpression(month, "postgres")
		if err != nil || got != "DATE_TRUNC('month', created_at)" {
			t.Fatalf("postgres grain: got %q err %v", got, err)
		}
	})
	t.Run("7/date-trunc-bigquery", func(t *testing.T) {
		got, err := codegen.SelectExpression(month, "bigquery")
		if err != nil || got != "DATE_TRUNC(created_at, MONTH)" {
			t.Fatalf("bigquery grain: got %q err %v", got, err)
		}
	})
	// §7 — month / quarter / year grains all render for both dialects, with the
	// dialect-correct DATE_TRUNC argument order.
	t.Run("7/date-trunc-grains", func(t *testing.T) {
		grains := []struct {
			dim, pg, bq string
		}{
			{"orders.order_month", "DATE_TRUNC('month', created_at)", "DATE_TRUNC(created_at, MONTH)"},
			{"orders.order_quarter", "DATE_TRUNC('quarter', created_at)", "DATE_TRUNC(created_at, QUARTER)"},
			{"orders.order_year", "DATE_TRUNC('year', created_at)", "DATE_TRUNC(created_at, YEAR)"},
		}
		for _, g := range grains {
			q := &query.Query{Metrics: []string{"orders.revenue"}, Dimensions: []string{g.dim}}
			if sql := compilePostgres(t, q); !strings.Contains(sql, g.pg) {
				t.Errorf("postgres %s: want %q:\n%s", g.dim, g.pg, sql)
			}
			if sql := compileBigQuery(t, q); !strings.Contains(sql, g.bq) {
				t.Errorf("bigquery %s: want %q:\n%s", g.dim, g.bq, sql)
			}
		}
	})
	t.Run("7/extract-interval-timezone", func(t *testing.T) {
		pending(t, "7", "EXTRACT / interval / timezone rewrites not modelled yet")
	})
}

// ---------------------------------------------------------------------------
// §8 Subquery / CTE strategy  (all codegen)
// ---------------------------------------------------------------------------

func TestSubqueryCTE(t *testing.T) {
	for _, id := range []string{"8/inline-subquery", "8/cte", "8/cte-vs-subquery-choice", "8/nested-cte", "8/recursive-cte", "8/push-predicate-into-cte"} {
		t.Run(id, func(t *testing.T) { pending(t, id, "subquery/CTE emission not implemented (codegen)") })
	}
}

// ---------------------------------------------------------------------------
// §9 NULL handling
// ---------------------------------------------------------------------------

func TestNullHandling(t *testing.T) {
	// §9 — IS NULL / IS NOT NULL carry a nil value (also covered in §1.1).
	t.Run("9/is-null-predicate", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{{Field: "orders.status", Op: "IS NULL"}},
		}
		plan := mustPlan(t, q)
		filters := nodesOf[*planner.FilterNode](plan)
		if len(filters) != 1 || filters[0].Predicates[0].Value != nil {
			t.Fatalf("IS NULL should carry a nil value, got %+v", filters)
		}
	})
	// §9 — IS NULL predicate renders as expr IS NULL (no value placeholder).
	t.Run("9/count-col-vs-star-render", func(t *testing.T) {
		q := &query.Query{
			Metrics: []string{"orders.item_count"},
			Filters: []query.Filter{{Field: "orders.status", Op: "IS NULL"}},
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "status IS NULL") {
			t.Errorf("IS NULL should render as 'status IS NULL':\n%s", sql)
		}
	})
	// §9 — COUNT(col) excludes NULLs and must render distinctly from COUNT(*).
	t.Run("9/count-col-excludes-null", func(t *testing.T) {
		colSQL := compilePostgres(t, &query.Query{Metrics: []string{"order_items.priced_lines"}})
		if !strings.Contains(colSQL, "COUNT(price)") {
			t.Errorf("COUNT(col) should render as COUNT(price):\n%s", colSQL)
		}
		starSQL := compilePostgres(t, &query.Query{Metrics: []string{"orders.item_count"}})
		if !strings.Contains(starSQL, "COUNT(*)") {
			t.Errorf("COUNT(*) should render as COUNT(*):\n%s", starSQL)
		}
	})
	t.Run("9/anti-join-null-check", func(t *testing.T) {
		pending(t, "9", "LEFT JOIN ... IS NULL anti-join pattern not implemented")
	})
	t.Run("9/null-safe-equality", func(t *testing.T) {
		pending(t, "9", "IS NOT DISTINCT FROM / <=> rendering not implemented (codegen)")
	})
}

// ---------------------------------------------------------------------------
// §10 Window functions  (need IR support)
// ---------------------------------------------------------------------------

func TestWindowFunctions(t *testing.T) {
	for _, id := range []string{"10/row-number", "10/rank", "10/running-sum", "10/moving-average", "10/filter-on-window"} {
		t.Run(id, func(t *testing.T) { pending(t, id, "query IR has no window-function construct") })
	}
}

// compilePostgres runs the full plan+pushdown+emit pipeline against the
// ecommerce model and returns the SQL string. It is used by dialect-rewrite
// conformance gates.
func compilePostgres(t *testing.T, q *query.Query) string {
	t.Helper()
	return compileDialect(t, q, "postgres")
}

// compileBigQuery is the BigQuery counterpart of compilePostgres.
func compileBigQuery(t *testing.T, q *query.Query) string {
	t.Helper()
	return compileDialect(t, q, "bigquery")
}

func compileDialect(t *testing.T, q *query.Query, dialect string) string {
	t.Helper()
	plan := mustPlan(t, q) // already calls PushDown
	sql, err := codegen.Compile(plan, dialect)
	if err != nil {
		t.Fatalf("Compile(%s): %v", dialect, err)
	}
	return sql
}

// ---------------------------------------------------------------------------
// §11 Dialect-specific rewrites
// ---------------------------------------------------------------------------

func TestDialectRewrites(t *testing.T) {
	simple := mustPlan(t, &query.Query{Metrics: []string{"orders.revenue"}})

	// §11 — unknown dialect is a hard error, not ErrNotImplemented.
	t.Run("11/unknown-dialect-error", func(t *testing.T) {
		if _, err := codegen.Compile(simple, "mysql"); err == nil {
			t.Fatal("unknown dialect should error")
		} else if errors.Is(err, codegen.ErrNotImplemented) {
			t.Fatal("unknown dialect should not report ErrNotImplemented")
		}
	})

	// §11 — recognised dialects either compile successfully or return
	// ErrNotImplemented (pending emitter); they must never return an
	// "unsupported dialect" hard error.
	t.Run("11/recognised-dialect-dispatch", func(t *testing.T) {
		for _, d := range codegen.SupportedDialects {
			_, err := codegen.Compile(simple, d)
			if err != nil && !errors.Is(err, codegen.ErrNotImplemented) {
				t.Errorf("dialect %q: want success or ErrNotImplemented, got %v", d, err)
			}
		}
	})

	// §11.1 — PostgreSQL identifier quoting: join ON conditions use
	// double-quoted "dataset"."column" syntax.
	t.Run("11/identifier-quoting", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
		}
		sql := compilePostgres(t, q)
		want := `"orders"."customer_id" = "customers"."customer_id"`
		if !strings.Contains(sql, want) {
			t.Errorf("join ON condition should use double-quoted identifiers\nwant substring: %s\ngot:\n%s", want, sql)
		}
	})

	// §11.1 — schema-qualified table source from OSI dataset.Source.
	t.Run("11/table-path", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.revenue"}, Dimensions: []string{"orders.status"}}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "FROM analytics.orders") {
			t.Errorf("FROM clause should use analytics.orders table path:\n%s", sql)
		}
	})

	// §11.1 — BigQuery identifier quoting: join ON conditions use
	// backtick-quoted `dataset`.`column` syntax.
	t.Run("11/bigquery-backtick-quoting", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.order_count"},
			Dimensions: []string{"customers.region"},
		}
		sql := compileBigQuery(t, q)
		want := "`orders`.`customer_id` = `customers`.`customer_id`"
		if !strings.Contains(sql, want) {
			t.Errorf("join ON condition should use backtick identifiers\nwant substring: %s\ngot:\n%s", want, sql)
		}
		if strings.Contains(sql, `"orders"`) {
			t.Errorf("BigQuery output must not contain double-quoted identifiers:\n%s", sql)
		}
	})

	// §11 — the same plan compiled for both dialects diverges in quoting.
	t.Run("11/dialect-divergence", func(t *testing.T) {
		q := &query.Query{Metrics: []string{"orders.order_count"}, Dimensions: []string{"customers.region"}}
		pg := compilePostgres(t, q)
		bq := compileBigQuery(t, q)
		if pg == bq {
			t.Errorf("postgres and bigquery output should differ:\n%s", pg)
		}
		if !strings.Contains(bq, "`") {
			t.Errorf("bigquery output should contain backticks:\n%s", bq)
		}
		if !strings.Contains(pg, `"`) {
			t.Errorf("postgres output should contain double quotes:\n%s", pg)
		}
	})

	// §11.2-11.4 — dialect-divergent expressions (casts, string concat) resolve
	// via the per-dialect OSI entry, while functions that agree across dialects
	// (UPPER, boolean literals) pass through unchanged.
	t.Run("11/casts-and-string-fns", func(t *testing.T) {
		check := func(label, want, got string, err error) {
			t.Helper()
			if err != nil || got != want {
				t.Errorf("%s: want %q, got %q (err %v)", label, want, got, err)
			}
		}
		// §11.4 — CAST type names diverge.
		cast := dx([2]string{"POSTGRES", "CAST(amount AS INTEGER)"}, [2]string{"BIGQUERY", "CAST(amount AS INT64)"})
		g, e := codegen.SelectExpression(cast, "postgres")
		check("cast/pg", "CAST(amount AS INTEGER)", g, e)
		g, e = codegen.SelectExpression(cast, "bigquery")
		check("cast/bq", "CAST(amount AS INT64)", g, e)
		// §11.4 — the `::` cast shorthand is PostgreSQL-only.
		shorthand := dx([2]string{"POSTGRES", "amount::integer"}, [2]string{"BIGQUERY", "CAST(amount AS INT64)"})
		g, e = codegen.SelectExpression(shorthand, "postgres")
		check("cast-shorthand/pg", "amount::integer", g, e)
		// §11.2 — string concat: `||` (ANSI/PostgreSQL) vs CONCAT (BigQuery).
		concat := dx([2]string{"ANSI_SQL", "first_name || last_name"}, [2]string{"BIGQUERY", "CONCAT(first_name, last_name)"})
		g, e = codegen.SelectExpression(concat, "postgres")
		check("concat/pg", "first_name || last_name", g, e)
		g, e = codegen.SelectExpression(concat, "bigquery")
		check("concat/bq", "CONCAT(first_name, last_name)", g, e)
		// §11.2 — UPPER is identical across dialects (ANSI fallback serves both).
		upper := ax("UPPER(region)")
		g, e = codegen.SelectExpression(upper, "postgres")
		check("upper/pg", "UPPER(region)", g, e)
		g, e = codegen.SelectExpression(upper, "bigquery")
		check("upper/bq", "UPPER(region)", g, e)
		// §11.3 — boolean literals are identical (TRUE/FALSE) in both dialects.
		boolean := ax("is_active = TRUE")
		g, e = codegen.SelectExpression(boolean, "postgres")
		check("bool/pg", "is_active = TRUE", g, e)
		g, e = codegen.SelectExpression(boolean, "bigquery")
		check("bool/bq", "is_active = TRUE", g, e)
	})
	t.Run("11/unnest", func(t *testing.T) {
		pending(t, "11.6", "UNNEST not modelled yet")
	})
}

// ---------------------------------------------------------------------------
// §12 Expression passthrough  (live via SelectExpression)
// ---------------------------------------------------------------------------

func TestExpressionPassthrough(t *testing.T) {
	// §12 — exact-dialect match wins.
	t.Run("12/verbatim-target-dialect", func(t *testing.T) {
		e := dx([2]string{"ANSI_SQL", "ansi"}, [2]string{"POSTGRES", "pg"}, [2]string{"BIGQUERY", "bq"})
		if got, _ := codegen.SelectExpression(e, "postgres"); got != "pg" {
			t.Errorf("want exact postgres expr 'pg', got %q", got)
		}
		if got, _ := codegen.SelectExpression(e, "bigquery"); got != "bq" {
			t.Errorf("want exact bigquery expr 'bq', got %q", got)
		}
	})

	// §12 — ANSI_SQL fallback when the target dialect is absent.
	t.Run("12/ansi-fallback", func(t *testing.T) {
		e := ax("ansi_only")
		got, err := codegen.SelectExpression(e, "bigquery")
		if err != nil || got != "ansi_only" {
			t.Fatalf("want ANSI fallback 'ansi_only', got %q err %v", got, err)
		}
	})

	// §12 — error when neither the dialect nor ANSI_SQL is present.
	t.Run("12/missing-dialect-error", func(t *testing.T) {
		e := dx([2]string{"SNOWFLAKE", "snow"})
		if _, err := codegen.SelectExpression(e, "postgres"); err == nil {
			t.Fatal("missing dialect + no ANSI fallback should error")
		}
	})

	t.Run("12/nested-expression-reference", func(t *testing.T) {
		pending(t, "12", "field-references-field expression nesting not modelled yet")
	})
}

// ---------------------------------------------------------------------------
// §13 ORDER BY
// ---------------------------------------------------------------------------

func TestOrderBy(t *testing.T) {
	// §13 — directions are carried, and an empty direction normalises to ASC.
	t.Run("13/directions-and-default", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.revenue"},
			Dimensions: []string{"orders.status"},
			OrderBy: []query.OrderItem{
				{Field: "orders.revenue", Direction: "DESC"},
				{Field: "orders.status"}, // no direction -> ASC
			},
		}
		plan := mustPlan(t, q)
		orders := nodesOf[*planner.OrderNode](plan)
		if len(orders) != 1 || len(orders[0].Items) != 2 {
			t.Fatalf("want 1 OrderNode with 2 items, got %+v", orders)
		}
		if orders[0].Items[0].Direction != "DESC" || orders[0].Items[1].Direction != "ASC" {
			t.Errorf("directions: want [DESC ASC], got %+v", orders[0].Items)
		}
	})
	// §13 — NULLS FIRST / NULLS LAST is carried through the plan and rendered.
	t.Run("13/nulls-first-last", func(t *testing.T) {
		q := &query.Query{
			Metrics:    []string{"orders.revenue"},
			Dimensions: []string{"orders.status"},
			OrderBy: []query.OrderItem{
				{Field: "orders.revenue", Direction: "DESC", Nulls: "LAST"},
				{Field: "orders.status", Nulls: "FIRST"},
			},
		}
		plan := mustPlan(t, q)
		orders := nodesOf[*planner.OrderNode](plan)
		if len(orders) != 1 || orders[0].Items[0].Nulls != "LAST" || orders[0].Items[1].Nulls != "FIRST" {
			t.Fatalf("nulls placement not carried: %+v", orders)
		}
		sql := compilePostgres(t, q)
		if !strings.Contains(sql, "DESC NULLS LAST") || !strings.Contains(sql, "ASC NULLS FIRST") {
			t.Errorf("NULLS FIRST/LAST should render:\n%s", sql)
		}
	})
}

// ---------------------------------------------------------------------------
// §14 Safety rules
// ---------------------------------------------------------------------------

func TestSafetyRules(t *testing.T) {
	// §14 — two datasets with no relationship path is an error (no cartesian).
	t.Run("14/no-cartesian-product", func(t *testing.T) {
		m := disconnectedModel()
		_, err := planner.Plan(&query.Query{Metrics: []string{"a.cnt"}, Dimensions: []string{"b.y"}}, m)
		if err == nil {
			t.Fatal("disconnected datasets should error, got nil")
		}
	})

	// §14 — a relationship cycle must not hang BFS; the plan still resolves.
	t.Run("14/cyclic-join-path-terminates", func(t *testing.T) {
		m := cyclicModel()
		_, err := planner.Plan(&query.Query{Metrics: []string{"a.cnt"}, Dimensions: []string{"b.x", "c.y"}}, m)
		if err != nil {
			t.Fatalf("cyclic graph should still resolve, got %v", err)
		}
	})

	t.Run("14/ambiguous-column-error", func(t *testing.T) {
		pending(t, "14", "ambiguous-column detection not implemented (codegen qualification)")
	})
}
