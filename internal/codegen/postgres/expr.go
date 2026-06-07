package postgres

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// quoteIdent wraps s in PostgreSQL double-quote delimiters, doubling any
// embedded double-quote characters per the SQL standard.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// renderPredicate converts a filter predicate to a SQL boolean expression.
// expr is the already-resolved field SQL fragment (from SelectExpression).
// val is nil for IS NULL / IS NOT NULL operators.
func renderPredicate(expr, op string, val json.RawMessage) (string, error) {
	switch op {
	case "IS NULL":
		return expr + " IS NULL", nil
	case "IS NOT NULL":
		return expr + " IS NOT NULL", nil
	case "IN", "NOT IN":
		lit, err := renderArray(val)
		if err != nil {
			return "", fmt.Errorf("postgres: rendering %s value: %w", op, err)
		}
		return fmt.Sprintf("%s %s (%s)", expr, op, lit), nil
	default:
		lit, err := renderScalar(val)
		if err != nil {
			return "", fmt.Errorf("postgres: rendering %s value: %w", op, err)
		}
		return fmt.Sprintf("%s %s %s", expr, op, lit), nil
	}
}

// renderScalar converts a single JSON value to a PostgreSQL literal.
func renderScalar(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "NULL", nil
	}

	// String → single-quoted, with internal single-quotes doubled.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", nil
	}

	// Number → pass the raw JSON token directly; it is already a valid SQL
	// numeric literal (integers and decimals, no scientific notation in our IR).
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), nil
	}

	// Boolean → SQL TRUE / FALSE.
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return "TRUE", nil
		}
		return "FALSE", nil
	}

	return "", fmt.Errorf("postgres: cannot render scalar JSON value: %s", raw)
}

// renderArray converts a JSON array to a comma-separated list of SQL literals
// for use inside an IN / NOT IN clause.
func renderArray(raw json.RawMessage) (string, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return "", fmt.Errorf("expected a JSON array, got: %s", raw)
	}
	parts := make([]string, 0, len(arr))
	for _, el := range arr {
		lit, err := renderScalar(el)
		if err != nil {
			return "", err
		}
		parts = append(parts, lit)
	}
	return strings.Join(parts, ", "), nil
}

// formatFloat renders a float64 HAVING threshold as a SQL number literal.
// Whole-number values are rendered without a decimal point (e.g. 1000 not
// 1000.000000); fractional values use the minimum necessary digits.
func formatFloat(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
