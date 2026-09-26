package checker

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"uptimex/internal/models"
	"uptimex/internal/version"
)

// HTTPChecker probes HTTP/HTTPS endpoints using net/http with a hard
// per-probe deadline derived from the endpoint configuration. It is safe for
// concurrent use; a shared transport pools connections across workers.
type HTTPChecker struct {
	client *http.Client
	guard  *Guard
}

// NewHTTPChecker builds a checker. The dialer enforces the SSRF policy at
// connect time (see Guard.Control).
func NewHTTPChecker(guard *Guard) *HTTPChecker {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   guard.Control,
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &HTTPChecker{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return nil
			},
		},
		guard: guard,
	}
}

// Check performs one probe. It never returns an error and never panics; every
// failure mode is expressed in the structured result.
func (c *HTTPChecker) Check(ctx context.Context, ep models.Endpoint) models.CheckResult {
	res := models.CheckResult{
		EndpointID: ep.ID,
		CheckedAt:  time.Now().UTC(),
	}

	if err := c.guard.ValidateURL(ep.URL); err != nil {
		if errors.Is(err, ErrBlocked) {
			res.ErrorType = models.ErrTypeBlocked
		} else {
			res.ErrorType = models.ErrTypeInvalidURL
		}
		res.ErrorMessage = err.Error()
		return res
	}

	timeout := time.Duration(ep.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(ep.Method), ep.URL, nil)
	if err != nil {
		res.ErrorType = models.ErrTypeInvalidURL
		res.ErrorMessage = "invalid request: " + err.Error()
		return res
	}
	req.Header.Set("User-Agent", version.ServiceName+"/"+version.Version)
	req.Header.Set("Accept", "*/*")

	start := time.Now()
	resp, err := c.client.Do(req)
	res.ResponseTimeMs = time.Since(start).Milliseconds()

	if err != nil {
		res.ErrorType = classifyError(ctx.Err(), err)
		res.ErrorMessage = sanitize(err.Error())
		return res
	}

	// Drain a bounded amount of the body so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()

	code := resp.StatusCode
	res.StatusCode = &code
	if code >= ep.ExpectedStatusMin && code <= ep.ExpectedStatusMax {
		res.Success = true
		return res
	}
	res.ErrorType = models.ErrTypeHTTPError
	res.ErrorMessage = sanitizeHTTPError(ep.ExpectedStatusMin, ep.ExpectedStatusMax, code)
	return res
}

// classifyError maps a transport error to a stable error type taxonomy.
func classifyError(ctxErr error, err error) string {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return models.ErrTypeTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return models.ErrTypeTimeout
	}
	if errors.Is(ctxErr, context.Canceled) {
		return models.ErrTypeCanceled
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return models.ErrTypeDNS
	}
	var tlsErr tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &tlsErr) || errors.As(err, &certErr) {
		return models.ErrTypeTLS
	}
	msg := err.Error()
	// Windows reports WSAECONNREFUSED as "actively refused" rather than
	// surfacing syscall.ECONNREFUSED through the url error chain.
	if errors.Is(err, syscall.ECONNREFUSED) ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "actively refused") {
		return models.ErrTypeConnection
	}
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate") {
		return models.ErrTypeTLS
	}
	if strings.Contains(msg, "blocked by SSRF") {
		return models.ErrTypeBlocked
	}
	return "network"
}

func sanitizeHTTPError(min, max, code int) string {
	return "status " + itoa(code) + " outside expected range " + itoa(min) + "-" + itoa(max)
}

// sanitize strips query strings and fragments from error text so URLs with
// embedded tokens are never persisted or logged verbatim.
func sanitize(msg string) string {
	if i := strings.Index(msg, "?"); i >= 0 {
		msg = msg[:i] + " (query removed)"
	}
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return msg
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
