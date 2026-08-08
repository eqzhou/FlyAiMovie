package textutil

import (
	"reflect"
	"strings"
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
	// The raw decode error is returned unwrapped: callers add their own prefix,
	// and a sentinel here would show up as a second layer in client messages.
	if !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("error %v does not carry the underlying JSON message", err)
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

func TestJSONValueConversions(t *testing.T) {
	if AsString(nil, false) != "" || AsString("ok", false) != "ok" || AsString(float64(1.5), false) != "" {
		t.Fatal("strict string conversion failed")
	}
	if AsString(float64(1.5), true) != "1.5" {
		t.Fatal("numeric string conversion failed")
	}
	values := []struct {
		value any
		want  int
	}{
		{uint(1), 1}, {uint64(2), 2}, {int64(3), 3}, {float64(4.9), 4}, {5, 5}, {"6", 6}, {"6x", 6}, {true, 0},
	}
	for _, tc := range values {
		if got := AsInt(tc.value, true); got != tc.want {
			t.Fatalf("AsInt(%T(%v))=%d, want %d", tc.value, tc.value, got, tc.want)
		}
	}
	if AsInt("6x", false) != 0 || AsUint("4", false) != 4 {
		t.Fatal("strict integer conversion failed")
	}
	for _, value := range []any{uint(1), uint64(2), int64(3)} {
		if got := AsInt(value, false); got != 0 {
			t.Fatalf("strict AsInt(%T(%v))=%d, want 0 to preserve HTTP parsing semantics", value, value, got)
		}
	}
	if got := UniquePositiveIDs([]any{float64(2), 2, 0, float64(2), "3"}, true); !reflect.DeepEqual(got, []uint{2, 3}) {
		t.Fatalf("UniquePositiveIDs=%v", got)
	}
}
