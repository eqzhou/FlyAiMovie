package textutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "returns first non-empty value", values: []string{"", "a", "b"}, want: "a"},
		{name: "returns empty string when all values are empty", values: []string{"", ""}, want: ""},
		{name: "returns empty string when no values are given", values: nil, want: ""},
		{name: "treats whitespace-only as non-empty", values: []string{" ", "a"}, want: " "},
		{name: "preserves surrounding whitespace of the winner", values: []string{"", " a "}, want: " a "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstNonEmpty(tc.values...); got != tc.want {
				t.Fatalf("FirstNonEmpty(%q) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

func TestFirstNonBlank(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "returns first non-blank value", values: []string{"", "a", "b"}, want: "a"},
		{name: "skips whitespace-only values", values: []string{" ", "\t\n", "a"}, want: "a"},
		{name: "returns empty string when all values are blank", values: []string{"", " ", "\t"}, want: ""},
		{name: "returns empty string when no values are given", values: nil, want: ""},
		{name: "returns the winner untrimmed", values: []string{" ", " a "}, want: " a "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstNonBlank(tc.values...); got != tc.want {
				t.Fatalf("FirstNonBlank(%q) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// TestSemanticsDiffer guards the reason both helpers exist: a whitespace-only
// candidate wins under FirstNonEmpty but is skipped by FirstNonBlank.
func TestSemanticsDiffer(t *testing.T) {
	values := []string{" ", "fallback"}
	if got := FirstNonEmpty(values...); got != " " {
		t.Fatalf("FirstNonEmpty(%q) = %q, want %q", values, got, " ")
	}
	if got := FirstNonBlank(values...); got != "fallback" {
		t.Fatalf("FirstNonBlank(%q) = %q, want %q", values, got, "fallback")
	}
}
