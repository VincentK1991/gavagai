package model

import "fmt"

// ValidationError describes a single structural problem in a semantic model.
// Path locates the offending element (e.g. "datasets[0].fields[1]").
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// Validate checks a semantic model for structural correctness: required
// fields are present, dataset names are unique, field and metric expressions
// carry at least one dialect, and relationships reference existing datasets.
// It returns every problem found rather than stopping at the first.
func Validate(m *SemanticModel) []ValidationError {
	var errs []ValidationError
	add := func(path, msg string) {
		errs = append(errs, ValidationError{Path: path, Message: msg})
	}

	if m.Name == "" {
		add("model", "name is required")
	}
	if len(m.Datasets) == 0 {
		add("model", "at least one dataset is required")
	}

	datasetNames := make(map[string]bool, len(m.Datasets))
	for i := range m.Datasets {
		ds := &m.Datasets[i]
		path := fmt.Sprintf("datasets[%d]", i)

		if ds.Name == "" {
			add(path, "dataset name is required")
		} else {
			if datasetNames[ds.Name] {
				add(path, fmt.Sprintf("duplicate dataset name %q", ds.Name))
			}
			datasetNames[ds.Name] = true
		}
		if ds.Source == "" {
			add(path, "dataset source is required")
		}

		for j := range ds.Fields {
			f := &ds.Fields[j]
			fpath := fmt.Sprintf("%s.fields[%d]", path, j)
			if f.Name == "" {
				add(fpath, "field name is required")
			}
			if len(f.Expression.Dialects) == 0 {
				add(fpath, "field expression must define at least one dialect")
			}
		}
	}

	for i := range m.Metrics {
		mt := &m.Metrics[i]
		path := fmt.Sprintf("metrics[%d]", i)
		if mt.Name == "" {
			add(path, "metric name is required")
		}
		if len(mt.Expression.Dialects) == 0 {
			add(path, "metric expression must define at least one dialect")
		}
	}

	for i := range m.Relationships {
		r := &m.Relationships[i]
		path := fmt.Sprintf("relationships[%d]", i)
		if r.Name == "" {
			add(path, "relationship name is required")
		}
		validateRelationshipEndpoint(add, path, "from", r.From, r.FromColumns, datasetNames)
		validateRelationshipEndpoint(add, path, "to", r.To, r.ToColumns, datasetNames)

		if len(r.FromColumns) != len(r.ToColumns) && len(r.FromColumns) > 0 && len(r.ToColumns) > 0 {
			add(path, "from_columns and to_columns must have equal length")
		}
	}

	return errs
}

// validateRelationshipEndpoint checks one side (from/to) of a relationship:
// the dataset reference is present and known, and its join columns are given.
func validateRelationshipEndpoint(
	add func(path, msg string),
	path, side, dataset string,
	columns []string,
	known map[string]bool,
) {
	switch {
	case dataset == "":
		add(path, fmt.Sprintf("%s dataset is required", side))
	case !known[dataset]:
		add(path, fmt.Sprintf("%s references unknown dataset %q", side, dataset))
	}
	if len(columns) == 0 {
		add(path, fmt.Sprintf("%s_columns must list at least one column", side))
	}
}
