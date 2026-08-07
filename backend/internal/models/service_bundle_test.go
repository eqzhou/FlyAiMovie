package models

import "testing"

// Bundle templates are shared, credential-free scaffolding: the API fills in
// secrets per request and must never persist them here. BeforeSave is the only
// thing standing between a malformed template and a stored secret, so these
// cases pin both the accept and the reject side.

func TestBeforeSaveRejectsInvalidJSON(t *testing.T) {
	bundle := &AIServiceBundle{ServicesJSON: `[{"provider":"openai"`}
	err := bundle.BeforeSave(nil)
	if err == nil {
		t.Fatal("malformed ServicesJSON was accepted")
	}
	if got := err.Error(); got != `invalid service bundle JSON: unexpected end of JSON input` {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestBeforeSaveAcceptsCredentialFreeTemplate(t *testing.T) {
	bundle := &AIServiceBundle{ServicesJSON: `[
		{"service_type":"text","provider":"openai","name":"GPT","base_url":"https://api.openai.com","model":"gpt-4o"},
		{"service_type":"image","provider":"openai","name":"Images","base_url":"https://api.openai.com","model":"dall-e-3"}
	]`}
	if err := bundle.BeforeSave(nil); err != nil {
		t.Fatalf("credential-free template rejected: %v", err)
	}
}

func TestBeforeSaveRejectsCredentialFields(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{name: "api_key at top level", json: `{"api_key":"sk-live"}`},
		{name: "api_key inside array element", json: `[{"provider":"openai","api_key":"sk-live"}]`},
		{name: "credentials key", json: `[{"credentials":{"text":"sk-live"}}]`},
		{name: "secret key", json: `[{"secret":"shh"}]`},
		{name: "token key", json: `[{"token":"abc"}]`},
		{name: "key casing is ignored", json: `[{"API_Key":"sk-live"}]`},
		{name: "surrounding whitespace is ignored", json: `[{" api_key ":"sk-live"}]`},
		{name: "nested deep in objects", json: `{"a":{"b":{"c":[{"token":"t"}]}}}`},
		// settings arrives as a JSON-encoded string, so a secret can hide one
		// level deeper than a plain walk of the outer document would reach.
		{name: "credential inside encoded settings string", json: `[{"settings":"{\"api_key\":\"sk-live\"}"}]`},
		{name: "credential nested inside encoded settings", json: `[{"settings":"{\"outer\":{\"secret\":\"s\"}}"}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := &AIServiceBundle{ServicesJSON: tc.json}
			err := bundle.BeforeSave(nil)
			if err == nil {
				t.Fatal("credential-bearing template was accepted")
			}
			if got := err.Error(); got != "service bundle templates must not contain credentials" {
				t.Fatalf("unexpected message: %q", got)
			}
		})
	}
}

func TestBeforeSaveAllowsBenignSettingsString(t *testing.T) {
	// settings is free-form, so a non-credential payload must still pass, and a
	// non-JSON string must not be mistaken for a nested document.
	for _, raw := range []string{
		`[{"settings":"{\"quality\":\"hd\"}"}]`,
		`[{"settings":"not json at all"}]`,
		`[{"settings":""}]`,
	} {
		bundle := &AIServiceBundle{ServicesJSON: raw}
		if err := bundle.BeforeSave(nil); err != nil {
			t.Fatalf("ServicesJSON %s rejected: %v", raw, err)
		}
	}
}

func TestContainsCredentialFieldIgnoresValues(t *testing.T) {
	// Only key names are credential markers. A value that merely looks like a
	// secret must not trip the guard, otherwise legitimate prompts and model
	// names become unsavable.
	if containsCredentialField(map[string]any{"model": "api_key"}) {
		t.Fatal("a value named api_key was treated as a credential key")
	}
	if containsCredentialField([]any{"secret", "token"}) {
		t.Fatal("string elements were treated as credential keys")
	}
	if containsCredentialField(nil) {
		t.Fatal("nil was treated as containing a credential")
	}
}
