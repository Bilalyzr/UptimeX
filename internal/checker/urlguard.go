package checker

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// ErrBlocked reports a target rejected by the SSRF guard.
var ErrBlocked = errors.New("target address blocked by SSRF policy")

// Guard validates outbound monitoring targets (PRD §26 SSRF requirements).
//
// Protection has two layers:
//  1. ValidateURL — syntactic checks at registration and at probe time:
//     scheme allow-list, no embedded credentials, sane host/port, and
//     literal-IP policy.
//  2. Control — a net.Dialer control hook invoked after DNS resolution but
//     before the connection is established, so hostnames that resolve to
//     private/loopback/link-local space (including cloud metadata endpoints)
//     are rejected even when the public DNS record changes later.
type Guard struct {
	AllowPrivate bool
}

// NewGuard builds a guard. allowPrivate=true is for local development and
// explicitly-trusted internal monitoring only.
func NewGuard(allowPrivate bool) *Guard {
	return &Guard{AllowPrivate: allowPrivate}
}

// ValidateURL enforces the URL-level policy.
func (g *Guard) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("URL has no host")
	}
	if u.User != nil {
		return errors.New("credentials in URL are not allowed")
	}
	if port := u.Port(); port != "" {
		p, err := net.LookupPort("tcp", port)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid port %q", port)
		}
	}

	if g.AllowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("%w: literal IP %s is private/reserved", ErrBlocked, host)
	}
	return nil
}

// Control is the dial-time hook: address is "ip:port" after DNS resolution.
func (g *Guard) Control(network, address string, _ syscall.RawConn) error {
	if g.AllowPrivate {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: unparseable dial address %q", ErrBlocked, address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: dial target %q is not an IP", ErrBlocked, address)
	}
	if isBlockedIP(ip) {
		return fmt.Errorf("%w: %s resolves to private/reserved address %s", ErrBlocked, address, host)
	}
	return nil
}

// isBlockedIP reports whether an IP must never be probed in strict mode:
// loopback, RFC1918/ULA private space, link-local (includes cloud metadata
// 169.254.169.254), unspecified, multicast and other reserved ranges.
func isBlockedIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() ||
		ip.IsMulticast() ||
		isReserved(ip)
}

// isReserved covers ranges net.IP does not classify (e.g. carrier-grade NAT
// 100.64/10, benchmarking 198.18/15).
func isReserved(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 { // CGNAT 100.64.0.0/10
			return true
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) { // 198.18.0.0/15
			return true
		}
		if v4[0] == 192 && v4[1] == 0 && v4[2] == 2 { // TEST-NET-1
			return true
		}
	}
	return false
}

// HostnameLooksPrivate is a fast textual pre-check used in tests and tools.
func HostnameLooksPrivate(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0" ||
		h == "metadata.google.internal" || h == "instance-data"
}
