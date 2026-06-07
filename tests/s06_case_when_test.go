package tests

import "testing"

// §6 — CASE WHEN expressions

// §6.1 CAN TEST NOW: basic CASE WHEN dimension passes through verbatim
func TestCaseBasicLabel(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_nested_case.json")
	// The nested CASE WHEN expression should appear in the SELECT / GROUP BY
	assertContains(t, sql, "CASE WHEN")
}

// §6.1 CAN TEST NOW: CASE WHEN IS NULL branch renders correctly
func TestCaseNullBranch(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_case_null_branch.json")
	assertContains(t, sql, "CASE WHEN")
	assertContains(t, sql, "IS NULL")
}

// §6.3 CAN TEST NOW: CASE WHEN boolean flag used as a filter operand
func TestCaseBooleanFlag(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s06_case_boolean_flag.json")
	// The is_large_order expression should be pushed down as a WHERE clause
	assertContains(t, sql, "CASE WHEN")
	assertContains(t, sql, "WHERE")
}
