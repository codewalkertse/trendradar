package fetch

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
)

// statusRe matches ghttp's non-2xx error text ("status=429, message=client
// error") so the summary can report the shape every other path uses.
var statusRe = regexp.MustCompile(`status=(\d{3})`)

// Summarize condenses a fetch error into a short, stable reason suitable for
// user-facing warnings and logs: HTTP status codes are reported as
// "status 429", and network failures are classified (timeout / EOF /
// connection reset / DNS). Anything unrecognised is truncated rather than
// dropped, so an unforeseen failure still leaves a usable trail.
func Summarize(err error) string {
	if err == nil {
		return ""
	}
	var fe *Error
	if errors.As(err, &fe) {
		if fe.Err != nil {
			return networkSummary(fe.Err)
		}
		return fe.Message
	}
	return networkSummary(err)
}

// networkSummary classifies transport-level failures. Retry exhaustion on top
// of a 429 is by far the most common cause of "all languages failed"; the
// timeout branch covers the slow-proxy case.
func networkSummary(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	if m := statusRe.FindStringSubmatch(msg); m != nil {
		return "status " + m[1]
	}
	switch {
	case strings.Contains(msg, "connection reset"):
		return "connection reset"
	case strings.Contains(msg, "no such host"):
		return "dns error"
	case strings.Contains(msg, "EOF"):
		return "EOF"
	}
	return clip(msg, 120)
}

// clip shortens s to at most n runes so one verbose error cannot bloat a
// warning line.
func clip(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
