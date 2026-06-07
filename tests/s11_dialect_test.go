package tests

import "testing"

// §11 — Dialect-specific rewrites

// §11.6 — UNNEST (PENDING: UNNEST / array unnesting not yet expressible in query IR)
func TestUnnest(t *testing.T) {
	pendingTest(t, "11.6", "unnest", "UNNEST array expansion not yet expressible in query IR")
	_ = loadQueryRaw(t, "s11_unnest.json")
}

func TestUnnestOrdinality(t *testing.T) {
	pendingTest(t, "11.6", "unnest-ordinality",
		"UNNEST WITH ORDINALITY / UNNEST(...) WITH OFFSET not yet expressible in query IR")
	_ = loadQueryRaw(t, "s11_unnest_ordinality.json")
}
