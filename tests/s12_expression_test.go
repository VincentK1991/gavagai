package tests

import "testing"

// §12 — Expression graph / derived expressions

// §12 — nested expression reference: status_label_upper is declared as
// UPPER(${status_label}); model loading expands the reference to the target
// field's (parenthesised) expression before planning.
func TestNestedExprRef(t *testing.T) {
	sql := compileSQL(t, "ecommerce.yaml", "s12_nested_expr_ref.json")
	assertContains(t, sql, "UPPER((CASE WHEN status = 'complete' THEN 'done' ELSE 'pending' END))")
	assertNotContains(t, sql, "${")
}
