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

// usageQuotaState is one tool's latest fetch: latest is the last answer
// whatever its Status, good the last answer that produced windows (shown
// dimmed as stale data when the latest attempt failed). Throttling lives
// in attemptAt and retryTill, both set the moment a request is dispatched
// or an answer is processed — before generation checks — so cancelling the
// UI work (hide/disable) never silently re-opens the endpoint. retryTill
// is the server-sent RetryAfter deadline, the only extra hold-back on top
// of the page's own refresh intervals.
type usageQuotaState struct {
	latest, good aiusage.Quota
	attemptAt    time.Time // when the last request was dispatched
	retryTill    time.Time
	busy         bool
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

	// workGen/workCancel retire background work: workCtx is cancelled when
	// the feature is disabled, hidden or the page left, and workGen rejects
	// results that land after that. Both are main-thread only.
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

// requestRefresh is the module-bar button: the local summary rebuilds at
// once and each enabled quota tool refetches, but never sooner than its
// minimum gap (usageQuotaManual, five minutes for Claude Code) or a server
// RetryAfter said.
func (f *usageFeature) requestRefresh() {
	if !f.enabled() {
		return
	}
	now := f.deps.now()
	if f.localOn() && !f.localBusy {
		f.startLocal(now)
	}
	for _, t := range quotaTools() {
		if !f.quotaOn(t) {
			continue
		}
		minGap := usageQuotaManual
		if t == aiusage.ToolClaude {
			minGap = usageQuotaInterval // Claude 429s below a 5-minute pace
		}
		st := &f.quota[quotaIndex(t)]
		if st.busy || now.Before(st.retryTill) || now.Sub(st.attemptAt) < minGap {
			continue
		}
		f.startQuota(t)
	}
}

// startQuota begins one fetch for tool on a worker goroutine, under the
// feature's work context so hiding the panel or disabling the page stops
// it; a result from a retired generation is dropped in gotQuota.
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
	st.attemptAt = f.deps.now()
	f.invalidate()
	gen := f.workGen
	base := f.workContext()
	goWork(func() {
		ctx, cancel := context.WithTimeout(base, usageQuotaTimeout)
		q := fetch(ctx)
		cancel()
		q.Tool = tool
		dispatchMain(func() { f.gotQuota(q, gen) })
	})
}

// gotQuota records a fetch outcome (main thread): a good answer replaces
// the shown data, a bad one keeps the last good numbers as stale data.
// A result from before the last stopWork — or one arriving while the
// feature is disabled or the page hidden — is dropped, but its server-sent
// RetryAfter still applies: dropping the backoff would let a retired 429
// reopen the endpoint on the next tick. retryTill is counted from when
// the response arrived (FetchedAt); the usageQuotaInterval gap is checked
// against attemptAt, set at dispatch, so neither a discarded answer nor a
// cleared busy flag re-opens the throttle.
func (f *usageFeature) gotQuota(q aiusage.Quota, gen int) {
	st := &f.quota[quotaIndex(q.Tool)]
	if q.RetryAfter > 0 {
		at := q.FetchedAt
		if at.IsZero() {
			at = f.deps.now()
		}
		st.retryTill = at.Add(q.RetryAfter)
	}
	if gen != f.workGen || !f.enabled() || !f.usageVisible() {
		return // retired by stopWork or a page/settings change while in flight
	}
	st.busy = false
	if q.FetchedAt.IsZero() {
		q.FetchedAt = f.deps.now()
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
