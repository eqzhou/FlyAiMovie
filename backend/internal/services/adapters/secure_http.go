package adapters

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
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
				return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, fmt.Errorf("resolve provider host: %w", err)
			}
			for _, ip := range ips {
				if isUnsafeProviderIP(ip) {
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
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func isUnsafeProviderIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || !ip.IsGlobalUnicast()
}
