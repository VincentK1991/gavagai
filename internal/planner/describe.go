package planner

// Describe renders a plan tree as a compact prefix-form string, e.g.
// "Limit(Order(Aggregate(Filter(Join(Scan(orders), Scan(customers))))))".
// It is used by tests for shape assertions and by the CLI's --explain flag.
func Describe(n PlanNode) string {
	switch t := n.(type) {
	case *ScanNode:
		return "Scan(" + t.Alias + ")"
	case *JoinNode:
		return "Join(" + Describe(t.Left) + ", " + Describe(t.Right) + ")"
	case *FilterNode:
		return "Filter(" + Describe(t.Input) + ")"
	case *AggregateNode:
		return "Aggregate(" + Describe(t.Input) + ")"
	case *HavingNode:
		return "Having(" + Describe(t.Input) + ")"
	case *OrderNode:
		return "Order(" + Describe(t.Input) + ")"
	case *LimitNode:
		return "Limit(" + Describe(t.Input) + ")"
	default:
		return "?"
	}
}
