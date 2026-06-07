package tests

import "testing"

// §12 — Expression graph / derived expressions

// §12 — PENDING: nested expression reference (derived field referencing another field's expression)
func TestNestedExprRef(t *testing.T) {
	pendingTest(t, "12", "nested-expr-ref",
		"nested expression graph (field referencing another field's expression) not yet implemented")
	_ = loadQueryRaw(t, "s12_nested_expr_ref.json")
}
