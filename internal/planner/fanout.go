package planner

import (
	"fmt"
	"strings"

	"github.com/vincentk1991/gavagai/internal/model"
)

// FanOutError reports that a metric would be double-counted by a join. It is
// returned by Plan when an aggregate that is not robust to row duplication is
// computed over a dataset whose rows are multiplied by a one-to-many join.
type FanOutError struct {
	MetricRef string              // the offending metric reference, e.g. "orders.revenue"
	Dataset   string              // the metric's source dataset (the "one" side)
	Rel       *model.Relationship // the join that causes the duplication
}

func (e *FanOutError) Error() string {
	return fmt.Sprintf(
		"fan-out detected: metric %q (aggregate over dataset %q) would double-count because "+
			"the join to %q via relationship %q (%s.%v = %s.%v) duplicates each %q row; "+
			"remove the %q reference from the query or use a fan-out-safe metric "+
			"(COUNT DISTINCT / MIN / MAX)",
		e.MetricRef, e.Dataset,
		e.Rel.From, e.Rel.Name, e.Rel.From, e.Rel.FromColumns, e.Rel.To, e.Rel.ToColumns,
		e.Dataset, e.Rel.From,
	)
}

// detectFanOut returns a FanOutError if any unsafe metric is aggregated over a
// dataset that sits on the "one" side of a join edge in use. A relationship's
// To side is the one side (a unique/primary key); when it is joined to its
// From (many) side, the one-side rows are duplicated.
func detectFanOut(metrics []MetricExpr, edges []*model.Relationship) error {
	for _, me := range metrics {
		if metricFanOutSafe(me.Metric) {
			continue
		}
		for _, e := range edges {
			if e.To == me.Dataset {
				return &FanOutError{MetricRef: me.Ref, Dataset: me.Dataset, Rel: e}
			}
		}
	}
	return nil
}

// metricFanOutSafe reports whether a metric's aggregate is robust to row
// duplication. COUNT(DISTINCT ...), MIN(...) and MAX(...) are safe; SUM, AVG
// and plain COUNT are not.
func metricFanOutSafe(m *model.Metric) bool {
	expr := strings.ToUpper(strings.TrimSpace(ansiExpression(m.Expression)))
	// Normalise whitespace after the opening paren, e.g. "COUNT( DISTINCT".
	expr = strings.ReplaceAll(expr, "( ", "(")
	switch {
	case strings.HasPrefix(expr, "COUNT(DISTINCT"):
		return true
	case strings.HasPrefix(expr, "MIN("):
		return true
	case strings.HasPrefix(expr, "MAX("):
		return true
	default:
		return false
	}
}

// ansiExpression returns the ANSI_SQL dialect expression if present, otherwise
// the first available dialect expression, otherwise the empty string.
func ansiExpression(e model.Expression) string {
	for _, d := range e.Dialects {
		if strings.EqualFold(d.Dialect, "ANSI_SQL") {
			return d.Expression
		}
	}
	if len(e.Dialects) > 0 {
		return e.Dialects[0].Expression
	}
	return ""
}
