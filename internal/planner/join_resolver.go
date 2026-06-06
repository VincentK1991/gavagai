package planner

import (
	"fmt"

	"github.com/vincentk1991/gavagai/internal/model"
)

// resolveJoins builds a join tree connecting every referenced dataset. The
// first element of refDatasets is the root (the fact/grain), placed on the
// left of every join so that LEFT joins preserve its rows. It returns the
// tree, the relationships used as join edges (for fan-out analysis), and an
// error if the datasets cannot all be connected.
//
// Multi-hop joins are supported: intermediate datasets on the shortest path
// from the root to a referenced dataset are pulled into the tree.
func resolveJoins(refDatasets []string, m *model.SemanticModel) (PlanNode, []*model.Relationship, error) {
	if len(refDatasets) == 0 {
		return nil, nil, fmt.Errorf("plan: query references no datasets")
	}

	dsByName := make(map[string]*model.Dataset, len(m.Datasets))
	for i := range m.Datasets {
		dsByName[m.Datasets[i].Name] = &m.Datasets[i]
	}

	root := refDatasets[0]
	rootDS, ok := dsByName[root]
	if !ok {
		return nil, nil, fmt.Errorf("plan: unknown dataset %q", root)
	}

	if len(refDatasets) == 1 {
		return &ScanNode{Dataset: rootDS, Alias: root}, nil, nil
	}

	parent, parentRel, order := bfs(root, m)

	// Determine which datasets are needed: the root plus every dataset on a
	// shortest path from the root to a referenced dataset.
	needed := map[string]bool{root: true}
	for _, d := range refDatasets {
		if d == root {
			continue
		}
		if _, reachable := parent[d]; !reachable && d != root {
			return nil, nil, fmt.Errorf(
				"plan: cannot resolve joins: no relationship connects dataset %q to %q", d, root)
		}
		for cur := d; cur != root; cur = parent[cur] {
			needed[cur] = true
		}
	}

	// Build the tree in BFS order so each added dataset attaches to a dataset
	// already present in the tree.
	tree := PlanNode(&ScanNode{Dataset: rootDS, Alias: root})
	var used []*model.Relationship
	for _, d := range order {
		if d == root || !needed[d] {
			continue
		}
		rel := parentRel[d]
		tree = &JoinNode{
			Left:         tree,
			Right:        &ScanNode{Dataset: dsByName[d], Alias: d},
			On:           joinConditions(rel),
			Kind:         LeftJoin,
			Relationship: rel,
		}
		used = append(used, rel)
	}

	return tree, used, nil
}

// bfs traverses the relationship graph from root, returning parent pointers,
// the relationship used to reach each dataset, and the visitation order.
// Neighbours are visited in relationship-declaration order for determinism.
func bfs(root string, m *model.SemanticModel) (parent map[string]string, parentRel map[string]*model.Relationship, order []string) {
	type edge struct {
		to  string
		rel *model.Relationship
	}
	adj := make(map[string][]edge)
	for i := range m.Relationships {
		r := &m.Relationships[i]
		adj[r.From] = append(adj[r.From], edge{to: r.To, rel: r})
		adj[r.To] = append(adj[r.To], edge{to: r.From, rel: r})
	}

	parent = make(map[string]string)
	parentRel = make(map[string]*model.Relationship)
	visited := map[string]bool{root: true}
	queue := []string{root}
	order = []string{root}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range adj[cur] {
			if visited[e.to] {
				continue
			}
			visited[e.to] = true
			parent[e.to] = cur
			parentRel[e.to] = e.rel
			queue = append(queue, e.to)
			order = append(order, e.to)
		}
	}
	return parent, parentRel, order
}

// joinConditions builds the equality conditions for a relationship from its
// positional from/to column lists.
func joinConditions(r *model.Relationship) []JoinCondition {
	n := len(r.FromColumns)
	if len(r.ToColumns) < n {
		n = len(r.ToColumns)
	}
	conds := make([]JoinCondition, 0, n)
	for i := 0; i < n; i++ {
		conds = append(conds, JoinCondition{
			Left:  ColumnRef{Dataset: r.From, Column: r.FromColumns[i]},
			Right: ColumnRef{Dataset: r.To, Column: r.ToColumns[i]},
		})
	}
	return conds
}
