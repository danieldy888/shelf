// Package aiusage is the contract for the AI 用量 page: quota windows and
// local token-usage records shared between the data collectors and the
// panel page. Only types and constants live here.
package aiusage

import "time"

// Tool names one AI tool the page reports on.
type Tool string

const (
	ToolOmp    Tool = "omp"
	ToolClaude Tool = "claude"
	ToolCodex  Tool = "codex"
)

// Window is one rate-limit window of a subscription quota.
type Window struct {
	UsedPct  float64       // 0–100
	Length   time.Duration // 5h or 7d: identified by length, not by position
	ResetsAt time.Time
}

// QuotaStatus is how a quota fetch went.
type QuotaStatus int

const (
	QuotaOK QuotaStatus = iota
	QuotaNotLoggedIn
	QuotaExpired
	QuotaRateLimited
	QuotaFailed
)

// Quota is one tool's subscription quota at FetchedAt; FiveHour and Weekly
// are nil while the fetch could not produce them (see Status and Err).
type Quota struct {
	Tool       Tool
	Plan       string
	FiveHour   *Window
	Weekly     *Window
	Status     QuotaStatus
	FetchedAt  time.Time
	RetryAfter time.Duration
	Err        string
}

// PaceInfo compares a window's usage against an even pace. AheadPct > 0
// means running faster than even; RunsOutIn is meaningful only when
// LastsUntilReset is false.
type PaceInfo struct {
	ExpectedLeftPct float64
	AheadPct        float64
	RunsOutIn       time.Duration
	LastsUntilReset bool
}

// Record is one assistant message's token usage, as logged by its tool.
// Cost is the tool's own price; Priced is false when the record carried no
// price (only omp records prices).
type Record struct {
	Tool                     Tool
	At                       time.Time
	Model                    string
	Input, Output, CacheRead int64
	CacheWrite               int64
	Cost                     float64
	Priced                   bool
}

// Range is the usage summary's time span.
type Range int

const (
	RangeToday Range = iota
	Range7d
	Range30d
	RangeAll
)

// Totals sums records over a range; Unpriced counts records without a
// price, Cost only sums the ones that had one.
type Totals struct {
	Input, Output, CacheRead, CacheWrite, Requests, Unpriced int64
	Cost                                                     float64
}

// Bucket is one bar of the stacked chart: records grouped by tool.
// Tokens = input+output+cacheRead+cacheWrite.
type Bucket struct {
	Start  time.Time
	Tokens map[Tool]int64
}

// Summary is a range's totals and its stacked-chart buckets: 24 hourly
// buckets for RangeToday, daily buckets for the others (RangeAll caps at
// 365).
type Summary struct {
	Range   Range
	Total   Totals
	ByTool  map[Tool]Totals
	Buckets []Bucket
}

// Sources picks which tools' local usage gets scanned (mirrors the
// aiUsage.local* settings).
type Sources struct {
	Omp, Claude, Codex bool
}
