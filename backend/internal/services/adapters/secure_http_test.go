package adapters

import (
	"net"
	"testing"
)

func TestUnsafeProviderIPRejectsPrivateAndSpecialRanges(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "::1", "0.0.0.0"} {
		if !isUnsafeProviderIP(net.ParseIP(raw)) {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
	if isUnsafeProviderIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was rejected")
	}
}
