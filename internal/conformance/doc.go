// Package conformance holds the executable gate for docs/pushdown-checklist.md.
//
// It owns no production code. Each checklist item maps to a subtest
// (named by its checklist id, e.g. "1.1/=") that runs the real pipeline
// — parse → validate → plan → pushdown → codegen — over an inline
// (semantic model, query) fixture and asserts the rewrite or SQL behaviour
// the box describes.
//
// A box is "checkable" when its subtest passes. A subtest that calls
// pending() is SKIPPED: the capability is not implemented yet (or the query
// IR cannot yet express it). Turning a box green is a two-step ritual:
// implement the behaviour, then delete the pending() call so the assertion
// runs. The package keeps a doc.go so `go build ./...` sees a non-test file.
package conformance
