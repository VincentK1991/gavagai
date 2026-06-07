// Package pretty post-processes emitted SQL for display. The emitters produce
// canonical multi-line SQL; Compact collapses it to a single line for machine
// consumption (the CLI default), while --pretty leaves the multi-line form.
package pretty

import "strings"

// Compact collapses every run of whitespace (spaces, tabs, newlines) that lies
// outside a single-quoted string literal into a single space, and trims the
// result, yielding a one-line statement. Whitespace inside string literals is
// preserved verbatim, so values such as 'North  America' survive intact.
// SQL escapes an embedded quote by doubling it (”) — handled here so an
// escaped quote does not prematurely end a literal.
func Compact(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))

	inString := false
	pendingSpace := false

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		if inString {
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' { // escaped quote
					b.WriteByte('\'')
					i++
					continue
				}
				inString = false
			}
			continue
		}

		switch c {
		case ' ', '\t', '\n', '\r':
			pendingSpace = true
		case '\'':
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			inString = true
			b.WriteByte(c)
		default:
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteByte(c)
		}
	}

	return b.String()
}
