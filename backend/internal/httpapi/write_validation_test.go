package httpapi

import "testing"

func TestWriteValidationHelpers(t *testing.T) {
	if got, ok := positiveJSONInt(float64(3)); !ok || got != 3 {
		t.Fatalf("positive int: %d %v", got, ok)
	}
	for _, v := range []any{float64(0), float64(-1), 1.5, "3"} {
		if _, ok := positiveJSONInt(v); ok {
			t.Errorf("accepted invalid positive value %#v", v)
		}
	}
	if got, ok := nonNegativeJSONInt(float64(0)); !ok || got != 0 {
		t.Fatalf("nonnegative: %d %v", got, ok)
	}
	if _, ok := nonNegativeJSONInt(float64(-1)); ok {
		t.Fatal("accepted negative value")
	}

	body := map[string]any{"name": "ok", "missing": 1, "bad": 2, "long": "x"}
	if got, ok, err := stringUpdate(body, "name", 4); err != nil || !ok || got != "ok" {
		t.Fatalf("string update: %q %v %v", got, ok, err)
	}
	if _, ok, err := stringUpdate(body, "missing-key", 4); err != nil || ok {
		t.Fatalf("missing key: %v %v", ok, err)
	}
	if _, _, err := stringUpdate(body, "bad", 4); err == nil {
		t.Fatal("accepted non-string")
	}
	if _, _, err := stringUpdate(map[string]any{"x": "long"}, "x", 2); err == nil {
		t.Fatal("accepted overlong value")
	}

	ids, ok := positiveJSONIDs([]any{float64(2), float64(2), float64(4)})
	if !ok || len(ids) != 2 || ids[0] != 2 || ids[1] != 4 {
		t.Fatalf("ids=%v ok=%v", ids, ok)
	}
	if _, ok := positiveJSONIDs([]any{float64(0)}); ok {
		t.Fatal("accepted invalid ids")
	}
}
