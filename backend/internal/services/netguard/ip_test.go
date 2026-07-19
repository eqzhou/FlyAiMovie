package netguard

import (
	"net"
	"testing"
)

func TestUnsafeIPRejectsPrivateSharedBenchmarkAndReservedRanges(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.1", "100.64.0.1", "192.0.0.8", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "::1",
		"100::1", "2001:2::1", "2001:db8::1",
	} {
		if !IsUnsafeIP(net.ParseIP(raw)) {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if IsUnsafeIP(net.ParseIP(raw)) {
			t.Fatalf("public address %s was rejected", raw)
		}
	}
}
