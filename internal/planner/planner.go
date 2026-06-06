package planner

import (
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/model"
	"github.com/vincentk1991/gavagai/internal/query"
)

// Plan resolves a query against a semantic model into a relational-algebra
// plan tree. It assumes the query has already passed query.Validate; it still
// re-resolves references defensively and returns an error for anything it
// cannot resolve, for unconnected datasets, or for fan-out.
//
// The node stack is built bottom-up:
//
//	Scan/Join -> Filter (WHERE) -> Aggregate (GROUP BY) -> Having -> Order -> Limit
//
// Clauses that are absent in the query are omitted from the stack.
func Plan(q *query.Query, m *model.SemanticModel) (PlanNode, error) {
	idx := newIndex(m)

	metrics, err := resolveMetrics(q.Metrics, idx)
	if err != nil {
		return nil, err
	}
	dims, err := resolveDimensions(q.Dimensions, idx)
	if err != nil {
		return nil, err
	}
	preds, err := resolveFilters(q.Filters, idx)
	if err != nil {
		return nil, err
	}
	havings, err := resolveHaving(q.Having, idx)
	if err != nil {
		return nil, err
	}
	orders := resolveOrderBy(q.OrderBy)

	refs := referencedDatasets(q)
	base, edges, err := resolveJoins(refs, m)
	if err != nil {
		return nil, err
	}

	if err := detectFanOut(metrics, edges); err != nil {
		return nil, err
	}

	node := base
	if len(preds) > 0 {
		node = &FilterNode{Input: node, Predicates: preds}
	}
	node = &AggregateNode{Input: node, GroupBy: dims, Aggregates: metrics}
	if len(havings) > 0 {
		node = &HavingNode{Input: node, Predicates: havings}
	}
	if len(orders) > 0 {
		node = &OrderNode{Input: node, Items: orders}
	}
	if q.Limit != nil {
		node = &LimitNode{Input: node, Count: *q.Limit}
	}

	return node, nil
}

// index holds lookup maps for fast reference resolution.
type index struct {
	datasets map[string]*model.Dataset
	fields   map[string]map[string]*model.Field
	metrics  map[string]*model.Metric
}

func newIndex(m *model.SemanticModel) *index {
	idx := &index{
		datasets: make(map[string]*model.Dataset, len(m.Datasets)),
		fields:   make(map[string]map[string]*model.Field, len(m.Datasets)),
		metrics:  make(map[string]*model.Metric, len(m.Metrics)),
	}
	for i := range m.Datasets {
		ds := &m.Datasets[i]
		idx.datasets[ds.Name] = ds
		fm := make(map[string]*model.Field, len(ds.Fields))
		for j := range ds.Fields {
			fm[ds.Fields[j].Name] = &ds.Fields[j]
		}
		idx.fields[ds.Name] = fm
	}
	for i := range m.Metrics {
		idx.metrics[m.Metrics[i].Name] = &m.Metrics[i]
	}
	return idx
}

func resolveMetrics(refs []string, idx *index) ([]MetricExpr, error) {
	out := make([]MetricExpr, 0, len(refs))
	for _, ref := range refs {
		ds, name, ok := splitRef(ref)
		if !ok {
			return nil, fmt.Errorf("plan: invalid metric reference %q", ref)
		}
		mt, found := idx.metrics[name]
		if !found {
			return nil, fmt.Errorf("plan: unknown metric %q", ref)
		}
		out = append(out, MetricExpr{Ref: ref, Dataset: ds, Metric: mt})
	}
	return out, nil
}

func resolveDimensions(refs []string, idx *index) ([]DimensionExpr, error) {
	out := make([]DimensionExpr, 0, len(refs))
	for _, ref := range refs {
		ds, name, ok := splitRef(ref)
		if !ok {
			return nil, fmt.Errorf("plan: invalid dimension reference %q", ref)
		}
		f, err := idx.field(ds, name)
		if err != nil {
			return nil, err
		}
		out = append(out, DimensionExpr{Ref: ref, Dataset: ds, Field: f})
	}
	return out, nil
}

func resolveFilters(filters []query.Filter, idx *index) ([]Predicate, error) {
	out := make([]Predicate, 0, len(filters))
	for _, f := range filters {
		ds, name, ok := splitRef(f.Field)
		if !ok {
			return nil, fmt.Errorf("plan: invalid filter field %q", f.Field)
		}
		field, err := idx.field(ds, name)
		if err != nil {
			return nil, err
		}
		out = append(out, Predicate{Dataset: ds, Field: field, Op: f.Op, Value: f.Value})
	}
	return out, nil
}

func resolveHaving(having []query.Having, idx *index) ([]HavingPredicate, error) {
	out := make([]HavingPredicate, 0, len(having))
	for _, h := range having {
		ds, name, ok := splitRef(h.Metric)
		if !ok {
			return nil, fmt.Errorf("plan: invalid having metric %q", h.Metric)
		}
		mt, found := idx.metrics[name]
		if !found {
			return nil, fmt.Errorf("plan: unknown having metric %q", h.Metric)
		}
		out = append(out, HavingPredicate{Dataset: ds, Metric: mt, Op: h.Op, Value: h.Value})
	}
	return out, nil
}

func resolveOrderBy(items []query.OrderItem) []OrderExpr {
	out := make([]OrderExpr, 0, len(items))
	for _, it := range items {
		dir := it.Direction
		if dir == "" {
			dir = "ASC"
		}
		out = append(out, OrderExpr{Ref: it.Field, Direction: dir})
	}
	return out
}

// field looks up a field within a dataset, returning a descriptive error if
// either is unknown.
func (idx *index) field(dataset, name string) (*model.Field, error) {
	fm, ok := idx.fields[dataset]
	if !ok {
		return nil, fmt.Errorf("plan: unknown dataset %q", dataset)
	}
	f, ok := fm[name]
	if !ok {
		return nil, fmt.Errorf("plan: unknown field %q in dataset %q", name, dataset)
	}
	return f, nil
}

// referencedDatasets returns the datasets touched by the query, in first-seen
// order. The first element is the root for join resolution: it is the source
// dataset of the first metric, or the first dimension when there are no
// metrics.
func referencedDatasets(q *query.Query) []string {
	seen := map[string]bool{}
	var out []string
	addRef := func(ref string) {
		if ds, _, ok := splitRef(ref); ok && !seen[ds] {
			seen[ds] = true
			out = append(out, ds)
		}
	}

	for _, ref := range q.Metrics {
		addRef(ref)
	}
	for _, ref := range q.Dimensions {
		addRef(ref)
	}
	for _, f := range q.Filters {
		addRef(f.Field)
	}
	for _, h := range q.Having {
		addRef(h.Metric)
	}
	for _, o := range q.OrderBy {
		addRef(o.Field)
	}
	return out
}

// splitRef splits a "dataset.name" reference into its parts.
func splitRef(ref string) (dataset, name string, ok bool) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
