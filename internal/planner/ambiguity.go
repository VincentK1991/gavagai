package planner

import (
	"regexp"

	"github.com/vincentk1991/gavagai/internal/model"
)

// bareIdent matches a SQL expression that is a single unqualified column name,
// e.g. "region" or "customer_id" — but not "customers.region", "SUM(x)", or any
// expression carrying a qualifier, function call, or operator.
var bareIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// bareColumn reports whether e is a single unqualified column reference and, if
// so, returns the column name. Such expressions are the ones that become
// ambiguous when more than one joined dataset exposes the same column.
func bareColumn(e model.Expression) (string, bool) {
	expr := ansiExpression(e)
	if bareIdent.MatchString(expr) {
		return expr, true
	}
	return "", false
}

// qualifyAmbiguousColumns disambiguates bare-column dimensions and filter
// predicates whose column name is exposed by more than one of the joined
// datasets. Rather than emit SQL where the column binds ambiguously (or reject
// the query), the planner pins each reference to its own dataset
// (Dataset.column) and gives colliding output columns distinct aliases.
//
// With fewer than two datasets in scope there is no ambiguity and the inputs
// are returned unchanged.
func qualifyAmbiguousColumns(dims []DimensionExpr, preds []Predicate, datasets []*model.Dataset) ([]DimensionExpr, []Predicate) {
	if len(datasets) < 2 {
		return dims, preds
	}

	// owners[col] = the datasets that expose a bare column named col.
	owners := map[string]map[string]bool{}
	for _, ds := range datasets {
		for i := range ds.Fields {
			if col, ok := bareColumn(ds.Fields[i].Expression); ok {
				if owners[col] == nil {
					owners[col] = map[string]bool{}
				}
				owners[col][ds.Name] = true
			}
		}
	}
	ambiguous := func(col string) bool { return len(owners[col]) >= 2 }

	// Count selected dimension field names so colliding ones can be aliased.
	nameCount := map[string]int{}
	for _, d := range dims {
		nameCount[d.Field.Name]++
	}

	outDims := make([]DimensionExpr, len(dims))
	for i, d := range dims {
		outDims[i] = d
		if col, ok := bareColumn(d.Field.Expression); ok && ambiguous(col) {
			outDims[i].QualifyColumn = col
		}
		if nameCount[d.Field.Name] > 1 {
			outDims[i].OutAlias = d.Dataset + "_" + d.Field.Name
		}
	}

	outPreds := make([]Predicate, len(preds))
	for i, p := range preds {
		outPreds[i] = p
		if p.Field == nil {
			continue
		}
		if col, ok := bareColumn(p.Field.Expression); ok && ambiguous(col) {
			outPreds[i].QualifyColumn = col
		}
	}

	return outDims, outPreds
}

// scanDatasets collects the datasets read by every ScanNode in the subtree, in
// first-seen order, de-duplicated by dataset name.
func scanDatasets(n PlanNode) []*model.Dataset {
	var out []*model.Dataset
	seen := map[string]bool{}
	var walk func(PlanNode)
	walk = func(n PlanNode) {
		switch t := n.(type) {
		case *ScanNode:
			if t.Dataset != nil && !seen[t.Dataset.Name] {
				seen[t.Dataset.Name] = true
				out = append(out, t.Dataset)
			}
		case *JoinNode:
			walk(t.Left)
			walk(t.Right)
		case *FilterNode:
			walk(t.Input)
		}
	}
	walk(n)
	return out
}
