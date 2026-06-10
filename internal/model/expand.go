package model

import (
	"fmt"
	"strings"
)

// refOpen/refClose delimit a ${field_name} reference inside a field
// expression. expandTokens scans for them by hand, which keeps the errors
// simple and avoids a regexp dependency.
const refOpen, refClose = "${", "}"

// ExpandFieldRefs rewrites every ${field_name} token in a dataset's field
// expressions to the referenced field's expression for the same dialect
// (falling back to ANSI_SQL), wrapped in parentheses. References resolve
// within the same dataset only and may nest; cycles and unknown names are
// errors. The model is modified in place; calling it again is a no-op since
// no ${...} tokens remain.
//
// This is the "nested expression" capability (checklist §12): a field like
//
//	net_label: UPPER(${status_label})
//
// expands to UPPER((CASE WHEN status = 'complete' THEN 'done' ... END))
// before planning, so the planner and emitters see plain expressions.
func ExpandFieldRefs(m *SemanticModel) error {
	for di := range m.Datasets {
		ds := &m.Datasets[di]
		byName := make(map[string]*Field, len(ds.Fields))
		for fi := range ds.Fields {
			byName[ds.Fields[fi].Name] = &ds.Fields[fi]
		}
		for fi := range ds.Fields {
			f := &ds.Fields[fi]
			for ei := range f.Expression.Dialects {
				entry := &f.Expression.Dialects[ei]
				expanded, err := expandTokens(entry.Expression, entry.Dialect, byName, map[string]bool{f.Name: true})
				if err != nil {
					return fmt.Errorf("dataset %q field %q (%s): %w", ds.Name, f.Name, entry.Dialect, err)
				}
				entry.Expression = expanded
			}
		}
	}
	return nil
}

// expandTokens replaces each ${name} in expr, recursing into the referenced
// expression. visiting carries the chain of field names currently being
// expanded, for cycle detection.
func expandTokens(expr, dialect string, byName map[string]*Field, visiting map[string]bool) (string, error) {
	var out strings.Builder
	rest := expr
	for {
		start := strings.Index(rest, refOpen)
		if start < 0 {
			out.WriteString(rest)
			return out.String(), nil
		}
		end := strings.Index(rest[start:], refClose)
		if end < 0 {
			return "", fmt.Errorf("unterminated ${...} reference in %q", expr)
		}
		name := rest[start+len(refOpen) : start+end]
		if name == "" {
			return "", fmt.Errorf("empty ${} reference in %q", expr)
		}

		out.WriteString(rest[:start])
		rest = rest[start+end+len(refClose):]

		if visiting[name] {
			return "", fmt.Errorf("cyclic field reference through %q", name)
		}
		ref, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("reference ${%s} names no field in this dataset", name)
		}
		sub, err := dialectEntry(ref.Expression, dialect)
		if err != nil {
			return "", fmt.Errorf("reference ${%s}: %w", name, err)
		}
		visiting[name] = true
		inner, err := expandTokens(sub, dialect, byName, visiting)
		delete(visiting, name)
		if err != nil {
			return "", err
		}
		// Parenthesise so the spliced expression binds as a unit, e.g.
		// ${net} * 2 with net = price - discount -> (price - discount) * 2.
		out.WriteString("(" + inner + ")")
	}
}

// dialectEntry returns the expression for the requested dialect, falling back
// to ANSI_SQL — the same selection rule codegen.SelectExpression applies.
func dialectEntry(e Expression, dialect string) (string, error) {
	for _, d := range e.Dialects {
		if strings.EqualFold(d.Dialect, dialect) {
			return d.Expression, nil
		}
	}
	for _, d := range e.Dialects {
		if strings.EqualFold(d.Dialect, "ANSI_SQL") {
			return d.Expression, nil
		}
	}
	return "", fmt.Errorf("no %s or ANSI_SQL expression on the referenced field", dialect)
}
