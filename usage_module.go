//go:build darwin

package main

// AI 用量页的数据侧：刷新节奏、后台取数、回主线程发布。页面绘制在
// usage_page.go。取数和扫描的具体实现由别的部分提供，这里只依赖
// internal/aiusage 的类型和注入进来的函数，所以页面可以在没有真数据源
// 的测试里跑通整条刷新链路。
//
// 线程模型（同 updater.go）：慢活（查额度、扫本地记录）跑在 goWork 的
// goroutine 上，结果经 dispatchMain 回主线程再写入 f.* 状态并
// Invalidate；UI、设置和调度只在主线程读写。首次扫描的进度条是唯一后台
// 直写的状态，走 atomic。

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"shelf/internal/aiusage"
	"shelf/internal/settings"
)

const (
	// usageLocalInterval is how often the local token summary is rebuilt
	// while the page shows; a page visit refreshes it at once when stale.
	usageLocalInterval = 60 * time.Second
	// usageQuotaInterval is the fastest a quota endpoint is asked, however
	// the refresh was started (timer or button): Claude Code 429s below
	// that pace. A server RetryAfter holds a tool back longer.
	usageQuotaInterval = 5 * time.Minute
	// usageQuotaManual is the shortest gap a manual refresh asks a quota
	// endpoint again; Claude Code's own minimum is usageQuotaInterval —
	// it 429s below that pace, whatever the trigger (PLAN.md).
	usageQuotaManual = 60 * time.Second
	// usageQuotaTimeout and usageScanTimeout bound one round of work so a
	// stalled source frees its busy flag instead of silencing refreshes.
	usageQuotaTimeout = 20 * time.Second
	usageScanTimeout  = 10 * time.Minute
)

// usageDeps injects the data collectors so the page never imports their
// implementations: quota fetches, the even-pace estimator, the local
// scanner and the clock (all swappable by tests).
type usageDeps struct {
	fetchCodex  func(context.Context) aiusage.Quota
	fetchClaude func(context.Context) aiusage.Quota
	pace        func(aiusage.Window, time.Time) aiusage.PaceInfo
	// newScanner builds a fresh local scanner per refresh so the
	// aiUsage.local* switches apply at once; the on-disk incremental
	// cache makes a rebuild cheap — the sources a Scanner was built
	// with are fixed (internal/aiusage.NewScanner).
	newScanner func() usageScanner
	now        func() time.Time
}

// usageScanner is the local record store slice B provides: Scan walks new
// records (incremental after the first pass) and Summary aggregates one
// range of what has been scanned so far.
type usageScanner interface {
	Scan(ctx context.Context, progress func(done, total int64)) error
	Summary(r aiusage.Range, now time.Time) aiusage.Summary
}

// usageQuotaState keeps the latest answer and the last usable windows.
// attemptAt enforces each provider's cadence; retryTill is server backoff.
type usageQuotaState struct {
	latest, good aiusage.Quota
	attemptAt    time.Time // when the last request was dispatched
	retryTill    time.Time
	busy         bool
	generation   int // distinguishes dispatches when fake-clock times tie
}

// usageFeature is the AI 用量 page: the panelPage whose ear icon and stage
// usage_page.go draws, and the module that schedules refreshes from the
// monitor tick.
type usageFeature struct {
	host *shelf
	deps usageDeps

	quota      [2]usageQuotaState // indexed by aiusage.Tool, via quotaIndex
	scanDone   atomic.Int64
	scanTotal  atomic.Int64
	localBusy  bool
	scanned    bool      // a local pass finished at least once (errors count)
	localErr   error     // last local scan/summary error, nil after success
	localAt    time.Time // last successful local refresh (drives 「N 前更新」)
	lastLocal  time.Time // last local refresh attempt (schedules the next)
	summary    aiusage.Summary
	hasSummary bool
	scanSrc    aiusage.Sources // the sources the last local scan was built with
	rangeSel   aiusage.Range
	wasVisible bool
	hoverTool  aiusage.Tool // the quota card under the pointer this frame

	// workGen/workCancel retire local scans and cancel in-flight fetches.
	// Completed quota answers remain useful after a page change.
	workGen    int
	workCtx    context.Context
	workCancel context.CancelFunc
}

// newUsageFeature wires the feature; integration registers it as a page
// and appends it to the modules.
func newUsageFeature(a *shelf, deps usageDeps) *usageFeature {
	f := &usageFeature{host: a, deps: deps}
	a.usage = f // hide/setTab/setAIUsage retire its work synchronously
	if f.deps.now == nil {
		f.deps.now = time.Now
	}
	return f
}

// usageSources maps the aiUsage.local* switches onto the scanner's source
// selection; read it at refresh time so a settings flip applies at once.
func usageSources(cfg *settings.AIUsageSettings) aiusage.Sources {
	return aiusage.Sources{Omp: cfg.LocalOmp, Claude: cfg.LocalClaude, Codex: cfg.LocalCodex}
}

func (f *usageFeature) cfg() *settings.AIUsageSettings { return &f.host.settings.AIUsage }

// quotaIndex maps the quota tools onto the fixed [2]state slots.
func quotaIndex(t aiusage.Tool) int {
	if t == aiusage.ToolClaude {
		return 1
	}
	return 0
}

// quotaTools is the card order: Codex first, then Claude Code (设计稿).
func quotaTools() []aiusage.Tool { return []aiusage.Tool{aiusage.ToolCodex, aiusage.ToolClaude} }

// localTools is the local-usage order: omp, Codex, then Claude Code.
func localTools() []aiusage.Tool {
	return []aiusage.Tool{aiusage.ToolOmp, aiusage.ToolCodex, aiusage.ToolClaude}
}

// quotaOn reports whether tool's quota card is enabled.
func (f *usageFeature) quotaOn(t aiusage.Tool) bool {
	cfg := f.cfg()
	if t == aiusage.ToolClaude {
		return cfg.QuotaClaude && f.deps.fetchClaude != nil
	}
	return cfg.QuotaCodex && f.deps.fetchCodex != nil
}

// localOn reports whether the local usage section is enabled.
func (f *usageFeature) localOn() bool {
	cfg := f.cfg()
	return (cfg.LocalOmp || cfg.LocalClaude || cfg.LocalCodex) && f.deps.newScanner != nil
}

// usageVisible reports whether the AI 用量 page is the one the open panel
// shows: refreshes only happen while the user can see them.
func (f *usageFeature) usageVisible() bool {
	return f.host.visible && f.host.currentPage().id() == pageUsage
}

// workContext returns the background context current work runs under; it is
// (re)created after stopWork cancelled the previous one. Main thread only.
func (f *usageFeature) workContext() context.Context {
	if f.workCtx == nil || f.workCtx.Err() != nil {
		f.workCtx, f.workCancel = context.WithCancel(context.Background())
	}
	return f.workCtx
}

// stopWork retires everything running in the background: the shared
// context is cancelled so workers stop at their next checkpoint (fetchers
// check it before reading credentials, the scanner before each file and
// before writing its cache), busy flags clear so nothing looks in flight,
// and workGen invalidates any result that still arrives.
func (f *usageFeature) stopWork() {
	f.workGen++
	if f.workCancel != nil {
		f.workCancel()
		f.workCtx, f.workCancel = nil, nil
	}
	for i := range f.quota {
		f.quota[i].busy = false
	}
	f.localBusy = false
	f.invalidate()
}

// tick runs from the monitor loop (main thread). It does nothing while the
// feature is disabled; once the page becomes visible it refreshes stale
// data at once and keeps it fresh at the two intervals. Leaving visibility
// (feature off, panel hidden, page switched away) cancels in-flight work.
func (f *usageFeature) tick(now time.Time) {
	if !f.enabled() || !f.usageVisible() {
		if f.wasVisible {
			f.wasVisible = false
			f.stopWork()
		}
		return
	}
	f.wasVisible = true
	f.ensureFresh(now)
	// Keep repainting while the first scan's progress bar moves.
	if f.localBusy && !f.hasSummary {
		f.invalidate()
	}
}

// ensureFresh starts the refreshes whose data is due at now: quota per
// enabled tool every usageQuotaInterval (or its own RetryAfter), the local
// summary every usageLocalInterval or once the chosen range has none.
func (f *usageFeature) ensureFresh(now time.Time) {
	for _, t := range quotaTools() {
		if !f.quotaOn(t) {
			continue
		}
		st := &f.quota[quotaIndex(t)]
		if st.busy || now.Before(st.retryTill) {
			continue
		}
		if st.attemptAt.IsZero() || now.Sub(st.attemptAt) >= usageQuotaInterval {
			f.startQuota(t)
		}
	}
	if f.localOn() && !f.localBusy {
		missing := f.summary.Range != f.rangeSel || !f.hasSummary
		// The published summary was aggregated from the sources the scan
		// was built with; a chip flip since then makes it stale.
		staleSrc := f.hasSummary && f.scanSrc != usageSources(f.cfg())
		if f.lastLocal.IsZero() || missing || staleSrc || now.Sub(f.lastLocal) >= usageLocalInterval {
			f.startLocal(now)
		}
	}
}

// requestRefresh rebuilds local usage and asks each enabled quota source
// within its minimum gap and RetryAfter. Every click gets a status toast,
// even when a fast local scan leaves no visibly busy frame.
func (f *usageFeature) requestRefresh() {
	if !f.enabled() {
		return
	}
	now := f.deps.now()
	started := false
	var wait time.Duration
	for _, t := range quotaTools() {
		if !f.quotaOn(t) {
			continue
		}
		minGap := usageQuotaManual
		if t == aiusage.ToolClaude {
			minGap = usageQuotaInterval // Claude 429s below a 5-minute pace
		}
		st := &f.quota[quotaIndex(t)]
		if st.busy {
			started = true
			continue
		}
		due := st.attemptAt.Add(minGap)
		if st.retryTill.After(due) {
			due = st.retryTill
		}
		if now.Before(due) {
			if d := due.Sub(now); wait == 0 || d < wait {
				wait = d
			}
			continue
		}
		f.startQuota(t)
		started = true
	}
	if f.localOn() {
		if !f.localBusy {
			f.startLocal(now)
		}
		started = true
	}
	// A held-back quota source is what makes a click look dead when the
	// local scan is instant: say when it may be asked again.
	switch {
	case wait > 0:
		f.host.toastStatus("刚查过，"+usageWait(wait)+"后可再刷新", time.Time{}, false)
	case started:
		f.host.toastStatus("正在刷新…", time.Time{}, false)
	default:
		f.host.toastStatus("没有可刷新的来源", time.Time{}, false)
	}
}

// startQuota begins one fetch for tool on a worker goroutine, under the
// feature's work context so hiding the panel or disabling the page stops
// it; a result from a retired generation still reaches gotQuota, which
// records it without touching the busy flag.
func (f *usageFeature) startQuota(tool aiusage.Tool) {
	st := &f.quota[quotaIndex(tool)]
	if st.busy {
		return
	}
	fetch := f.deps.fetchCodex
	if tool == aiusage.ToolClaude {
		fetch = f.deps.fetchClaude
	}
	if fetch == nil {
		return
	}
	st.busy = true
	attempted := f.deps.now()
	st.attemptAt = attempted
	f.invalidate()
	gen := f.workGen
	st.generation = gen
	base := f.workContext()
	goWork(func() {
		ctx, cancel := context.WithTimeout(base, usageQuotaTimeout)
		q := fetch(ctx)
		// Our own retirement (hide, page switch, disable) cancels ctx; the
		// timeout belongs to the request. Only the former means the fetch
		// says nothing about the endpoint.
		aborted := errors.Is(ctx.Err(), context.Canceled)
		cancel()
		q.Tool = tool
		dispatchMain(func() { f.gotQuota(q, gen, attempted, aborted) })
	})
}

// gotQuota retains completed answers even after a page change, so reopening
// never shows an unanswered card for a request that already finished.
// Cancelled Codex work may be retried immediately. Claude attempts still
// retain their five-minute floor because cancellation cannot prove that the
// server never received the request, but the failed outcome of a cancelled
// fetch never replaces a previous answer (that would fake 网络不通); a first
// look still publishes it so the card cannot sit on 正在查询…. Server
// RetryAfter always survives.
func (f *usageFeature) gotQuota(q aiusage.Quota, gen int, attempted time.Time, aborted bool) {
	st := &f.quota[quotaIndex(q.Tool)]
	if q.RetryAfter > 0 {
		at := q.FetchedAt
		if at.IsZero() {
			at = f.deps.now()
		}
		if until := at.Add(q.RetryAfter); until.After(st.retryTill) {
			st.retryTill = until
		}
	}
	if aborted && q.Status == aiusage.QuotaFailed && q.RetryAfter <= 0 {
		if st.generation == gen && st.attemptAt.Equal(attempted) {
			st.busy = false
			if q.Tool == aiusage.ToolCodex {
				st.attemptAt = time.Time{}
			}
		}
		if q.Tool == aiusage.ToolCodex || !st.latest.FetchedAt.IsZero() {
			f.invalidate()
			return
		}
	}
	if q.FetchedAt.IsZero() {
		q.FetchedAt = f.deps.now()
	}
	if st.generation == gen && st.attemptAt.Equal(attempted) {
		st.busy = false // this answer belongs to the attempt still in flight
	}
	if q.FetchedAt.Before(st.latest.FetchedAt) {
		return
	}
	st.latest = q
	if q.Status == aiusage.QuotaOK && (q.FiveHour != nil || q.Weekly != nil) {
		st.good = q
	}
	f.invalidate()
}

// startLocal begins the local refresh: scan what's new, then aggregate the
// currently selected range. Both run on the worker under the feature's
// work context; only the progress counters (atomics) and the published
// summary reach the UI.
func (f *usageFeature) startLocal(now time.Time) {
	if f.localBusy {
		return
	}
	scanner := f.deps.newScanner
	if scanner == nil {
		return
	}
	sc := scanner()
	if sc == nil {
		return
	}
	f.localBusy = true
	f.lastLocal = now
	f.scanSrc = usageSources(f.cfg())
	f.scanDone.Store(0)
	f.scanTotal.Store(0)
	f.invalidate()
	r := f.rangeSel // captured on the main thread: the worker never reads f.*
	gen := f.workGen
	base := f.workContext()
	goWork(func() {
		ctx, cancel := context.WithTimeout(base, usageScanTimeout)
		scanErr := sc.Scan(ctx, func(done, total int64) {
			f.scanDone.Store(done)
			f.scanTotal.Store(total)
		})
		var sum aiusage.Summary
		if ctx.Err() == nil {
			sum = sc.Summary(r, f.deps.now())
		}
		cancel()
		dispatchMain(func() { f.gotLocal(sum, r, scanErr, gen) })
	})
}

// gotLocal publishes the local refresh outcome on the main thread. A
// summary the user has since switched away from is dropped and a fresh
// pass for the new range is started instead; a result retired by stopWork
// while the worker ran is dropped outright.
func (f *usageFeature) gotLocal(sum aiusage.Summary, r aiusage.Range, err error, gen int) {
	if gen != f.workGen || !f.enabled() || !f.usageVisible() {
		return // retired by stopWork or a page/settings change while in flight
	}
	f.localBusy = false
	f.scanned = true
	f.localErr = err
	if err == nil {
		sum.Range = r
		f.summary, f.hasSummary = sum, true
		f.localAt = f.deps.now()
	}
	f.invalidate()
	// The range or the source selection may have moved on while the worker
	// ran: refresh it.
	if f.localOn() && (f.summary.Range != f.rangeSel || f.scanSrc != usageSources(f.cfg())) && f.usageVisible() {
		f.startLocal(f.deps.now())
	}
}

// quotaShown is the quota a card draws: the latest answer while it was
// good, otherwise the last good one (shown dimmed as stale data). stale is
// false when nothing good ever arrived, so no fake numbers render.
func (f *usageFeature) quotaShown(t aiusage.Tool) (q aiusage.Quota, stale bool) {
	st := &f.quota[quotaIndex(t)]
	if st.latest.Status == aiusage.QuotaOK && (st.latest.FiveHour != nil || st.latest.Weekly != nil) {
		return st.latest, false
	}
	return st.good, !st.good.FetchedAt.IsZero()
}

// offline reports whether an enabled quota source's last fetch failed on
// the network: the module bar appends 「· 网络不通」 while stale data shows.
func (f *usageFeature) offline() bool {
	for _, t := range quotaTools() {
		if f.quotaOn(t) && f.quota[quotaIndex(t)].latest.Status == aiusage.QuotaFailed {
			return true
		}
	}
	return false
}

// paceOf returns the even-pace estimate for a window, safe when the
// estimator was not injected.
func (f *usageFeature) paceOf(w *aiusage.Window, now time.Time) (aiusage.PaceInfo, bool) {
	if w == nil || f.deps.pace == nil {
		return aiusage.PaceInfo{}, false
	}
	return f.deps.pace(*w, now), true
}

// lastUpdatedAt is the newest moment any shown data was fetched (the last
// good quota answer, not a failed attempt); zero while nothing ever
// arrived.
func (f *usageFeature) lastUpdatedAt() time.Time {
	var t time.Time
	for _, tool := range quotaTools() {
		q, _ := f.quotaShown(tool)
		if at := q.FetchedAt; at.After(t) {
			t = at
		}
	}
	if f.localAt.After(t) {
		t = f.localAt
	}
	return t
}

// invalidate asks the panel for a repaint after state changed.
func (f *usageFeature) invalidate() {
	if a := f.host; a != nil && a.win != nil {
		a.win.Invalidate()
	}
}
