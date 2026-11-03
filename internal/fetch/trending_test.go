package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morehao/trendradar/internal/ipc"
)

// TestFetchTrendingRecordsFailureReasons verifies a degraded run keeps *why* a
// language failed, not just its name: "status 429" and "status 502" must stay
// distinguishable so the run warning can say whether to wait out a rate limit or
// look at the network.
func TestFetchTrendingRecordsFailureReasons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending/python":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/trending/go":
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`<html><article class="Box-row"></article></html>`))
		}
	}))
	defer srv.Close()

	c := NewClient(Options{BaseURL: srv.URL, APIBaseURL: srv.URL, RetryInterval: 5 * time.Millisecond, MaxRetry: 1})
	res, err := c.FetchTrending(context.Background(), []string{"go", "python", "rust"}, "daily")
	if err != nil {
		t.Fatalf("FetchTrending: %v", err)
	}
	if len(res.Pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(res.Pages))
	}
	// Sorted by language so the warning text is stable across runs.
	if got, want := strings.Join(res.FailedLangs, ","), "go,python"; got != want {
		t.Fatalf("FailedLangs = %q, want %q", got, want)
	}
	if got, want := SummarizeFailures(res.Failures), "go=status 502, python=status 429"; got != want {
		t.Fatalf("SummarizeFailures = %q, want %q", got, want)
	}
}

// TestFetchTrendingAllFailKeepsReason verifies the all-languages-failed error
// carries the per-language reasons, so the caller can put them in the warning
// that accompanies the search-API fallback.
func TestFetchTrendingAllFailKeepsReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(Options{BaseURL: srv.URL, APIBaseURL: srv.URL, RetryInterval: 5 * time.Millisecond, MaxRetry: 1})
	res, err := c.FetchTrending(context.Background(), []string{"all", "go"}, "daily")
	if err == nil {
		t.Fatal("expected error when every language fails")
	}
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("error type = %T, want *fetch.Error", err)
	}
	if !strings.Contains(fe.Message, "all=status 429") || !strings.Contains(fe.Message, "go=status 429") {
		t.Fatalf("message = %q, want both language reasons", fe.Message)
	}
	if len(res.Failures) != 2 {
		t.Fatalf("Failures = %v, want 2 entries", res.Failures)
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"rate limited", &Error{Code: ipc.CodeFetchRateLimited, Message: "status 429"}, "status 429"},
		{"bad status", &Error{Code: ipc.CodeFetchFailed, Message: "unexpected status 502"}, "unexpected status 502"},
		{"timeout", &Error{Code: ipc.CodeFetchFailed, Message: "request failed", Err: context.DeadlineExceeded}, "timeout"},
		{"eof", &Error{Code: ipc.CodeFetchFailed, Message: "request failed", Err: io.ErrUnexpectedEOF}, "EOF"},
		{"dns", errors.New("dial tcp: lookup github.com: no such host"), "dns error"},
		{"reset", errors.New("read tcp 127.0.0.1:1->2: connection reset by peer"), "connection reset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.err); got != tc.want {
				t.Fatalf("Summarize = %q, want %q", got, tc.want)
			}
		})
	}
	if got := Summarize(nil); got != "" {
		t.Fatalf("Summarize(nil) = %q, want empty", got)
	}
}
