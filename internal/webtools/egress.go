package webtools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type EgressPolicy struct {
	AllowedSchemes       map[string]bool
	AllowPrivateNetworks bool
}

type EgressViolation struct{ Message string }

func (e EgressViolation) Error() string { return e.Message }

func NewEgressPolicy(schemes string, allowPrivate bool) EgressPolicy {
	allowed := map[string]bool{}
	for _, scheme := range strings.Split(schemes, ",") {
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme != "" {
			allowed[scheme] = true
		}
	}
	if len(allowed) == 0 {
		allowed["http"] = true
		allowed["https"] = true
	}
	return EgressPolicy{AllowedSchemes: allowed, AllowPrivateNetworks: allowPrivate}
}

func (p EgressPolicy) ValidateURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, EgressViolation{Message: "web_fetch URL is invalid"}
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, EgressViolation{Message: "web_fetch URL must include scheme and host"}
	}
	if !p.AllowedSchemes[strings.ToLower(parsed.Scheme)] {
		return nil, EgressViolation{Message: fmt.Sprintf("web_fetch scheme %q is not allowed", parsed.Scheme)}
	}
	return parsed, nil
}

func (p EgressPolicy) ValidateHost(host string) error {
	_, err := p.resolveHost(context.Background(), host)
	return err
}

func (p EgressPolicy) resolveHost(ctx context.Context, host string) ([]net.IPAddr, error) {
	if host == "" {
		return nil, EgressViolation{Message: "web_fetch host is empty"}
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, EgressViolation{Message: "web_fetch host did not resolve"}
	}
	for _, addr := range ips {
		if err := p.ValidateIP(addr.IP); err != nil {
			return nil, err
		}
	}
	return ips, nil
}

func newValidatedClient(base *http.Client, host string, ips []net.IPAddr) *http.Client {
	client := *base
	transport, ok := base.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	clone := transport.Clone()
	clone.Proxy = nil
	dialer := &net.Dialer{}
	clone.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		for _, addr := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.IP.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("web_fetch validated addresses for %s are unreachable", host)
	}
	client.Transport = clone
	return &client
}

func (p EgressPolicy) ValidateIP(ip net.IP) error {
	if p.AllowPrivateNetworks {
		return nil
	}
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return EgressViolation{Message: "web_fetch resolved to a disallowed network address"}
	}
	return nil
}
