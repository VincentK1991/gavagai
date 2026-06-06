package planner

// PushDown rewrites a plan tree so that each FilterNode sits at the lowest
// scope whose input already exposes the columns its predicates reference —
// inside a per-dataset Scan (or a future subquery/CTE) rather than above a
// Join. Pushing filters down shrinks the rows a join has to consider and lets
// dialects emit more selective sub-queries.
//
// This is the Phase 4 seam. The real rewrite is not implemented yet; this
// identity placeholder returns the tree unchanged, which is always correct —
// a filter evaluated at the outer scope yields the same rows as one pushed
// down, only less efficiently. The conformance suite asserts the *target*
// scope for each pushdown case and skips (PENDING) until this function does
// the relocation.
func PushDown(root PlanNode) PlanNode {
	return root
}
