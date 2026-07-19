package adapters

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/services/netguard"
)

// providerHTTPClient validates DNS answers at connection time. Literal IP
// endpoints are retained for local contract tests; user-facing config rejects
// private literal addresses before a provider request is accepted.
func providerHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// Do not honor process proxy variables: a proxy would bypass the
		// connection-level destination validation below.
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				if isUnsafeProviderIP(ip) && !providerLiteralIPAllowed(host, ip) {
					return nil, fmt.Errorf("provider address is not allowed")
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, fmt.Errorf("resolve provider host: %w", err)
			}
			allowPrivate := providerPrivateHostAllowed(host)
			for _, ip := range ips {
				if isUnsafeProviderIP(ip) && !allowPrivate {
					continue
				}
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
			}
			return nil, fmt.Errorf("provider host resolves only to disallowed addresses")
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 180 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		TLSClientConfig:       providerTLSConfig(),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func providerLiteralIPAllowed(host string, ip net.IP) bool {
	// Loopback literals are used by in-process contract tests. User-controlled
	// provider configuration still has to pass the stricter URL validation.
	if ip.IsLoopback() {
		return true
	}
	return providerPrivateHostAllowed(host)
}

func providerPrivateHostAllowed(host string) bool {
	if !ProviderCustomCAConfigured() {
		return false
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	for _, candidate := range strings.Split(os.Getenv("AI_PROVIDER_PRIVATE_HOSTS"), ",") {
		if strings.TrimSuffix(strings.ToLower(strings.TrimSpace(candidate)), ".") == host {
			return true
		}
	}
	return false
}

func providerTLSConfig() *tls.Config {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if roots, ok := providerCustomRoots(); ok {
		config.RootCAs = roots
	}
	return config
}

func providerCustomRoots() (*x509.CertPool, bool) {
	caFile := os.Getenv("AI_PROVIDER_CA_FILE")
	if caFile == "" {
		return nil, false
	}
	payload, err := os.ReadFile(caFile)
	if err != nil {
		return nil, false
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(payload) {
		return nil, false
	}
	return roots, true
}

// ProviderCustomCAConfigured reports whether the configured CA file contains
// at least one parseable certificate. Callers must still authorize target
// hosts separately.
func ProviderCustomCAConfigured() bool {
	_, ok := providerCustomRoots()
	return ok
}

// SecureProviderHTTPClient exposes the provider transport to callers that use
// third-party SDKs while preserving connection-time DNS and IP validation.
func SecureProviderHTTPClient(timeout time.Duration) *http.Client {
	return providerHTTPClient(timeout)
}

func isUnsafeProviderIP(ip net.IP) bool {
	return netguard.IsUnsafeIP(ip)
}
