package adapters

import (
	"context"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnsafeProviderIPRejectsPrivateAndSpecialRanges(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "100.64.0.1", "198.18.0.1", "192.0.2.1", "169.254.169.254", "::1", "0.0.0.0"} {
		if !isUnsafeProviderIP(net.ParseIP(raw)) {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
	if isUnsafeProviderIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was rejected")
	}
}

func TestProviderHTTPClientRejectsReservedLiteralAddress(t *testing.T) {
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "")
	t.Setenv("AI_PROVIDER_CA_FILE", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://100.100.100.200/latest/meta-data", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = providerHTTPClient(time.Second).Do(req)
	if err == nil || !strings.Contains(err.Error(), "provider address is not allowed") {
		t.Fatalf("reserved address was not rejected at dial time: %v", err)
	}
}

func TestProviderHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer source.Close()

	response, err := providerHTTPClient(2 * time.Second).Get(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if targetRequests.Load() != 0 {
		t.Fatal("provider redirect reached its target")
	}
}

func TestPrivateProviderHostnameRequiresExactAllowlistAndCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	certificate := server.Certificate()
	caFile := t.TempDir() + "/provider-ca.pem"
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_PROVIDER_CA_FILE", caFile)
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "model.internal.example")

	if !providerPrivateHostAllowed("MODEL.INTERNAL.EXAMPLE.") {
		t.Fatal("exact private hostname with a valid custom CA was not allowed")
	}
	if providerPrivateHostAllowed("other.internal.example") {
		t.Fatal("non-allowlisted private hostname was allowed")
	}
	t.Setenv("AI_PROVIDER_CA_FILE", caFile+".missing")
	if providerPrivateHostAllowed("model.internal.example") {
		t.Fatal("private hostname was allowed without a valid custom CA")
	}
}

func TestHasSafeProviderIP(t *testing.T) {
	if hasSafeProviderIP([]net.IP{net.ParseIP("198.18.0.44")}) {
		t.Fatal("benchmarking range should not count as safe")
	}
	if !hasSafeProviderIP([]net.IP{net.ParseIP("198.18.0.44"), net.ParseIP("8.8.8.8")}) {
		t.Fatal("public address should count as safe")
	}
}

func TestLookupIPsViaPublicDNSReturnsGlobalAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ips, err := lookupIPsViaPublicDNS(ctx, "one.one.one.one")
	if err != nil {
		t.Skipf("public DNS unavailable in this environment: %v", err)
	}
	if !hasSafeProviderIP(ips) {
		t.Fatalf("public DNS returned only unsafe addresses: %v", ips)
	}
}
