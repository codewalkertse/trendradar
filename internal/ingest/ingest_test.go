package ingest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/trendradar/internal/fetch"
	"github.com/morehao/trendradar/internal/ipc"
)

func TestIngestFallsBackWhenHTMLHasNoRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trending" {
			_, _ = w.Write([]byte(`<html><body><p>no rows here</p></body></html>`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"full_name":"acme/one","html_url":"https://github.com/acme/one","description":"d","language":"Go","stargazers_count":10,"forks_count":1}]}`))
	}))
	defer srv.Close()

	c := fetch.NewClient(fetch.Options{BaseURL: srv.URL, APIBaseURL: srv.URL})
	res, err := Ingest(context.Background(), c, []string{"all"}, "daily")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !res.Degraded {
		t.Fatal("expected Degraded=true")
	}
	if res.DegradedReason != ReasonSearchFallback {
		t.Fatalf("DegradedReason = %q, want %q", res.DegradedReason, ReasonSearchFallback)
	}
	if len(res.Repos) != 1 || res.Repos[0].Source != "search-api" {
		t.Fatalf("unexpected repos: %+v", res.Repos)
	}
	// The HTML page arrived but had no rows: the warning must say so instead of
	// blaming the network, because the fix (parser vs. proxy) differs.
	if !strings.Contains(res.DegradedDetail, "未解析出条目") {
		t.Fatalf("DegradedDetail = %q, want a parse-shape explanation", res.DegradedDetail)
	}
}

// TestIngestFallbackKeepsFetchReason verifies the search-fallback path still
// reports why every page failed, so "回退到搜索 API" is not the end of the trail.
func TestIngestFallbackKeepsFetchReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trending" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"full_name":"acme/one","html_url":"https://github.com/acme/one","description":"d","language":"Go","stargazers_count":10,"forks_count":1}]}`))
	}))
	defer srv.Close()

	c := fetch.NewClient(fetch.Options{BaseURL: srv.URL, APIBaseURL: srv.URL, RetryInterval: 5 * time.Millisecond, MaxRetry: 1})
	res, err := Ingest(context.Background(), c, []string{"all"}, "daily")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.DegradedReason != ReasonSearchFallback {
		t.Fatalf("DegradedReason = %q, want %q", res.DegradedReason, ReasonSearchFallback)
	}
	if got, want := res.DegradedDetail, "all=status 429"; got != want {
		t.Fatalf("DegradedDetail = %q, want %q", got, want)
	}
}

func TestIngestBothPathsFailReturnsFetchFailed(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := fetch.NewClient(fetch.Options{BaseURL: srv.URL, APIBaseURL: srv.URL, RetryInterval: 5 * time.Millisecond, MaxRetry: 2})
	_, err := Ingest(context.Background(), c, []string{"all"}, "daily")
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *fetch.Error
	if !errors.As(err, &fe) || fe.Code != ipc.CodeFetchFailed {
		t.Fatalf("err = %v, want FETCH_FAILED", err)
	}
}

// TestIngestToleratesPartialLanguageFailure verifies that when one language's
// page fails but another succeeds with rows, Ingest uses the successful data
// (degraded=html_partial) instead of falling all the way back to search.
func TestIngestToleratesPartialLanguageFailure(t *testing.T) {
	page := `<html><body>
<article class="Box-row">
  <h2 class="h3 lh-condensed"><a href="/acme/one">acme/one</a></h2>
  <p class="col-9">desc one</p>
  <span itemprop="programmingLanguage">Go</span>
  <a href="/acme/one/stargazers">1,200</a>
  <a href="/acme/one/forks">30</a>
  <span>40 stars today</span>
</article>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending/go":
			_, _ = w.Write([]byte(page))
		case "/trending/python":
			w.WriteHeader(http.StatusInternalServerError) // this language fails
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := fetch.NewClient(fetch.Options{BaseURL: srv.URL, APIBaseURL: srv.URL, RetryInterval: 5 * time.Millisecond, MaxRetry: 1})
	res, err := Ingest(context.Background(), c, []string{"go", "python"}, "daily")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !res.Degraded || res.DegradedReason != ReasonHTMLPartial {
		t.Fatalf("want degraded=html_partial, got degraded=%v reason=%q", res.Degraded, res.DegradedReason)
	}
	if len(res.DegradedLangs) != 1 || res.DegradedLangs[0] != "python" {
		t.Fatalf("DegradedLangs = %v, want [python]", res.DegradedLangs)
	}
	if got, want := res.DegradedDetail, "python=status 500"; got != want {
		t.Fatalf("DegradedDetail = %q, want %q", got, want)
	}
	if len(res.Repos) != 1 || res.Repos[0].Source != "trending-html" {
		t.Fatalf("unexpected repos: %+v", res.Repos)
	}
}
