//go:build darwin

package main

// AI 用量页（设计稿 design/usage-v1.png）：上排两张套餐额度双圈卡
// （外圈每周、内圈 5 小时，画「剩余」，悬停出 Synara 式明细），下排本机
// token 用量（分段范围、4 格指标、堆叠柱图、图例）。取数调度在
// usage_module.go，这里只有绘制和格式化。
//
// 颜色例外：额度圈（每周 #30D158、5 小时 #FF9F0A、低余量 #FF453A）与
// 来源色（omp 蓝、Claude Code #D97757、Codex 灰白）是 DESIGN.md
// 「唯一强调色」规则的第二个例外，本文件是唯一使用它们的地方。

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/aiusage"
	"shelf/internal/settings"
)

// Quota ring and source colours — the DESIGN.md single-accent rule's only
// other exception besides the now-playing cover tint (DESIGN.md「AI 用量」).
var (
	usageWeekly  = ui.Hex("#30D158")            // 外圈：每周；费用数值同色
	usageFive    = ui.Hex("#FF9F0A")            // 内圈：5 小时；节奏告警同色
	usageClaude  = ui.Hex("#D97757")            // Claude Code 的来源色与标志色
	usageCodex   = ui.Hex("#B8B8C0")            // Codex 的来源色
	usagePopBG   = ui.Hex("#1C1C1E")            // 悬停明细底色
	usagePopEdge = ui.RGBA(255, 255, 255, 0.12) // 悬停明细描边
	usageKpiSep  = ui.RGBA(255, 255, 255, 0.10) // 指标分隔线
	usageGrid    = ui.RGBA(255, 255, 255, 0.08) // 柱图网格线
	usageBase    = ui.RGBA(255, 255, 255, 0.12) // 柱图基线
)

// The quota card's metrics from the mock (.qc{height:88}, .quota{gap:6}).
const (
	usageCardH   = 88
	usageCardGap = 6
)

// Symbols the page adds to panel.go's set: the refresh button and the ear
// icon's fallback gauge. The two tools' marks inside the ring are the
// official logos: OpenAI's Blossom (white) from the brand-page download
// https://cdn.openai.com/brand/openai-logos.zip (openai.com/brand) and
// Anthropic's Claude Spark (#D97757) from the press kit at
// https://anthropic.com/press-kit — both scaled only, per
// resources/THIRD_PARTY_NOTICES.txt.
var (
	refreshSymbol = symbol(`<path d="M20 12a8 8 0 1 1-2.34-5.66M20 3.5v4.5h-4.5"/>`)
	gaugeSymbol   = symbol(`<path d="M4 17a8 8 0 1 1 16 0"/><path d="M12 17l4.5-5"/><circle cx="12" cy="17" r="1.6"/>`)
	codexLogo     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 716 716" xmlns="http://www.w3.org/2000/svg"><path fill="white" d="M508.749 317.399C516.777 287.314 508.991 253.884 485.389 230.282C461.788 206.681 428.36 198.895 398.273 206.923C376.231 184.928 343.39 174.956 311.148 183.596C278.906 192.234 255.45 217.292 247.36 247.361C217.291 255.451 192.233 278.91 183.595 311.149C174.957 343.391 184.927 376.232 206.924 398.274C198.896 428.359 206.683 461.789 230.284 485.391C253.885 508.992 287.313 516.779 317.401 508.75C339.442 530.745 372.286 540.717 404.525 532.079C436.767 523.441 460.223 498.384 468.313 468.315C498.383 460.224 523.44 436.766 532.078 404.526C540.716 372.285 530.747 339.443 508.749 317.402V317.399ZM470.899 244.776C486.892 260.77 493.488 282.601 490.687 303.412L415.577 260.046C412.411 258.218 408.509 258.218 405.345 260.046L317.401 310.82V277.526C317.401 275.191 318.652 273.005 320.676 271.837L387.644 233.174C414.178 218.353 448.346 222.223 470.901 244.776H470.899ZM357.837 311.144L398.275 334.491V381.185L357.837 404.532L317.398 381.185V334.491L357.837 311.144ZM264.776 269.693C265.207 239.305 285.644 211.649 316.453 203.393C338.3 197.54 360.505 202.744 377.127 215.573L302.014 258.937C298.848 260.764 296.898 264.144 296.898 267.798V369.346L268.065 352.699C266.043 351.531 264.776 349.353 264.776 347.017V269.691V269.693ZM203.391 316.454C209.244 294.608 224.854 277.978 244.276 269.999V356.73C244.276 360.384 246.226 363.763 249.392 365.591L337.337 416.365L308.503 433.013C306.481 434.181 303.961 434.188 301.939 433.02L234.971 394.357C208.868 378.789 195.138 347.261 203.391 316.454ZM244.775 470.9C228.781 454.906 222.186 433.075 224.986 412.264L300.096 455.63C303.263 457.457 307.164 457.457 310.328 455.63L398.273 404.856V438.149C398.273 440.485 397.022 442.671 394.997 443.839L328.029 482.502C301.495 497.322 267.327 493.452 244.772 470.9H244.775ZM450.897 445.982C450.466 476.371 430.029 504.027 399.22 512.283C377.373 518.136 355.168 512.932 338.547 500.102L413.659 456.738C416.826 454.911 418.775 451.532 418.775 447.877V346.329L447.609 362.977C449.631 364.145 450.897 366.323 450.897 368.659V445.985V445.982ZM512.282 399.221C506.429 421.068 490.819 437.697 471.397 445.676V358.946C471.397 355.292 469.448 351.912 466.281 350.085L378.336 299.311L407.17 282.663C409.192 281.495 411.712 281.487 413.734 282.655L480.702 321.318C506.805 336.887 520.536 368.415 512.282 399.221Z"/></svg>`))
	claudeLogo    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 94 94" xmlns="http://www.w3.org/2000/svg"><path fill="#D97757" d="M18.7657 62.4437L37.1822 52.1167L37.4857 51.2122L37.1822 50.7085H36.2715L33.1852 50.5208L22.6615 50.2391L13.5545 49.8636L4.70044 49.3942L2.47428 48.9248L0.399902 46.1553L0.602281 44.794L2.47428 43.5266L5.15579 43.7613L11.0754 44.1837L19.98 44.794L26.4055 45.1695L35.9679 46.1553H37.4857L37.6881 45.545L37.1822 45.1695L36.7774 44.794L27.5692 38.5508L17.6021 31.9791L12.3908 28.1769L9.60812 26.2524L8.19147 24.4686L7.58433 20.5256L10.1141 17.7091L13.5545 17.9438L14.4146 18.1785L17.9056 20.8542L25.343 26.6279L35.0572 33.7629L36.4739 34.9364L37.0443 34.5514L37.1316 34.2792L36.4739 33.1996L31.212 23.6706L25.596 13.9539L23.0663 9.91695L22.4086 7.52296C22.1538 6.51831 22.0038 5.68714 22.0038 4.65957L24.8877 0.716544L26.5067 0.200195L30.4025 0.716544L32.0215 2.12477L34.4501 7.66379L38.3458 16.3478L44.4172 28.1769L46.188 31.6975L47.1493 34.9364L47.5035 35.9222H48.1106V35.3589L48.6166 28.6933L49.5273 20.5256L50.438 10.0108L50.7415 7.05356L52.2088 3.48605L55.1433 1.56148L57.42 2.64112L59.292 5.31674L59.039 7.05356L57.926 14.2824L55.7504 25.5952L54.3337 33.1996H55.1433L56.1046 32.2138L59.9497 27.1442L66.3752 19.0704L69.2085 15.8784L72.5478 12.3579L74.6728 10.668H78.7203L81.6548 15.0804L80.3394 19.6337L76.1906 24.8911L72.7502 29.3504L67.8172 35.9595L64.7562 41.2734L65.0307 41.7118L65.7681 41.6489L76.8989 39.255L82.9197 38.1753L90.1041 36.9549L93.3422 38.457L93.6963 40.006L92.4315 43.151L84.7411 45.0287L75.7353 46.8594L62.3244 50.0164L62.1759 50.1358L62.3512 50.3958L68.399 50.9432L70.9794 51.084H77.3037L89.0922 51.9759L92.1785 53.9944L93.9999 56.4822L93.6963 58.4068L88.9404 60.8008L82.5655 59.2987L67.6401 55.7312L62.5301 54.4638H61.8217V54.8862L66.0717 59.064L73.9139 66.1051L83.6786 75.2116L84.1845 77.4648L82.9197 79.2485L81.6042 79.0608L73.0032 72.5829L69.6639 69.6726L62.1759 63.3356H61.67V63.9928L63.3902 66.5276L72.5478 80.2812L73.0032 84.5059L72.3454 85.8672L69.9675 86.7121L67.3871 86.2427L61.9735 78.6852L56.4587 70.2359L52.0064 62.6315L51.4687 62.971L48.8189 91.2654L47.6047 92.7206L44.7714 93.8002L42.3934 92.0164L41.1286 89.1061L42.3934 83.3324L43.9113 75.8219L45.1255 69.8604L46.2386 62.4437L46.9184 59.9661L46.8583 59.8003L46.3153 59.8916L40.7238 67.5603L32.2239 79.0608L25.4948 86.2427L23.8758 86.8999L21.0931 85.4447L21.3461 82.863L22.9145 80.5629L32.2239 68.7338L37.8399 61.3641L41.4594 57.1337L41.4242 56.5218L41.2244 56.5048L16.489 72.6299L12.0873 73.1932L10.1647 71.4094L10.4176 68.4991L11.3283 67.5603L18.7657 62.4437Z"/></svg>`))
)

// toolName is a tool's display name on cards and the legend.
func toolName(t aiusage.Tool) string {
	switch t {
	case aiusage.ToolClaude:
		return "Claude Code"
	case aiusage.ToolCodex:
		return "Codex"
	default:
		return "omp"
	}
}

// toolLogo is the tool's mark inside the double ring in its own colour,
// with the icon box size that shows the mark at ~18pt: both official
// SVGs pad the artwork inside their viewBox — the Blossom's ink spans
// the centre 51.1% of 716, the Spark ~99.8% of 94 (and rides slightly
// right of the viewBox centre, which balances the spark's leftward
// tail) — so the boxes are 35.2 and 18pt. Scaling is uniform: the
// viewBoxes are square and Icon keeps the aspect ratio.
func toolLogo(t aiusage.Tool) (*ui.SVG, ui.Color, float32) {
	if t == aiusage.ToolClaude {
		return claudeLogo, usageClaude, 18
	}
	return codexLogo, ink, 35.2
}

// toolColor is a tool's stacked-bar and legend colour.
func toolColor(t aiusage.Tool) ui.Color {
	switch t {
	case aiusage.ToolClaude:
		return usageClaude
	case aiusage.ToolCodex:
		return usageCodex
	default:
		return accent
	}
}

// localSourceOn reports whether the local-usage chip for tool is on.
func (f *usageFeature) localSourceOn(t aiusage.Tool) bool {
	cfg := f.cfg()
	switch t {
	case aiusage.ToolClaude:
		return cfg.LocalClaude
	case aiusage.ToolCodex:
		return cfg.LocalCodex
	default:
		return cfg.LocalOmp
	}
}

// --- panelPage ----------------------------------------------------------

func (f *usageFeature) id() pageID         { return pageUsage }
func (f *usageFeature) label() string      { return "AI 用量" }
func (f *usageFeature) enabled() bool      { return f.host.settings.AIUsage.Enabled }
func (f *usageFeature) sharesHeight() bool { return false }

// icon is the left-ear's third icon: a miniature of the quota card's
// double ring showing the tool with the least 5-hour remaining (outer
// weekly, inner 5h); without any window data it falls back to a plain
// gauge. The ear recolours ordinary icons for hover, but the rings keep
// their own colours — the colours are the information.
func (f *usageFeature) icon(c *ui.Context, _ bool) ui.Element {
	if five, week, ok := f.leastWindow(); ok {
		e := ui.Box(c).Size(17, 17)
		e.Draw(func(p *ui.Painter, r ui.Rect) {
			lw := r.W * 2 / 17
			f.drawWindowRing(p, r, r.W*7.5/17, lw, week, usageWeekly, false, false)
			f.drawWindowRing(p, r, r.W*4/17, lw, five, usageFive, false, false)
		})
		return e
	}
	return ui.Icon(c, gaugeSymbol)
}

// leastWindow picks the tool with the least 5-hour remaining for the ear
// icon; its weekly window rides along on the outer ring. A tool without a
// 5-hour window never wins; with no 5-hour data at all, the least weekly
// remaining shows alone.
func (f *usageFeature) leastWindow() (five, week *aiusage.Window, ok bool) {
	best := math.Inf(1)
	for _, t := range quotaTools() {
		if !f.quotaOn(t) {
			continue
		}
		q, _ := f.quotaShown(t)
		if q.FiveHour == nil {
			continue
		}
		if left := 100 - q.FiveHour.UsedPct; left < best {
			best, five, week, ok = left, q.FiveHour, q.Weekly, true
		}
	}
	if ok {
		return five, week, true
	}
	best = math.Inf(1)
	for _, t := range quotaTools() {
		if !f.quotaOn(t) {
			continue
		}
		q, _ := f.quotaShown(t)
		if q.Weekly == nil {
			continue
		}
		if left := 100 - q.Weekly.UsedPct; left < best {
			best, week, ok = left, q.Weekly, true
		}
	}
	return nil, week, ok
}

// moduleTitle is the module bar's left side: the page name, then how long
// ago data last arrived (plus 「· 网络不通」 while a quota source is down
// and stale data shows).
func (f *usageFeature) moduleTitle() (title, count string) {
	title = "AI 用量"
	at := f.lastUpdatedAt()
	if at.IsZero() {
		if f.quota[0].busy || f.quota[1].busy || f.localBusy {
			return title, "正在更新…"
		}
		return title, ""
	}
	count = usageAgo(at, f.deps.now()) + "更新"
	if f.offline() {
		count += " · 网络不通"
	}
	return title, count
}

// moduleTrailing is the refresh button; 「刷新」 is its tooltip and
// accessible name.
func (f *usageFeature) moduleTrailing(c *ui.Context, live bool) []ui.Element {
	b := f.host.iconButton(c, "usage-refresh", refreshSymbol, "刷新")
	if b.Clicked() && live {
		f.requestRefresh()
	}
	return []ui.Element{b}
}

// stageH is what the body wants below the module bar: the card row plus
// the local section, or the shared empty state. sharesHeight is false, so
// the panel eases to this height on every switch.
func (f *usageFeature) stageH() int {
	cards := 0
	for _, t := range quotaTools() {
		if f.quotaOn(t) {
			cards++
		}
	}
	if cards == 0 && !f.localOn() {
		return 0
	}
	h := 0
	if cards > 0 {
		h = 2*usageCardGap + usageCardH
	}
	if f.localOn() {
		h += f.localSectionH()
	}
	return h
}

// localSectionH is 「本机用量」's height: the header plus either the
// KPI + chart block or the scan/progress card. The chart block is
// 22 head + 8 + ~49 KPI + 10 + 64 chart + 3 + ~12 axis + 8 + ~11 legend —
// ~187 laid out — plus the stage's fixed shrink and breathing room under
// the legend, so nothing ends flush with the panel's bottom edge.
func (f *usageFeature) localSectionH() int {
	if f.hasSummary && f.summary.Range == f.rangeSel {
		return 210
	}
	return 112 // 22 head + 8 + 64 scan card, plus the same slack
}

// body is everything under the module bar. The detail popover anchors to
// the hovered card with AttachTo and is PassThrough, so it never takes the
// pointer itself: it exists exactly while a card is hovered, like the
// mock's hover detail.
func (f *usageFeature) body(c *ui.Context, _ bool, _ bool) ui.Element {
	if !f.anySource() {
		return f.host.emptyState(c, gaugeSymbol, "没有选中的数据来源", "在设置 → AI 用量里打开套餐额度或本机用量")
	}
	root := ui.Column(c.Key("usage")).FillWidth().Grow(1).MinHeight(0).PassThrough()
	root.Children(func() {
		f.hoverTool = "" // rebuilt each frame by the cards below
		var anchors map[aiusage.Tool]ui.Element
		ui.Scroll(c.Key("usage-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c.Key("usage-body")).FillWidth().Children(func() {
				anchors = f.quotaRow(c)
				if f.localOn() {
					f.localSection(c)
				}
			})
		})
		// The popover is built after the scroll so it paints on top;
		// AttachTo follows the card's laid-out box and flips up when the
		// stage is short.
		if card, ok := anchors[f.hoverTool]; ok && card.Valid() {
			pop := ui.Column(c.Key("usage-pop")).AttachTo(card, ui.AnchorBottomStart, ui.AnchorTopStart).
				Margin(6, 0, 0, 0).Width(300).Radius(14).Background(usagePopBG).
				Border(0.5, usagePopEdge).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.55)).
				Padding(12, 14, 12, 14).PassThrough()
			pop.Children(func() { f.popoverBody(c, f.hoverTool) })
		}
	})
	return root
}

// anySource reports whether at least one card or the local section shows.
func (f *usageFeature) anySource() bool {
	for _, t := range quotaTools() {
		if f.quotaOn(t) {
			return true
		}
	}
	return f.localOn()
}

// quotaRow draws the enabled quota cards side by side and returns their
// elements for the popover's AttachTo.
func (f *usageFeature) quotaRow(c *ui.Context) map[aiusage.Tool]ui.Element {
	anchors := map[aiusage.Tool]ui.Element{}
	var tools []aiusage.Tool
	for _, t := range quotaTools() {
		if f.quotaOn(t) {
			tools = append(tools, t)
		}
	}
	if len(tools) == 0 {
		return anchors
	}
	ui.Row(c.Key("quota")).FillWidth().Gap(usageCardGap).Margin(usageCardGap, 0, usageCardGap, 0).Children(func() {
		for _, t := range tools {
			anchors[t] = f.quotaCard(c, t)
		}
	})
	return anchors
}

// quotaCard draws one tool's card: the double ring plus name/plan, two
// window lines and a pace line — or a state message when the fetch could
// not produce numbers.
func (f *usageFeature) quotaCard(c *ui.Context, tool aiusage.Tool) ui.Element {
	st := &f.quota[quotaIndex(tool)]
	shown, stale := f.quotaShown(tool)
	card := ui.Row(c.Key("quota-"+string(tool))).Height(usageCardH).Grow(1).MinWidth(0).
		Radius(14).Padding(0, 10, 0, 10).AlignItems(ui.Center).Gap(10)
	if _, _, over := card.PointerPosition(); over {
		card.Background(fill2)
		f.hoverTool = tool
	} else {
		card.Background(fill1)
	}
	card.Children(func() {
		f.quotaRing(c, tool, shown, stale)
		ui.Column(c.Key("info")).Grow(1).MinWidth(0).Children(func() {
			f.quotaInfo(c, tool, st, shown, stale)
		})
	})
	return card
}

// quotaInfo is the card's right side. Numbers show for a good fetch (or
// the last good one, dimmed, when the latest only failed to reach the
// endpoint); 未登录 and 已过期 always replace numbers with their message.
func (f *usageFeature) quotaInfo(c *ui.Context, tool aiusage.Tool, st *usageQuotaState, q aiusage.Quota, stale bool) {
	now := f.deps.now()
	ui.Row(c.Key("name")).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Text(c, toolName(tool)).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
		if q.Plan != "" {
			ui.Text(c, q.Plan).SingleLine().FontSize(10).FontWeight(600).TextColor(ink2).
				Background(fill2).Radius(6).Padding(1, 6, 1, 6)
		}
	})
	showNumbers := false
	switch {
	case st.latest.FetchedAt.IsZero():
		// First fetch not answered yet.
	case st.latest.Status == aiusage.QuotaOK:
		showNumbers = q.FiveHour != nil || q.Weekly != nil
	case st.latest.Status == aiusage.QuotaRateLimited || st.latest.Status == aiusage.QuotaFailed:
		showNumbers = stale // last good numbers stay, dimmed
	}
	if showNumbers {
		f.windowLine(c, q.FiveHour, "5 小时", usageFive, now)
		f.windowLine(c, q.Weekly, "每周", usageWeekly, now)
		f.paceLine(c, q, stale, now)
		return
	}
	if st.latest.FetchedAt.IsZero() {
		ui.Text(c, "正在查询…").FontSize(11).TextColor(ink3).Margin(4, 0, 0, 0)
		return
	}
	f.quotaMessage(c, tool, st.latest.Status)
}

// quotaMessage is the card's state text for the answers that produce no
// numbers: 未登录, 已过期, 限流, 查不到.
func (f *usageFeature) quotaMessage(c *ui.Context, tool aiusage.Tool, status aiusage.QuotaStatus) {
	var lines string
	switch status {
	case aiusage.QuotaNotLoggedIn:
		if tool == aiusage.ToolClaude {
			lines = "本机没有登录 Claude Code\n在终端运行 claude login 后显示"
		} else {
			lines = "本机没有登录 Codex\n在终端运行 codex login 后显示"
		}
	case aiusage.QuotaExpired:
		lines = fmt.Sprintf("登录已过期\n打开一次 %s 就会恢复", toolName(tool))
	case aiusage.QuotaRateLimited:
		lines = "额度查询暂时被限流\n稍后再看"
	default:
		lines = "暂时查不到额度\n稍后再看"
	}
	ui.Text(c, lines).FontSize(11).TextColor(ink3).Margin(4, 0, 0, 0)
}

// windowLine is one card row: coloured dot, window name, remaining % and
// its reset time (4:25 后重置 under a day, else 周六 02:00 重置).
func (f *usageFeature) windowLine(c *ui.Context, w *aiusage.Window, name string, col ui.Color, now time.Time) {
	left := -1.0
	if w != nil {
		left = 100 - w.UsedPct
	}
	if left >= 0 && left < 10 {
		col = danger
	}
	ui.Row(c.Key("wl-"+name)).AlignItems(ui.Center).Gap(4).Margin(5, 0, 0, 0).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(col)
		ui.Text(c, name).SingleLine().FontSize(11).TextColor(ink2).Width(34)
		pct, pctCol := "—", ink
		if w != nil {
			pct = fmt.Sprintf("%.0f%%", left)
			if left < 10 {
				pctCol = danger
			}
		}
		ui.Text(c, pct).SingleLine().FontSize(12).FontWeight(600).TextColor(pctCol).MinWidth(28)
		if w != nil {
			ui.Text(c, usageReset(w.ResetsAt, now)).SingleLine().FontSize(10).TextColor(ink3)
		}
	})
}

// paceLine is the card's last row: 「N 分钟前的数据」 for stale numbers,
// red 快用完了 under 10% left, the orange depletion estimate naming the
// window that runs out first, otherwise the quiet 正常 line.
func (f *usageFeature) paceLine(c *ui.Context, q aiusage.Quota, stale bool, now time.Time) {
	text, col := "节奏正常，可撑到重置", ink3
	if stale {
		text = usageAgo(q.FetchedAt, now) + "的数据"
	} else {
		lowest := 101.0
		for _, w := range []*aiusage.Window{q.FiveHour, q.Weekly} {
			if w != nil && 100-w.UsedPct < lowest {
				lowest = 100 - w.UsedPct
			}
		}
		runsOut, d := false, time.Duration(0)
		name := ""
		for i, w := range []*aiusage.Window{q.FiveHour, q.Weekly} {
			if pi, ok := f.paceOf(w, now); ok && !pi.LastsUntilReset {
				if !runsOut || pi.RunsOutIn < d {
					d = pi.RunsOutIn
					name = [...]string{"5 小时", "每周"}[i]
				}
				runsOut = true
			}
		}
		switch {
		case lowest < 10:
			text, col = "快用完了", danger
		case runsOut:
			text, col = name+" · "+usageDeplete(d)+"用完", usageFive
		}
	}
	ui.Text(c, text).SingleLine().FontSize(11).TextColor(col).Margin(5, 0, 0, 0)
}

// quotaRing draws the card's 64pt double ring and centres the 18pt tool
// logo on the circle centre — the mock's .rg/.logo stack; Center on the
// ring box puts the child's geometric centre over (32, 32), inside the
// inner circle's hollow (r20 − stroke ≈ 17.5pt). Without window data it
// leaves the tracks and dims the logo.
func (f *usageFeature) quotaRing(c *ui.Context, tool aiusage.Tool, q aiusage.Quota, stale bool) {
	e := ui.Box(c.Key("ring")).Size(64, 64).Center()
	has := q.FiveHour != nil || q.Weekly != nil
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		f.drawWindowRing(p, r, 28, 5, q.Weekly, usageWeekly, stale, true)
		f.drawWindowRing(p, r, 20, 5, q.FiveHour, usageFive, stale, true)
	})
	e.Children(func() {
		logo, col, size := toolLogo(tool)
		ic := ui.Icon(c, logo).Size(size, size).TextColor(col)
		if !has {
			ic.Opacity(0.4)
		}
	})
}

// drawWindowRing paints one arc of the double ring inside r: the fill3
// track, the remaining fraction starting at 12 o'clock clockwise (round
// caps via StrokePath), and the white tick at the even-pace position. A
// nil window paints the bare track; dim fades the arc for stale data.
func (f *usageFeature) drawWindowRing(p *ui.Painter, r ui.Rect, rad, lw float32, w *aiusage.Window, col ui.Color, dim, tick bool) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	var track ui.Path
	track.Circle(cx, cy, rad)
	p.StrokePath(&track, lw, fill3)
	if w == nil {
		return
	}
	left := max(0, min(100, 100-w.UsedPct)) / 100
	if col == usageFive && left < 0.10 {
		col = danger
	}
	if dim {
		col = col.Alpha(0.45)
	}
	strokeRingArc(p, cx, cy, rad, lw, float32(left), col)
	if tick {
		if pi, ok := f.paceOf(w, f.deps.now()); ok {
			expected := float32(max(0, min(100, pi.ExpectedLeftPct)) / 100)
			strokeRingTick(p, cx, cy, rad, lw, expected)
		}
	}
}

// strokeRingArc strokes the arc that is the fraction frac of a circle,
// starting at 12 o'clock clockwise (the Circle path starts at 3 o'clock
// and winds down, so 0.75 is the top). frac ≤ 0.25 stays inside the last
// quarter of the path; a larger fraction wraps around to its beginning.
func strokeRingArc(p *ui.Painter, cx, cy, rad, lw, frac float32, col ui.Color) {
	if frac <= 0 {
		return
	}
	var head ui.Path
	head.Circle(cx, cy, rad).Trim(0.75, min(1, 0.75+frac))
	p.StrokePath(&head, lw, col)
	if frac > 0.25 {
		var tail ui.Path
		tail.Circle(cx, cy, rad).Trim(0, frac-0.25)
		p.StrokePath(&tail, lw, col)
	}
}

// strokeRingTick draws the small white marker across the ring at the
// even-pace-remaining fraction (same clock as strokeRingArc).
func strokeRingTick(p *ui.Painter, cx, cy, rad, lw, frac float32) {
	a := float64(frac)*2*math.Pi - math.Pi/2
	ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
	var tick ui.Path
	tick.MoveTo(cx+(rad-lw/2-1)*ca, cy+(rad-lw/2-1)*sa)
	tick.LineTo(cx+(rad+lw/2+1)*ca, cy+(rad+lw/2+1)*sa)
	p.StrokePath(&tick, 1.5, ui.RGB(255, 255, 255))
}

// --- hover detail --------------------------------------------------------

// popoverBody fills the Synara-style detail card: tool + plan, a legend,
// and per window a track bar with the even-pace tick and the pace delta.
func (f *usageFeature) popoverBody(c *ui.Context, tool aiusage.Tool) {
	q, _ := f.quotaShown(tool)
	now := f.deps.now()
	ui.Row(c.Key("ph")).AlignItems(ui.End).Children(func() {
		ui.Text(c, toolName(tool)).SingleLine().FontSize(14).FontWeight(600).TextColor(ink)
		ui.Box(c).Grow(1)
		if q.Plan != "" {
			ui.Text(c, q.Plan).SingleLine().FontSize(12).FontWeight(500).TextColor(ink2)
		}
	})
	ui.Row(c.Key("leg")).Gap(14).Margin(6, 0, 0, 0).Children(func() {
		if q.Weekly != nil {
			popLegend(c, usageWeekly, "每周 · 外圈")
		}
		if q.FiveHour != nil {
			popLegend(c, usageFive, "5 小时 · 内圈")
		}
	})
	if q.FiveHour != nil {
		f.popWindow(c, "5 小时", q.FiveHour, usageFive, now)
	}
	if q.Weekly != nil {
		f.popWindow(c, "每周", q.Weekly, usageWeekly, now)
	}
}

func popLegend(c *ui.Context, col ui.Color, s string) {
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(7, 7).Radius(4).Background(col).Margin(0, 5, 0, 0)
		ui.Text(c, s).SingleLine().FontSize(11).TextColor(ink2)
	})
}

// popWindow is one detail block: name, remaining and long reset over the
// track (fill3 bar, coloured remaining, white/red even-pace tick), then
// the pace delta and the depletion estimate.
func (f *usageFeature) popWindow(c *ui.Context, name string, w *aiusage.Window, col ui.Color, now time.Time) {
	left := max(0, min(100, 100-w.UsedPct))
	pi, hasPace := f.paceOf(w, now)
	ui.Row(c.Key("pw-"+name)).AlignItems(ui.End).Margin(12, 0, 0, 0).Children(func() {
		ui.Text(c, name).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
		ui.Text(c, fmt.Sprintf("剩 %.0f%%", left)).SingleLine().FontSize(11).FontWeight(600).TextColor(ink2).Margin(0, 0, 0, 6)
		ui.Box(c).Grow(1)
		ui.Text(c, usageResetLong(w.ResetsAt, now)).SingleLine().FontSize(11).TextColor(ink3)
	})
	bar := ui.Box(c.Key("trk-"+name)).FillWidth().Height(5).Radius(3).Margin(6, 0, 0, 0)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, fill3, r.H/2)
		if wf := float32(left) / 100; wf > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * wf, H: r.H}, col, r.H/2)
		}
		if hasPace {
			expected := float32(max(0, min(100, pi.ExpectedLeftPct)) / 100)
			tick := ui.RGB(255, 255, 255)
			if !pi.LastsUntilReset {
				tick = danger
			}
			x := r.X + r.W*expected
			p.Fill(ui.Rect{X: x - 1, Y: r.Y - 3, W: 2, H: 11}, tick, 1)
		}
	})
	ui.Row(c.Key("pf-"+name)).AlignItems(ui.End).Margin(5, 0, 0, 0).Children(func() {
		delta, dcol := "", ink3
		if hasPace {
			if pi.AheadPct > 0 {
				delta, dcol = fmt.Sprintf("比均匀用量超前 %.0f%%", pi.AheadPct), col
			} else {
				delta = fmt.Sprintf("比均匀用量留有 %.0f%%", -pi.AheadPct)
			}
		}
		if delta != "" {
			ui.Text(c, delta).SingleLine().FontSize(10.5).TextColor(dcol)
		}
		ui.Box(c).Grow(1)
		right := "可撑到重置"
		if hasPace && !pi.LastsUntilReset {
			right = "按这个速度 " + usageDeplete(pi.RunsOutIn) + "用完"
		}
		ui.Text(c, right).SingleLine().FontSize(10.5).TextColor(ink3)
	})
}

// --- local usage ---------------------------------------------------------

// localSection is 本机用量: the header with the range segments, then the
// KPI row, stacked chart and legend — or the scan/progress card.
func (f *usageFeature) localSection(c *ui.Context) {
	ui.Row(c.Key("uh")).Height(22).AlignItems(ui.Center).Margin(6, 0, 0, 0).Padding(0, 2, 0, 2).Children(func() {
		ui.Text(c, "本机用量").SingleLine().FontSize(12).FontWeight(600).TextColor(ink2)
		ui.Box(c).Grow(1)
		f.usageSeg(c)
	})
	if !f.hasSummary || f.summary.Range != f.rangeSel {
		f.scanCard(c)
		return
	}
	s := f.shownSummary()
	f.kpiRow(c, s)
	f.usageChart(c, s)
	f.usageLegend(c, s)
}

// shownSummary is the summary as it must draw right now: KPI totals, the
// chart and the legend all follow the live aiUsage.local* chips, so a chip
// flip repaints the numbers at once instead of waiting for the next scan.
// With every source on (the default) it returns the published summary
// unchanged; otherwise it rebuilds Total from the kept per-tool totals
// and drops the dropped tools' tokens from each bucket.
func (f *usageFeature) shownSummary() aiusage.Summary {
	s := f.summary
	var kept []aiusage.Tool
	for _, t := range localTools() {
		if f.localSourceOn(t) {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(localTools()) {
		return s
	}
	s.ByTool = make(map[aiusage.Tool]aiusage.Totals, len(kept))
	s.Total = aiusage.Totals{}
	for _, t := range kept {
		bt := f.summary.ByTool[t]
		s.ByTool[t] = bt
		s.Total.Input += bt.Input
		s.Total.Output += bt.Output
		s.Total.CacheRead += bt.CacheRead
		s.Total.CacheWrite += bt.CacheWrite
		s.Total.Requests += bt.Requests
		s.Total.Unpriced += bt.Unpriced
		s.Total.Cost += bt.Cost
	}
	s.Buckets = make([]aiusage.Bucket, len(f.summary.Buckets))
	for i, b := range f.summary.Buckets {
		tokens := make(map[aiusage.Tool]int64, len(kept))
		for _, t := range kept {
			if v := b.Tokens[t]; v > 0 {
				tokens[t] = v
			}
		}
		s.Buckets[i] = aiusage.Bucket{Start: b.Start, Tokens: tokens}
	}
	return s
}

// usageSeg is the 今天/7 天/30 天/全部 picker: a fill1 track of small
// buttons; the active segment gets the fill2 capsule and ink text.
func (f *usageFeature) usageSeg(c *ui.Context) {
	labels := []string{"今天", "7 天", "30 天", "全部"}
	ranges := []aiusage.Range{aiusage.RangeToday, aiusage.Range7d, aiusage.Range30d, aiusage.RangeAll}
	ui.Row(c.Key("seg")).Background(fill1).Radius(8).Padding(2, 2, 2, 2).Gap(2).Children(func() {
		for i, s := range labels {
			r := ranges[i]
			b := ui.ButtonBase(c.Key("seg-"+s)).Radius(6).Padding(2, 9, 2, 9).Label(s)
			on := f.rangeSel == r
			if on {
				b.Background(fill2)
			}
			b.Children(func() {
				tc := ink3
				if on {
					tc = ink
				}
				ui.Text(c, s).SingleLine().FontSize(11).FontWeight(600).TextColor(tc)
			})
			if b.Clicked() && f.rangeSel != r {
				f.rangeSel = r
				if f.localOn() && !f.localBusy {
					f.startLocal(f.deps.now())
				}
				f.invalidate()
			}
		}
	})
}

// scanCard fills the local section while there is no summary to show: the
// first scan's progress, the pre-scan 「正在统计…」, or a read error.
func (f *usageFeature) scanCard(c *ui.Context) {
	ui.Column(c.Key("scan")).Height(64).Margin(8, 0, 0, 0).Radius(14).Background(fill1).
		AlignItems(ui.Center).Justify(ui.Center).Gap(6).Padding(0, 12, 0, 12).Children(func() {
		switch {
		case f.localErr != nil && !f.localBusy:
			ui.Text(c, "本机记录读取失败，稍后再试").SingleLine().FontSize(12).TextColor(ink2)
		case f.localBusy:
			done, total := f.scanDone.Load(), f.scanTotal.Load()
			pct := ""
			var frac float32
			if total > 0 {
				n := min(100, done*100/total)
				pct = fmt.Sprintf(" %d%%", n)
				frac = float32(n) / 100
			}
			ui.Text(c, "第一次统计本机记录…"+pct).SingleLine().FontSize(12).TextColor(ink2)
			ui.Box(c.Key("sbar")).Width(220).Height(3).Radius(2).Background(fill3).Draw(func(p *ui.Painter, r ui.Rect) {
				if frac > 0 {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, progress, 2)
				}
			})
			ui.Text(c, f.scanSources()).SingleLine().FontSize(11).TextColor(ink3)
		default:
			ui.Text(c, "正在统计本机用量…").SingleLine().FontSize(12).TextColor(ink2)
		}
	})
}

// scanSources names the enabled local sources in the scan card's caption.
func (f *usageFeature) scanSources() string {
	var names []string
	for _, t := range localTools() {
		if f.localSourceOn(t) {
			names = append(names, toolName(t))
		}
	}
	if len(names) == 0 {
		return "没有选中的本机来源"
	}
	return "只在这台电脑上读取 " + strings.Join(names, "、") + " 的会话记录"
}

// kpiRow is the four metric cells: Token (with the in/out/cache split),
// requests, cost and cache-hit rate, split by hairlines.
func (f *usageFeature) kpiRow(c *ui.Context, s aiusage.Summary) {
	tot := s.Total
	tools := 0
	for _, bt := range s.ByTool {
		if bt.Requests > 0 {
			tools++
		}
	}
	ui.Row(c.Key("kpi")).FillWidth().Margin(8, 0, 0, 0).Children(func() {
		f.kpi(c, "Token", usageTokens(totalTokens(tot)),
			fmt.Sprintf("输入%s·输出%s·缓存%s", usageTokens(tot.Input), usageTokens(tot.Output), usageTokens(tot.CacheRead+tot.CacheWrite)),
			false, 1.6)
		f.kpiSep(c, "1")
		f.kpi(c, "请求", fmt.Sprintf("%d", tot.Requests), fmt.Sprintf("%d 个工具", tools), false, 1)
		f.kpiSep(c, "2")
		f.kpi(c, "费用", usageCost(tot), usageCostSub(tot), true, 1)
		f.kpiSep(c, "3")
		f.kpi(c, "缓存命中", usageHitRate(tot), "占输入", false, 1)
	})
}

// kpi is one metric cell: label, value, one muted sub-line. money turns
// the value green only while a real price shows.
func (f *usageFeature) kpi(c *ui.Context, k, v, sub string, money bool, grow float32) {
	ui.Column(c.Key("kpi-"+k)).Grow(grow).MinWidth(0).Padding(0, 10, 0, 10).Children(func() {
		ui.Text(c, k).SingleLine().FontSize(11).FontWeight(600).TextColor(ink3)
		col := ink
		if money && v != "—" {
			col = usageWeekly
		}
		ui.Text(c, v).SingleLine().FontSize(17).FontWeight(600).TextColor(col).Margin(2, 0, 0, 0)
		ui.Text(c, sub).SingleLine().FontSize(10).TextColor(ink3).Margin(2, 0, 0, 0)
	})
}

func (f *usageFeature) kpiSep(c *ui.Context, key string) {
	ui.Box(c.Key("sep-" + key)).Width(0.5).FillHeight().Background(usageKpiSep)
}

// usageChart is the stacked bar chart: per bucket a column of per-tool
// segments (omp bottom, Codex middle, Claude Code top, as the mock's
// legend orders them), gridlines at half and top, the baseline, and the
// x labels under it.
func (f *usageFeature) usageChart(c *ui.Context, s aiusage.Summary) {
	buckets := s.Buckets
	var maxTokens int64 = 1
	for _, b := range buckets {
		var v int64
		for _, t := range localTools() {
			v += b.Tokens[t]
		}
		if v > maxTokens {
			maxTokens = v
		}
	}
	ui.Box(c.Key("chart")).FillWidth().Height(64).Margin(10, 0, 0, 0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+0.25, r.X+r.W, r.Y+0.25, 0.5, usageGrid)
		p.Line(r.X, r.Y+r.H/2, r.X+r.W, r.Y+r.H/2, 0.5, usageGrid)
		p.Line(r.X, r.Y+r.H-0.25, r.X+r.W, r.Y+r.H-0.25, 0.5, usageBase)
		n := len(buckets)
		if n == 0 {
			return
		}
		slot := r.W / float32(n)
		gap := float32(3)
		if n > 40 {
			gap = 1
		}
		w := max(1, slot-gap)
		for i, b := range buckets {
			var tot int64
			for _, t := range localTools() {
				tot += b.Tokens[t]
			}
			if tot == 0 {
				continue
			}
			x0 := r.X + float32(i)*slot + (slot-w)/2
			y := r.Y + r.H
			topDone := false
			// Stacked bottom-up in localTools order: omp, Codex, Claude Code.
			for j := len(localTools()) - 1; j >= 0; j-- {
				t := localTools()[j]
				v := b.Tokens[t]
				if v <= 0 {
					continue
				}
				sh := max(0, float32(v)/float32(maxTokens)*(r.H-1))
				col := toolColor(t)
				if !topDone {
					var path ui.Path
					topRoundedRect(&path, x0, y-sh, w, sh, 2)
					p.FillPath(&path, col)
					topDone = true
				} else {
					p.Fill(ui.Rect{X: x0, Y: y - sh, W: w, H: sh}, col, 0)
				}
				y -= sh
			}
		}
	})
	ui.Row(c.Key("xl")).Justify(ui.SpaceBetween).Margin(3, 0, 0, 0).Padding(0, 2, 0, 2).Children(func() {
		for _, s := range f.xLabels(buckets) {
			ui.Text(c, s).SingleLine().FontSize(10).TextColor(ink3)
		}
	})
}

// topRoundedRect draws a bar-top path: a rectangle with only its top
// corners rounded.
func topRoundedRect(path *ui.Path, x, y, w, h, r float32) {
	r = min(r, min(w, h)/2)
	path.MoveTo(x, y+h)
	path.LineTo(x, y+r)
	path.QuadTo(x, y, x+r, y)
	path.LineTo(x+w-r, y)
	path.QuadTo(x+w, y, x+w, y+r)
	path.LineTo(x+w, y+h)
	path.Close()
}

// xLabels is the axis under the chart: clock marks for today, dates for
// the other ranges.
func (f *usageFeature) xLabels(buckets []aiusage.Bucket) []string {
	if f.rangeSel == aiusage.RangeToday {
		return []string{"00:00", "06:00", "12:00", "18:00", "24:00"}
	}
	labels := []string{"", "", "", "", ""}
	if len(buckets) == 0 {
		return labels
	}
	for i, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		b := buckets[min(len(buckets)-1, int(frac*float64(len(buckets)-1)+0.5))]
		labels[i] = fmt.Sprintf("%d/%d", int(b.Start.Month()), b.Start.Day())
	}
	return labels
}

// usageLegend is the chart's footnote: one chip per enabled local source
// with its total (and omp's priced cost).
func (f *usageFeature) usageLegend(c *ui.Context, s aiusage.Summary) {
	ui.Row(c.Key("legend")).Gap(14).Margin(8, 0, 0, 0).Padding(0, 2, 0, 2).Children(func() {
		for _, t := range []aiusage.Tool{aiusage.ToolOmp, aiusage.ToolClaude, aiusage.ToolCodex} {
			if !f.localSourceOn(t) {
				continue
			}
			bt := s.ByTool[t]
			ui.Row(c.Key("lg-" + string(t))).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(7, 7).Radius(2).Background(toolColor(t)).Margin(0, 5, 0, 0)
				ui.Text(c, toolName(t)).SingleLine().FontSize(11).TextColor(ink2)
				ui.Text(c, usageTokens(totalTokens(bt))).SingleLine().FontSize(11).FontWeight(600).TextColor(ink).Margin(0, 0, 0, 4)
				if t == aiusage.ToolOmp && bt.Cost > 0 {
					ui.Text(c, fmt.Sprintf("$%.2f", bt.Cost)).SingleLine().FontSize(11).TextColor(ink3).Margin(0, 0, 0, 6)
				}
			})
		}
	})
}

// --- formatting ------------------------------------------------------------

// totalTokens is a range total's full token count (input + output + both
// cache kinds), matching Bucket.Tokens' definition.
func totalTokens(t aiusage.Totals) int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// usageTokens compacts a token count with at most 3 significant digits:
// one decimal under 100 of the unit (4.7M / 39.3K), none at ≥100 (291M /
// 117K) — real-scale KPI lines must stay short enough to fit their cell.
func usageTokens(n int64) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return compact(float64(n)/1e3, "K")
	case n < 1_000_000_000:
		return compact(float64(n)/1e6, "M")
	default:
		return compact(float64(n)/1e9, "B")
	}
}

// compact renders v with one decimal when v < 100 (dropping a trailing
// .0), none at ≥100: 2.5M and 4.7M keep the digit, 164M and 291M don't.
func compact(v float64, unit string) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f%s", v, unit)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0") + unit
}

// usageCost is the KPI's money line: ≈$42.96, + appended when some records
// had no price; — when nothing was priced at all.
func usageCost(t aiusage.Totals) string {
	if t.Cost <= 0 {
		return "—"
	}
	s := fmt.Sprintf("≈$%.2f", t.Cost)
	if t.Unpriced > 0 {
		s += "+"
	}
	return s
}

// usageCostSub explains the money line: only the tools' own prices count,
// and how many records carried none.
func usageCostSub(t aiusage.Totals) string {
	if t.Unpriced > 0 {
		return fmt.Sprintf("%d 条没有价格", t.Unpriced)
	}
	return "只算自带价格"
}

// usageHitRate is the cache-read share of everything read (input + cache).
func usageHitRate(t aiusage.Totals) string {
	den := t.Input + t.CacheRead
	if den <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(t.CacheRead)/float64(den)*100)
}

// usageAgo renders 「N 前」 for the update stamp and stale-data line:
// 刚刚 / N 分钟前 / N 小时前 / N 天前 (the caller appends 更新 or 的数据).
func usageAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	default:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
}

// usageReset is the card's reset time: 「4:25 后重置」 under a day, else
// 「周六 02:00 重置」.
func usageReset(at, now time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "即将重置"
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d:%02d 后重置", int(d.Hours()), int(d.Minutes())%60)
	}
	local := at.Local()
	return fmt.Sprintf("%s %02d:%02d 重置", weekdayCN(local.Weekday()), local.Hour(), local.Minute())
}

// usageResetLong is the popover's reset time: 「3 小时 13 分后重置」 under
// a day (minutes under an hour), else 「4 天 18 小时后重置」.
func usageResetLong(at, now time.Time) string {
	d := at.Sub(now)
	if d <= 0 {
		return "即将重置"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%d 天 %d 小时后重置", int(d.Hours()/24), int(d.Hours())%24)
	}
	if d >= time.Hour {
		return fmt.Sprintf("%d 小时 %d 分后重置", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d 分后重置", max(1, int(d.Minutes())))
}

// usageDeplete formats the depletion estimate: 「1:42 后」 under a day,
// else 「3 天 2 小时后」.
func usageDeplete(d time.Duration) string {
	if d <= 0 {
		return "马上"
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%d 天 %d 小时后", int(d.Hours()/24), int(d.Hours())%24)
	}
	return fmt.Sprintf("%d:%02d 后", int(d.Hours()), int(d.Minutes())%60)
}

// weekdayCN maps a weekday to the mock's Chinese short name.
func weekdayCN(d time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[d]
}

// --- settings ------------------------------------------------------------

// setAIUsage saves an aiUsage mutation through applySettings (the single
// path), like setNowPlaying; the page reads the settings every frame and
// tick, so a change applies without a restart.
func (a *shelf) setAIUsage(mutate func(*settings.Settings)) {
	wasOn := a.settings.AIUsage.Enabled
	next := a.settings
	mutate(&next)
	if !a.applySettings(next) {
		return
	}
	// Switching the feature off retires its in-flight work right away; the
	// next monitor tick must not find quota requests or scans still running.
	if wasOn && !next.AIUsage.Enabled && a.usage != nil {
		a.usage.stopWork()
	}
}
