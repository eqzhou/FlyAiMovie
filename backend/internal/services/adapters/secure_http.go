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
			allowPrivate := providerPrivateHostAllowed(host)
			ips, err := lookupProviderIPs(ctx, host, allowPrivate)
			if err != nil {
				return nil, fmt.Errorf("resolve provider host: %w", err)
			}
			// Clash/MacPacket fake-ip DNS returns 198.18.0.0/15. Those addresses are
			// local proxy routes for the original public hostname, not real private
			// networks. Allow dialing them only when every answer is in that range.
			allowFakeIP := !allowPrivate && onlyProxyFakeIPs(ips)
			var lastDialErr error
			tried := 0
			for _, ip := range ips {
				if isUnsafeProviderIP(ip) && !allowPrivate && !(allowFakeIP && isProxyFakeIP(ip)) {
					continue
				}
				tried++
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				lastDialErr = dialErr
			}
			if tried == 0 {
				return nil, fmt.Errorf("provider host resolves only to disallowed addresses")
			}
			if lastDialErr != nil {
				return nil, fmt.Errorf("provider host dial failed: %w", lastDialErr)
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

// lookupProviderIPs resolves provider hostnames with a public-DNS fallback.
// Local Clash/MacPacket fake-ip DNS often returns 198.18.0.0/15 addresses that
// are valid proxy routes for the original public hostname. Prefer those answers
// over public DNS replacement so traffic still goes through the local proxy.
func lookupProviderIPs(ctx context.Context, host string, allowPrivate bool) ([]net.IP, error) {
	ips, err := net.LookupIP(host)
	if err == nil {
		if allowPrivate || hasSafeProviderIP(ips) || onlyProxyFakeIPs(ips) {
			return ips, nil
		}
	}
	if allowPrivate {
		if err != nil {
			return nil, err
		}
		return ips, nil
	}
	fallback, fallbackErr := lookupIPsViaPublicDNS(ctx, host)
	if fallbackErr == nil && len(fallback) > 0 {
		return fallback, nil
	}
	if err != nil {
		return nil, err
	}
	return ips, nil
}

func hasSafeProviderIP(ips []net.IP) bool {
	for _, ip := range ips {
		if !isUnsafeProviderIP(ip) {
			return true
		}
	}
	return false
}

func lookupIPsViaPublicDNS(ctx context.Context, host string) ([]net.IP, error) {
	resolvers := []string{"1.1.1.1:53", "8.8.8.8:53"}
	seen := map[string]struct{}{}
	out := make([]net.IP, 0, 4)
	var lastErr error
	for _, server := range resolvers {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(dialCtx, "udp", server)
			},
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		addrs, err := resolver.LookupIPAddr(lookupCtx, host)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		for _, addr := range addrs {
			if addr.IP == nil {
				continue
			}
			key := addr.IP.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, addr.IP)
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no public DNS answers for %s", host)
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

// proxyFakeIPNet is the RFC 2544 benchmarking range commonly used by Clash
// and similar clients for fake-ip mode.
var proxyFakeIPNet = func() *net.IPNet {
	_, network, err := net.ParseCIDR("198.18.0.0/15")
	if err != nil {
		panic(err)
	}
	return network
}()

func isProxyFakeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return proxyFakeIPNet.Contains(ip)
}

func onlyProxyFakeIPs(ips []net.IP) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isProxyFakeIP(ip) {
			return false
		}
	}
	return true
}

func isUnsafeProviderIP(ip net.IP) bool {
	return netguard.IsUnsafeIP(ip)
}
