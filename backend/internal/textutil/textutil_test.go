package textutil

import (
	"errors"
	"reflect"
	"testing"
)

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

func TestParseStringList(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty input yields empty slice", raw: "", want: []string{}},
		{name: "whitespace-only input yields empty slice", raw: "   ", want: []string{}},
		{name: "decodes JSON array", raw: `["a","b"]`, want: []string{"a", "b"}},
		{name: "empty JSON array yields empty slice", raw: `[]`, want: []string{}},
		{name: "trims items inside JSON array", raw: `[" a ","b"]`, want: []string{"a", "b"}},
		{name: "drops blank items inside JSON array", raw: `["a","","  "]`, want: []string{"a"}},
		{name: "splits comma-separated value", raw: "a,b", want: []string{"a", "b"}},
		{name: "trims comma-separated items", raw: " a , b ", want: []string{"a", "b"}},
		{name: "drops blank comma-separated items", raw: "a,,b", want: []string{"a", "b"}},
		{name: "single value becomes one item", raw: "only", want: []string{"only"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseStringList(tc.raw)
			if err != nil {
				t.Fatalf("ParseStringList(%q) returned error: %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseStringList(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseStringListNeverReturnsNilOnSuccess(t *testing.T) {
	for _, raw := range []string{"", "  ", "[]", "a"} {
		got, err := ParseStringList(raw)
		if err != nil {
			t.Fatalf("ParseStringList(%q) returned error: %v", raw, err)
		}
		if got == nil {
			t.Fatalf("ParseStringList(%q) returned nil slice, want non-nil", raw)
		}
	}
}

func TestParseStringListRejectsMalformedJSON(t *testing.T) {
	got, err := ParseStringList(`["a"`)
	if err == nil {
		t.Fatalf("ParseStringList did not report malformed JSON, got %#v", got)
	}
	if !errors.Is(err, ErrInvalidJSONList) {
		t.Fatalf("error %v does not match ErrInvalidJSONList", err)
	}
	if got != nil {
		t.Fatalf("ParseStringList returned %#v on error, want nil", got)
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
