package planner

// PushDown rewrites a plan tree so that each FilterNode sits at the lowest
// scope whose input already exposes the columns its predicates reference —
// directly above a ScanNode rather than above a JoinNode or higher. This
// shrinks the rows each side of a join must process before the join runs.
//
// Each Predicate carries a Dataset field that names the scan it belongs to, so
// every predicate can always be located at its ScanNode. Predicates for a
// dataset that is not reached by any ScanNode in the subtree (which cannot
// happen when the query has passed Plan) remain as a residual FilterNode above
// the root.
//
// The rewrite is idempotent: applying PushDown twice yields the same tree.
func PushDown(root PlanNode) PlanNode {
	return pushDown(root)
}

func pushDown(n PlanNode) PlanNode {
	switch t := n.(type) {
	case *ScanNode:
		return t

	case *FilterNode:
		// Recurse first so child nodes are already in their lowest scope, then
		// relocate this FilterNode's predicates into the resulting subtree.
		input := pushDown(t.Input)
		return injectPredicates(input, t.Predicates)

	case *JoinNode:
		return &JoinNode{
			Left:         pushDown(t.Left),
			Right:        pushDown(t.Right),
			On:           t.On,
			Kind:         t.Kind,
			Relationship: t.Relationship,
		}

	case *AggregateNode:
		return &AggregateNode{
			Input:      pushDown(t.Input),
			GroupBy:    t.GroupBy,
			Aggregates: t.Aggregates,
		}

	case *HavingNode:
		return &HavingNode{Input: pushDown(t.Input), Predicates: t.Predicates}

	case *OrderNode:
		return &OrderNode{Input: pushDown(t.Input), Items: t.Items}

	case *LimitNode:
		return &LimitNode{Input: pushDown(t.Input), Count: t.Count, HasLimit: t.HasLimit, Offset: t.Offset}

	default:
		return n
	}
}

// injectPredicates groups preds by dataset and injects each group as a batch
// into the lowest matching ScanNode in the subtree of root. Predicates whose
// dataset is not reachable from root remain as a residual FilterNode above root.
func injectPredicates(root PlanNode, preds []Predicate) PlanNode {
	// Group by dataset, preserving first-seen order for deterministic output.
	byDS := make(map[string][]Predicate, len(preds))
	var dsOrder []string
	for _, p := range preds {
		if _, seen := byDS[p.Dataset]; !seen {
			dsOrder = append(dsOrder, p.Dataset)
		}
		byDS[p.Dataset] = append(byDS[p.Dataset], p)
	}

	var residual []Predicate
	for _, ds := range dsOrder {
		batch := byDS[ds]
		var ok bool
		root, ok = injectBatch(root, ds, batch)
		if !ok {
			residual = append(residual, batch...)
		}
	}
	if len(residual) > 0 {
		return &FilterNode{Input: root, Predicates: residual}
	}
	return root
}

// injectBatch pushes a batch of same-dataset predicates to the ScanNode whose
// Alias matches ds. Returns the rewritten subtree and true on success, or the
// original n and false if ds is not reachable.
//
// The three node types handled are the only ones that appear between a root
// FilterNode and its target ScanNode in the trees produced by Plan + pushDown:
// ScanNode (the target), JoinNode (branch point), and FilterNode (an earlier
// push of a different dataset's predicates to the same branch). All other node
// types are treated as opaque so predicates do not cross aggregate/having/order
// boundaries.
func injectBatch(n PlanNode, ds string, preds []Predicate) (PlanNode, bool) {
	switch t := n.(type) {
	case *ScanNode:
		if t.Alias == ds {
			return &FilterNode{Input: t, Predicates: preds}, true
		}
		return n, false

	case *FilterNode:
		// An earlier pass already pushed predicates for a different dataset to
		// this branch. Recurse through the FilterNode to reach the scan below it.
		in, ok := injectBatch(t.Input, ds, preds)
		if !ok {
			return n, false
		}
		return &FilterNode{Input: in, Predicates: t.Predicates}, true

	case *JoinNode:
		if left, ok := injectBatch(t.Left, ds, preds); ok {
			return &JoinNode{Left: left, Right: t.Right, On: t.On, Kind: t.Kind, Relationship: t.Relationship}, true
		}
		if right, ok := injectBatch(t.Right, ds, preds); ok {
			return &JoinNode{Left: t.Left, Right: right, On: t.On, Kind: t.Kind, Relationship: t.Relationship}, true
		}
		return n, false

	default:
		// Do not push predicates through aggregate, having, order, or limit
		// nodes — they represent scope boundaries that must not be crossed.
		return n, false
	}
}
