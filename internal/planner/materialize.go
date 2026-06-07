package planner

// Strategy selects how base table sources are emitted as SQL.
type Strategy int

const (
	// Flat keeps each scan as a direct table reference; any pushed-down filter
	// is rendered as a WHERE clause in the query block that owns the scan. This
	// is the default and leaves the plan tree unchanged.
	Flat Strategy = iota

	// Subquery wraps every base scan in a derived table:
	// `(SELECT * FROM t WHERE ...) AS alias`. A pushed-down predicate is
	// evaluated inside the subquery.
	Subquery

	// CTE hoists every base scan into a WITH clause and references it by name:
	// `WITH alias AS (SELECT * FROM t WHERE ...) ... FROM alias`.
	CTE

	// Auto chooses CTE when any dataset is scanned more than once (so the shared
	// definition is written exactly once) and Subquery otherwise.
	Auto
)

// Materialize rewrites a post-PushDown plan so that base table scans are
// emitted as subqueries or CTEs according to s. Flat returns root unchanged.
//
// It runs after PushDown, so every dataset's predicates already sit in a
// FilterNode directly above its ScanNode. Materialize moves that filtered scan
// into a nested SELECT, realising the "push the predicate into the subquery /
// CTE body" rewrites from the checklist (§1.4, §8). Scans without a filter are
// still materialized so the chosen strategy is applied uniformly.
func Materialize(root PlanNode, s Strategy) PlanNode {
	switch s {
	case Flat:
		return root
	case Auto:
		s = chooseStrategy(root)
	}

	if s == CTE {
		var defs []CTEDef
		body := rewriteScans(root, CTE, &defs)
		if len(defs) == 0 {
			return body
		}
		return &WithNode{CTEs: defs, Body: body}
	}
	return rewriteScans(root, Subquery, nil)
}

// chooseStrategy implements the CTE-vs-subquery rule: a dataset referenced more
// than once must be a CTE (it cannot be inlined without duplicating its body),
// otherwise single-use scans become inline subqueries.
func chooseStrategy(root PlanNode) Strategy {
	counts := map[string]int{}
	countScans(root, counts)
	for _, c := range counts {
		if c > 1 {
			return CTE
		}
	}
	return Subquery
}

// rewriteScans walks the tree and replaces each base scan — a bare ScanNode or
// a FilterNode sitting directly above one — with a SubqueryNode (Subquery) or a
// CTERef plus a hoisted CTEDef (CTE). A FilterNode above a JoinNode (a residual
// cross-dataset predicate) is left in place; only its input is rewritten.
func rewriteScans(n PlanNode, s Strategy, defs *[]CTEDef) PlanNode {
	switch t := n.(type) {
	case *ScanNode:
		return materializeSource(t, t, s, defs)

	case *FilterNode:
		if scan, ok := t.Input.(*ScanNode); ok {
			return materializeSource(scan, t, s, defs)
		}
		return &FilterNode{Input: rewriteScans(t.Input, s, defs), Predicates: t.Predicates}

	case *JoinNode:
		return &JoinNode{
			Left:         rewriteScans(t.Left, s, defs),
			Right:        rewriteScans(t.Right, s, defs),
			On:           t.On,
			Kind:         t.Kind,
			Relationship: t.Relationship,
		}

	case *AggregateNode:
		return &AggregateNode{Input: rewriteScans(t.Input, s, defs), GroupBy: t.GroupBy, Aggregates: t.Aggregates}

	case *HavingNode:
		return &HavingNode{Input: rewriteScans(t.Input, s, defs), Predicates: t.Predicates}

	case *OrderNode:
		return &OrderNode{Input: rewriteScans(t.Input, s, defs), Items: t.Items}

	case *LimitNode:
		return &LimitNode{Input: rewriteScans(t.Input, s, defs), Count: t.Count, HasLimit: t.HasLimit, Offset: t.Offset}

	default:
		return n
	}
}

// materializeSource turns a base scan (body carries its WHERE filter, if any)
// into a SubqueryNode, or hoists it into a CTE and returns a CTERef. A dataset
// referenced more than once is hoisted only once: later references reuse the
// same CTE definition by name.
func materializeSource(scan *ScanNode, body PlanNode, s Strategy, defs *[]CTEDef) PlanNode {
	if s == CTE {
		if !hasCTE(*defs, scan.Alias) {
			*defs = append(*defs, CTEDef{Name: scan.Alias, Query: body})
		}
		return &CTERef{Name: scan.Alias, Alias: scan.Alias}
	}
	return &SubqueryNode{Input: body, Alias: scan.Alias}
}

// hasCTE reports whether defs already contains a CTE named name.
func hasCTE(defs []CTEDef, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// countScans tallies how many ScanNodes carry each alias across the tree.
func countScans(n PlanNode, counts map[string]int) {
	switch t := n.(type) {
	case *ScanNode:
		counts[t.Alias]++
	case *FilterNode:
		countScans(t.Input, counts)
	case *JoinNode:
		countScans(t.Left, counts)
		countScans(t.Right, counts)
	case *AggregateNode:
		countScans(t.Input, counts)
	case *HavingNode:
		countScans(t.Input, counts)
	case *OrderNode:
		countScans(t.Input, counts)
	case *LimitNode:
		countScans(t.Input, counts)
	case *SubqueryNode:
		countScans(t.Input, counts)
	case *WithNode:
		for _, c := range t.CTEs {
			countScans(c.Query, counts)
		}
		countScans(t.Body, counts)
	}
}
