package fetch

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/morehao/trendradar/internal/ipc"
)

// FailedLang is one language whose trending page could not be fetched, plus a
// short user-facing reason ("status 429"、"timeout"…). Keeping the reason is
// deliberate: "抓取失败" on its own cannot tell a rate limit from a network
// outage, and what to do next (wait it out vs. check the proxy) depends on
// which one it was.
type FailedLang struct {
	Lang   string
	Reason string
}

// TrendingResult is the outcome of fetching trending pages. Pages holds the
// successfully downloaded pages keyed by language ("all" means the global
// page); FailedLangs lists the languages whose fetch failed and Failures
// carries the same list with reasons. A partial result (some pages, some
// failures) is returned without error so one flaky language never discards the
// rest — callers decide how to treat FailedLangs.
type TrendingResult struct {
	Pages       map[string][]byte
	FailedLangs []string
	Failures    []FailedLang
}

// Any reports whether at least one page was fetched successfully.
func (r TrendingResult) Any() bool { return len(r.Pages) > 0 }

// FetchTrending downloads the trending HTML for each language concurrently.
// It tolerates per-language failures: successful pages are returned alongside
// the list of languages that failed. It returns an error only when every
// language failed, so callers can fall back to the search API in that case.
// A failed language always keeps its reason (see FailedLang), so a degraded run
// can report why it degraded instead of only which languages were dropped.
func (c *Client) FetchTrending(ctx context.Context, languages []string, since string) (TrendingResult, error) {
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	out := make(map[string][]byte, len(languages))
	var failures []FailedLang

	for _, lang := range languages {
		lang := lang
		g.Go(func() error {
			body, err := c.getHTML(gctx, trendingPath(lang, since))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, FailedLang{Lang: lang, Reason: Summarize(err)})
				return nil // tolerate: do not abort the group
			}
			out[lang] = body
			return nil
		})
	}
	// Every goroutine returns nil, so Wait only surfaces context cancellation.
	if err := g.Wait(); err != nil {
		return TrendingResult{}, err
	}

	// Goroutines finish in arbitrary order; sort so warnings read the same on
	// every run and two records of one incident stay comparable.
	sort.Slice(failures, func(i, j int) bool { return failures[i].Lang < failures[j].Lang })
	failed := make([]string, 0, len(failures))
	for _, f := range failures {
		failed = append(failed, f.Lang)
	}

	res := TrendingResult{Pages: out, FailedLangs: failed, Failures: failures}
	if len(res.Pages) == 0 {
		return res, &Error{Code: ipc.CodeFetchFailed, Message: "all languages failed to fetch: " + SummarizeFailures(failures)}
	}
	return res, nil
}

// trendingPath builds the request path. "all" (or empty) uses the global page.
func trendingPath(lang, since string) string {
	p := "/trending"
	if lang != "" && lang != "all" {
		p += "/" + url.PathEscape(lang)
	}
	if since != "" {
		p += "?since=" + url.QueryEscape(since)
	}
	return p
}

// SummarizeFailures renders "all=status 429, python=timeout" for logs, warnings
// and error messages. Order follows the slice, which FetchTrending sorts.
func SummarizeFailures(failures []FailedLang) string {
	parts := make([]string, 0, len(failures))
	for _, f := range failures {
		reason := f.Reason
		if reason == "" {
			reason = "unknown"
		}
		parts = append(parts, f.Lang+"="+reason)
	}
	return strings.Join(parts, ", ")
}
