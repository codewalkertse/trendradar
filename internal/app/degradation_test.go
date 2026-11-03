package app

import (
	"strings"
	"testing"

	"github.com/morehao/trendradar/internal/ingest"
)

func TestIngestDegradation(t *testing.T) {
	cases := []struct {
		name        string
		reason      string
		langs       []string
		detail      string
		wantBanner  string
		wantWarning string
	}{
		{
			name:        "search fallback keeps the reason",
			reason:      ingest.ReasonSearchFallback,
			detail:      "all=status 429",
			wantBanner:  "本次为降级数据",
			wantWarning: "趋势页抓取失败，已回退到 GitHub 搜索 API：无今日新增增量（全部为 0），排序按总 star（原因：all=status 429）",
		},
		{
			name:        "partial keeps per-language reasons",
			reason:      ingest.ReasonHTMLPartial,
			langs:       []string{"python", "typescript"},
			detail:      "python=timeout, typescript=status 429",
			wantBanner:  "以下语言抓取失败",
			wantWarning: "以下语言抓取失败，已跳过：python, typescript（原因：python=timeout, typescript=status 429）",
		},
		{
			name:        "partial without detail stays unchanged",
			reason:      ingest.ReasonHTMLPartial,
			langs:       []string{"python"},
			wantBanner:  "以下语言抓取失败",
			wantWarning: "以下语言抓取失败，已跳过：python",
		},
		{
			name:   "clean run emits nothing",
			reason: ingest.ReasonNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			banner, warning := ingestDegradation(tc.reason, tc.langs, tc.detail)
			if warning != tc.wantWarning {
				t.Fatalf("warning = %q, want %q", warning, tc.wantWarning)
			}
			if tc.wantBanner == "" {
				if banner != "" {
					t.Fatalf("banner = %q, want empty", banner)
				}
				return
			}
			if !strings.Contains(banner, tc.wantBanner) {
				t.Fatalf("banner = %q, want it to contain %q", banner, tc.wantBanner)
			}
		})
	}
}
