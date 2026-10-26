// Package clientip configures Fiber so c.IP() is the visitor address behind
// Railway's edge proxy, and the TCP peer for every other connection.
//
// Railway's public docs name X-Real-IP as the header that identifies the
// client's remote IP:
// https://docs.railway.com/networking/public-networking/specs-and-limits
// Staff on Railway Station confirm the edge proxy always sets that header
// and always overwrites a client-supplied value, and that an app on Railway's
// HTTP proxy cannot be reached except through that proxy:
// https://station.railway.com/questions/need-authoritative-railway-client-ip-p-b7a7b4bd
//
// X-Forwarded-For is a different header. Staff say the edge strips a
// client-supplied value, the leftmost entry is the connecting client, and a
// later hop through Railway's network may append another address:
// https://station.railway.com/questions/security-critical-questions-on-edge-prox-8fddd775
// Operators have also observed the chain "client, railway-edge" with the TCP
// peer being a third internal address (https://docs.webdecoy.com/installation/railway/).
// The rightmost X-Forwarded-For entry is therefore an internal hop, not the
// visitor. This package does not read X-Forwarded-For. A client-prepended
// fake in that header cannot change c.IP().
//
// Railway does not publish a stable proxy CIDR. The addresses seen as the
// TCP peer are in 100.64.0.0/10 (RFC 6598), for example 100.64.0.2–100.64.0.4:
// https://station.railway.com/questions/caddy-not-whitelisting-railways-proxy-4484064e
// A Railway employee noted that range can change. TRUSTED_PROXIES overrides it.
// A peer outside the list is not trusted, so c.IP() stays the TCP peer.
package clientip

import (
	"fmt"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const (
	// DefaultHeader is the header Railway's edge writes with the client IP.
	DefaultHeader = "X-Real-IP"

	// DefaultProxies is the CGNAT range observed for Railway's internal proxy.
	DefaultProxies = "100.64.0.0/10"
)

// Resolve turns operator input into the header and peer allow-list Fiber
// should trust. Empty input selects the Railway defaults. The sentinels
// "none", "off", "false", and "-" disable header trust so c.IP() is always
// the TCP peer.
func Resolve(headerRaw, proxiesRaw string) (header string, proxies []string, err error) {
	header, err = resolveHeader(headerRaw)
	if err != nil {
		return "", nil, err
	}
	if header == "" {
		return "", nil, nil
	}
	proxies, err = resolveProxies(proxiesRaw)
	if err != nil {
		return "", nil, err
	}
	return header, proxies, nil
}

func resolveHeader(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultHeader, nil
	}
	if isDisabled(raw) {
		return "", nil
	}
	if strings.ContainsAny(raw, " \t,") {
		return "", fmt.Errorf("trusted proxy header %q must be a single header name", raw)
	}
	return raw, nil
}

func resolveProxies(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultProxies
	}
	if isDisabled(raw) {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if err := validateProxy(part); err != nil {
			return nil, err
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trusted proxies list is empty")
	}
	return out, nil
}

func validateProxy(p string) error {
	if strings.Contains(p, "/") {
		if _, _, err := net.ParseCIDR(p); err != nil {
			return fmt.Errorf("trusted proxy %q is not a CIDR: %w", p, err)
		}
		return nil
	}
	if net.ParseIP(p) == nil {
		return fmt.Errorf("trusted proxy %q is not an IP address or CIDR", p)
	}
	return nil
}

func isDisabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none", "off", "false", "-":
		return true
	default:
		return false
	}
}

// Apply copies base and sets Fiber's trusted-proxy fields.
// c.IP() reads header only when the TCP peer is inside proxies.
// Any other peer, including local dev, gets the TCP peer address.
func Apply(base fiber.Config, header string, proxies []string) fiber.Config {
	base.ProxyHeader = header
	base.EnableTrustedProxyCheck = true
	base.EnableIPValidation = true
	if proxies == nil {
		base.TrustedProxies = []string{}
	} else {
		base.TrustedProxies = proxies
	}
	return base
}
