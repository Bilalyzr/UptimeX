package middleware

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CORS allows the dashboard origin to call the API from a browser. When
// allowedOrigin is empty no CORS headers are added (same-origin deployments
// proxy /api through the dashboard's own web server).
type CORS struct {
	AllowedOrigin string
	Next          http.Handler
}

func (m *CORS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.AllowedOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", m.AllowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	m.Next.ServeHTTP(w, r)
}

// APIKeyAuth guards administrative routes. When key is empty the middleware
// is a pass-through (development mode); in production an API key must be
// configured (PRD §26). The comparison is constant-time.
type APIKeyAuth struct {
	Key  string
	Next http.Handler
}

func (m *APIKeyAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if m.Key == "" {
		m.Next.ServeHTTP(w, r)
		return
	}
	provided := r.Header.Get("X-API-Key")
	if provided == "" {
		// Also accept Authorization: Bearer <key> for CLI convenience.
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			provided = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(m.Key)) != 1 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"missing or invalid API key"}`))
		return
	}
	m.Next.ServeHTTP(w, r)
}

// tokenBucket is a minimal token-bucket rate limiter.
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	rps        float64
	burst      float64
}

func (b *tokenBucket) allow(now time.Time) bool {
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.rps
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.lastRefill = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimit throttles requests per client IP using a token bucket. Clients
// exceeding the limit receive 429. Liveness/readiness paths in ExemptPaths
// bypass throttling so monitoring probes never trip false alarms.
type RateLimit struct {
	rps         float64
	burst       float64
	TrustProxy  bool
	ExemptPaths map[string]bool
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	Next        http.Handler
}

// NewRateLimit builds the limiter; rps is sustained requests/second and burst
// is the bucket capacity.
func NewRateLimit(rps float64, burst int, next http.Handler) *RateLimit {
	return &RateLimit{
		rps:     rps,
		burst:   float64(burst),
		buckets: make(map[string]*tokenBucket),
		Next:    next,
	}
}

func (m *RateLimit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.check(w, r, m.Next)
}

// Wrap returns a handler that draws from this limiter's shared per-client
// buckets but dispatches to the given next handler — used to bind one
// strict budget across a route group (e.g. every credential endpoint).
func (m *RateLimit) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.check(w, r, next)
	})
}

func (m *RateLimit) check(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if m.ExemptPaths != nil && m.ExemptPaths[r.URL.Path] {
		next.ServeHTTP(w, r)
		return
	}
	client := resolveClientIP(r, m.TrustProxy)
	m.mu.Lock()
	b, ok := m.buckets[client]
	if !ok {
		b = &tokenBucket{tokens: m.burst, lastRefill: time.Now(), rps: m.rps, burst: m.burst}
		m.buckets[client] = b
		// Opportunistic cleanup keeps the map bounded under address churn.
		if len(m.buckets) > 10_000 {
			cutoff := time.Now().Add(-10 * time.Minute)
			for k, v := range m.buckets {
				if v.lastRefill.Before(cutoff) {
					delete(m.buckets, k)
				}
			}
		}
	}
	allowed := b.allow(time.Now())
	m.mu.Unlock()

	if !allowed {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
		return
	}
	next.ServeHTTP(w, r)
}

// resolveClientIP extracts the caller IP. Forwarding headers (CF-Connecting-IP,
// X-Real-IP, X-Forwarded-For) are honored when trustProxy is explicitly enabled
// OR when the immediate peer is a loopback/private reverse proxy (such as the
// bundled Docker nginx container or a platform load balancer). For
// X-Forwarded-For we inspect right-to-left so client-prepended fake IPs cannot
// spoof the proxy-appended address.
func resolveClientIP(r *http.Request, trustProxy bool) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = strings.TrimSpace(r.RemoteAddr)
	}
	peerIP := net.ParseIP(peer)
	isTrustedPeer := trustProxy || (peerIP != nil && (peerIP.IsLoopback() || peerIP.IsPrivate() || peerIP.IsLinkLocalUnicast()))

	if isTrustedPeer {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" && net.ParseIP(cf) != nil {
			return cf
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" && net.ParseIP(xr) != nil {
			return xr
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				cand := strings.TrimSpace(parts[i])
				if net.ParseIP(cand) != nil {
					return cand
				}
			}
		}
	}
	return peer
}
