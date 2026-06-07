package pretty

import "testing"

func TestCompact(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "multiline collapses to single spaces",
			in:   "SELECT\n  a,\n  b\nFROM t\n",
			want: "SELECT a, b FROM t",
		},
		{
			name: "leading and trailing whitespace trimmed",
			in:   "  \n SELECT 1 \n ",
			want: "SELECT 1",
		},
		{
			name: "single space inside literal preserved",
			in:   "WHERE region = 'North America'",
			want: "WHERE region = 'North America'",
		},
		{
			name: "multiple spaces inside literal preserved",
			in:   "WHERE x = 'a  b'\nAND y = 1",
			want: "WHERE x = 'a  b' AND y = 1",
		},
		{
			name: "newline inside literal preserved",
			in:   "WHERE x = 'line1\nline2'",
			want: "WHERE x = 'line1\nline2'",
		},
		{
			name: "escaped quote does not end the literal",
			in:   "WHERE x = 'O''Brien   Inc'  AND  y = 2",
			want: "WHERE x = 'O''Brien   Inc' AND y = 2",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compact(tc.in); got != tc.want {
				t.Errorf("Compact(%q)\n  want %q\n  got  %q", tc.in, tc.want, got)
			}
		})
	}
}
