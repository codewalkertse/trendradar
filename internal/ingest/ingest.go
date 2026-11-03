// Package ingest wires fetch and parse into one operation with the HTML->search
// fallback described in the plan (M2 T2.9).
package ingest

import (
	"context"

	"github.com/morehao/trendradar/internal/digest"
	"github.com/morehao/trendradar/internal/fetch"
	"github.com/morehao/trendradar/internal/ipc"
	"github.com/morehao/trendradar/internal/parse"
)

// Degraded reason codes surfaced on the digest and in warnings.
const (
	// ReasonNone means a clean trending-HTML run.
	ReasonNone = ""
	// ReasonHTMLPartial means some languages failed to fetch/parse but the rest
	// produced trending-HTML rows (per-language tolerance).
	ReasonHTMLPartial = "html_partial"
	// ReasonSearchFallback means the whole HTML path failed and the search API
	// provided the data (no per-period star deltas).
	ReasonSearchFallback = "search_fallback"
)

// Result is what one ingest run produced.
type Result struct {
	Repos    []digest.RepoSnapshot
	Report   digest.ParseReport
	Degraded bool // true when the data is not a clean trending-HTML result
	// DegradedReason explains why the run degraded (one of the Reason* codes).
	// Empty when Degraded is false.
	DegradedReason string
	// DegradedLangs lists languages that failed to fetch or produced no rows and
	// were therefore dropped from this run. Empty for a clean run.
	DegradedLangs []string
	// DegradedDetail explains *why* the run degraded, as a compact
	// "lang=reason" list ("python=timeout, typescript=status 429") or a single
	// sentence when the failure is not per-language (e.g. the pages arrived but
	// nothing parsed). Empty for a clean run. It is what turns "抓取失败" into a
	// diagnosable warning.
	DegradedDetail string
	// ByPage is the per-page view used by the renderer to lay markdown out into
	// one section per requested language. Keyed by page language ("all" = global
	// page); repos under a key keep that page's order and are NOT de-duplicated
	// across pages. It is nil for the search-fallback path, where there is no
	// notion of a page (see fallback).
	ByPage map[string][]digest.RepoSnapshot
}

// Ingest fetches trending pages, parses them, and falls back to the search API
// when scraping fails or yields no rows. Per-language failures are tolerated:
// as long as one language yields rows, that partial result is used (with
// Degraded=true, reason html_partial) instead of discarding everything.
func Ingest(ctx context.Context, c *fetch.Client, languages []string, since string) (Result, error) {
	fr, ferr := c.FetchTrending(ctx, languages, since)
	if ferr != nil && ctx.Err() != nil {
		// Cancellation is not a degradation: surface it instead of quietly
		// switching to the search API.
		return Result{}, ferr
	}
	if ferr != nil || !fr.Any() {
		// Every language failed: fall back to the search API.
		res, err := fallback(ctx, c, languages, since)
		if err != nil {
			return res, err
		}
		res.DegradedDetail = fetch.SummarizeFailures(fr.Failures)
		if res.DegradedDetail == "" {
			res.DegradedDetail = "所有语言页均未取到内容"
		}
		return res, nil
	}

	repos, byPage, report, perr := parse.ParseAll(fr.Pages)
	if perr != nil || len(repos) == 0 {
		res, err := fallback(ctx, c, languages, since)
		if err != nil {
			return res, err
		}
		// The pages did arrive, so this is a parser/shape problem rather than a
		// network one. Saying which matters: the fix is completely different.
		reason := "趋势页抓取成功但未解析出条目（页面结构可能已变化）"
		if perr != nil {
			reason = "趋势页内容解析失败：" + perr.Error()
		}
		if detail := fetch.SummarizeFailures(fr.Failures); detail != "" {
			reason += "；同时以下语言抓取失败：" + detail
		}
		res.DegradedDetail = reason
		return res, nil
	}

	// Languages that were requested but contributed no page are degraded even
	// if the pages we did get parsed fine.
	degradedLangs := append([]string(nil), fr.FailedLangs...)
	res := Result{Repos: repos, ByPage: byPage, Report: report}
	if len(degradedLangs) > 0 {
		res.Degraded = true
		res.DegradedReason = ReasonHTMLPartial
		res.DegradedLangs = degradedLangs
		res.DegradedDetail = fetch.SummarizeFailures(fr.Failures)
	}
	return res, nil
}

func fallback(ctx context.Context, c *fetch.Client, languages []string, since string) (Result, error) {
	repos, err := c.SearchFallback(ctx, languages, since)
	if err != nil {
		return Result{}, &fetch.Error{
			Code:    ipc.CodeFetchFailed,
			Message: "trending HTML and search API both failed",
			Err:     err,
		}
	}
	// No page information on the search-fallback path: leave ByPage nil so the
	// renderer groups by repo language (its legacy layout) and the digest is
	// clearly marked degraded.
	return Result{
		Repos:          repos,
		Report:         digest.ParseReport{Total: len(repos), Degraded: true},
		Degraded:       true,
		DegradedReason: ReasonSearchFallback,
	}, nil
}
