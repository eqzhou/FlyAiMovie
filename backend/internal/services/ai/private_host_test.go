package ai

import "testing"

// providerPrivateHostAllowed gates the one path that lets a provider URL point
// at a private address. validateProviderURL reaches it only after a custom CA
// is configured, so the existing URL tests short-circuit before this runs;
// these exercise the allowlist parsing directly.

func TestProviderPrivateHostAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		allow   string
		host    string
		allowed bool
	}{
		{name: "exact match", allow: "10.0.0.5", host: "10.0.0.5", allowed: true},
		{name: "host not listed", allow: "10.0.0.5", host: "10.0.0.6", allowed: false},
		{name: "empty allowlist denies", allow: "", host: "10.0.0.5", allowed: false},
		{name: "one of several entries", allow: "a.internal,10.0.0.5,b.internal", host: "10.0.0.5", allowed: true},
		{name: "entry whitespace is trimmed", allow: " 10.0.0.5 , b.internal", host: "10.0.0.5", allowed: true},
		{name: "entry casing is ignored", allow: "Host.Internal", host: "host.internal", allowed: true},
		{name: "queried host casing is ignored", allow: "host.internal", host: "HOST.INTERNAL", allowed: true},
		// A trailing dot is a valid FQDN form, so both sides are normalized to
		// keep "host.internal." from bypassing an allowlist of "host.internal".
		{name: "trailing dot on queried host", allow: "host.internal", host: "host.internal.", allowed: true},
		{name: "trailing dot on allowlist entry", allow: "host.internal.", host: "host.internal", allowed: true},
		// Substrings must not match: an allowlist is an exact-host list.
		{name: "suffix is not a match", allow: "host.internal", host: "evil-host.internal", allowed: false},
		{name: "prefix is not a match", allow: "host.internal", host: "host.internal.evil.com", allowed: false},
		// Splitting "," yields empty entries, which do match an empty host.
		// Unreachable in practice: validateProviderURL rejects a URL with no
		// host before this is consulted. Pinned so the quirk stays visible.
		{name: "empty entry matches empty host", allow: ",", host: "", allowed: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", tc.allow)
			if got := providerPrivateHostAllowed(tc.host); got != tc.allowed {
				t.Fatalf("providerPrivateHostAllowed(%q) with allowlist %q = %v, want %v",
					tc.host, tc.allow, got, tc.allowed)
			}
		})
	}
}
