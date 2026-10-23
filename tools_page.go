//go:build darwin

package main

// The 工具 page: the card grid the design's §1/§2 spec. Cards come from
// layout.json (internal/layout); drawing and per-card behaviour live in
// tools_cards.go. This file owns the page's four-column packing, the
// long-press / drag-to-reorder gestures, the editing chrome (⊖ badge,
// span handle, wobble, the + tile), the card catalogue picker, and the
// module-bar entries (pencil, 完成).

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
)

const (
	pageTools pageID = "tools" // 工具：用户排版的卡片页

	toolCols         = 4                             // 4 列网格
	toolCellH        = 84                            // 行高
	toolCellGap      = 6                             // 卡片间距
	toolMaxRows      = 3                             // 最多 3 行可见，再多在舞台内滚动
	toolStageW       = panelWidth - 16 - 2*panelPadX // 舞台内容宽（panel.go 的 560-16-2·16）
	toolPickRowH     = 30                            // 目录行高（design §3 .pi 30px）
	toolLongPress    = 500 * time.Millisecond        // 长按进入编辑
	toolDragSlop     = 6                             // 普通态拖动超过此距离作废点击
	toolJiggleSpan   = 300 * time.Millisecond        // 无可执行项时的轻摇
	toolSettleAnim   = 240 * time.Millisecond        // 松手吸附
	toolWobblePeriod = 520 * time.Millisecond        // 编辑摆动周期
	toolWobbleDeg    = 0.7                           // 编辑摆动 ±0.7°
)

// toolCell is one card's position in content coordinates.
type toolCell struct {
	id      string
	x, y, w float32
	plus    bool // the dashed + slot (edit mode only)
}

// toolPress is the pointer session on one card: from the down, through a
// possible long-press or reorder drag, to the settle spring after the
// release. All coordinates are stage content space.
type toolPress struct {
	id         string
	downAt     time.Time
	downX      float32 // pointer position at the down (content space)
	downY      float32
	px, py     float32 // latest pointer position
	grabDX     float32 // pointer offset inside the card at the down
	grabDY     float32
	cardX      float32 // the card element's rendered left/top this frame
	cardY      float32 //   (the animation's answer, so moves stay aligned)
	origCell   toolCell
	dragging   bool // slop passed: the lifted copy follows the pointer
	suppress   bool // moved without edit: the release is not a click
	settling   bool // released: flying to the committed cell or back
	settleJump bool // the settle channels were Jump'ed to the start
	settleX    float32
	settleY    float32
	index      int // current insertion index while dragging
}

// toolsPage is the card grid page.
type toolsPage struct {
	host   *shelf
	scroll ui.ScrollState

	editing   bool
	picking   bool
	editAt    time.Time // wobble phase origin
	press     *toolPress
	skipClick bool   // the last release was a suppressed press, not a tap
	jiggleID  string // card shaking for 无可执行项
	jiggleAt  time.Time
}

func (p *toolsPage) id() pageID    { return pageTools }
func (p *toolsPage) label() string { return "工具" }
func (p *toolsPage) icon(c *ui.Context, _ bool) ui.Element {
	return ui.Icon(c, toolsNavSymbol)
}
func (p *toolsPage) enabled() bool      { return true }
func (p *toolsPage) sharesHeight() bool { return false }

// toolsPageOf is the shelf's tools page, or nil for test shelves that
// never registered one.
func (a *shelf) toolsPage() *toolsPage {
	if p, ok := a.page(pageTools).(*toolsPage); ok {
		return p
	}
	return nil
}

// toolsLayout is the persisted arrangement. It loads lazily from
// layout.json, defaults first so the page never waits on the file.
func (a *shelf) toolsLayout() layout.Layout {
	if a.toolsLay == nil {
		l := layout.Default()
		if a.dataDir != "" {
			got, err := layout.Load(a.dataDir)
			if err != nil {
				// Like settingsBroken: the page still draws, but the file
				// the user could repair stays untouched until restart.
				a.toolsLayBroken = true
				a.report(err)
			}
			l = got
		}
		a.toolsLay = &l
	}
	return *a.toolsLay
}

// mutateTools applies fn to a copy of the layout and saves it, reporting
// whether the change took. The in-memory copy is replaced only after the
// write succeeds, so a failed Write leaves both the file and the shown
// arrangement as they were — and the caller learns the card never moved;
// a load that failed (toolsLayBroken) latches writes off entirely — the
// page mutates for the session, the recoverable file stays put, which
// still counts as taken.
func (a *shelf) mutateTools(fn func(*layout.Layout)) bool {
	l := a.toolsLayout()
	l.Cards = slices.Clone(l.Cards)
	l.Pins = slices.Clone(l.Pins)
	fn(&l)
	if a.dataDir != "" && !a.toolsLayBroken {
		if err := layout.Write(a.dataDir, l); err != nil {
			a.report(fmt.Errorf("tools layout: %w", err))
			return false // keep the pre-change layout and the file as they were
		}
	}
	a.toolsLay = &l
	if a.win != nil {
		a.win.Invalidate()
	}
	return true
}

// resetToolsEdit ends editing and the picker; called on hide and on
// leaving the page so re-entry starts clean.
func (a *shelf) resetToolsEdit() {
	if p := a.toolsPage(); p != nil {
		p.editing, p.picking, p.press = false, false, nil
		p.jiggleID = ""
		if a.win != nil {
			a.win.Invalidate()
		}
	}
}

func (p *toolsPage) moduleTitle() (title, count string) {
	if p.picking {
		return "卡片目录", ""
	}
	return "工具", fmt.Sprintf("%d 项", len(p.host.toolsLayout().Cards))
}

func (p *toolsPage) moduleTrailing(c *ui.Context, live bool) []ui.Element {
	a := p.host
	switch {
	case p.picking:
		return []ui.Element{a.toolDoneButton(c, live, "pick-done")}
	case p.editing:
		return []ui.Element{a.toolDoneButton(c, live, "edit-done")}
	default:
		b := a.iconButton(c, "edit", pencilSymbol, "编辑")
		if live && b.Clicked() {
			p.editing, p.editAt = true, time.Now()
		}
		return []ui.Element{b}
	}
}

// toolDoneButton is the 完成 pill at the module bar's right.
func (a *shelf) toolDoneButton(c *ui.Context, live bool, key string) ui.Element {
	b := ui.Row(c.Key(key)).Height(24).Padding(0, 12, 0, 12).Radius(12).
		Background(accent).Center().Label("完成")
	if live && b.Clicked() {
		if p := a.toolsPage(); p != nil {
			if p.picking {
				p.picking = false
			} else {
				p.editing, p.press = false, nil
			}
		}
	}
	b.Children(func() {
		ui.Text(c, "完成").SingleLine().FontSize(12).FontWeight(600).TextColor(ink)
	})
	return b
}

// stageH is what the grid needs: up to three rows of cards, or the
// picker's three tiles.
func (p *toolsPage) stageH() int {
	a := p.host
	if p.picking {
		return p.pickerH()
	}
	l := a.toolsLayout()
	editing := p.editing
	cells := packToolCards(l.Cards, editing)
	if d := p.press; d != nil && editing && (d.dragging || d.settling) {
		if ins := toolInsertIndex(l.Cards, d.id, d.index, d.px, d.py); ins >= 0 {
			cells = packToolCards(toolVirtualCards(l.Cards, d.id, ins), editing)
		}
	}
	rows := 0
	for _, cell := range cells {
		rows = max(rows, int((cell.y+toolCellH)/toolCellH))
	}
	if rows == 0 {
		rows = 1 // keep a one-cell stage so editing a pin-everything grid still shows +
	}
	rows = min(rows, toolMaxRows)
	return rows*toolCellH + (rows-1)*toolCellGap
}

// pickerH is the catalogue's stage height: padding, one h4 per group
// that has entries, and 30pt rows two per line.
func (p *toolsPage) pickerH() int {
	groups := p.pickerGroups()
	h := 10 + 12 // picker padding top+bottom (design .picker 10px/12px)
	for _, g := range groups {
		h += 8 + 18 // h4 margin+text
		rows := (len(g) + 1) / 2
		h += rows*toolPickRowH + max(0, rows-1)*4 + 8 // rows, 4px gaps, group gap
	}
	return h - 8 // last group has no trailing gap
}

// pickerGroups is the catalogue split into 动作 and 小组件 rows in
// picker order.
func (p *toolsPage) pickerGroups() (groups [][]toolCardDef) {
	var actions, widgets []toolCardDef
	for _, def := range toolCatalog() {
		if !def.available(p.host) {
			continue
		}
		if def.kind == cardAction {
			actions = append(actions, def)
		} else {
			widgets = append(widgets, def)
		}
	}
	if len(actions) > 0 {
		groups = append(groups, actions)
	}
	if len(widgets) > 0 {
		groups = append(groups, widgets)
	}
	return
}

// body is the stage: the picker's tile list while picking, else the card
// grid. The + slot only exists while editing.
func (p *toolsPage) body(c *ui.Context, _ bool, live bool) ui.Element {
	a := p.host
	if p.picking {
		return p.pickerBody(c, live)
	}
	l := a.toolsLayout()
	editing := p.editing
	d := p.press
	// The dragged card's slot is virtual: the list it would have if the
	// pointer released now.
	cards := l.Cards
	if d != nil && editing && (d.dragging || d.settling) {
		if ins := toolInsertIndex(l.Cards, d.id, d.index, d.px, d.py); ins >= 0 {
			d.index = ins
			cards = toolVirtualCards(l.Cards, d.id, ins)
		}
	}
	cells := packToolCards(cards, editing)
	contentH := float32(0)
	for _, cell := range cells {
		contentH = max(contentH, cell.y+toolCellH+toolCellGap)
	}
	return ui.Column(c.Key("tools-stage")).Grow(1).MinHeight(0).Children(func() {
		scroll := ui.Scroll(c.Key("cards")).Grow(1).MinHeight(0).TrackScroll(&p.scroll)
		// Pointer tracking makes the stage interactive and reports the
		// viewport-local pointer for the press bookkeeping.
		vpx, vpy, _ := scroll.PointerPosition()
		if d != nil && !d.settling {
			d.px, d.py = vpx, vpy+p.scroll.Y
		}
		scroll.Children(func() {
			// A box in normal flow carries the grid's real height: an
			// absolute child of the scroll itself counts toward neither
			// the scrollable content nor the offset, so a grid past the
			// third row could neither scroll nor be reached. The cards
			// absolute-position inside the box, which scrolls with them.
			ui.Box(c.Key("cards-stack")).Width(toolStageW).Height(contentH).Children(func() {
				for _, cell := range cells {
					p.toolCell(c, cell, editing, live)
				}
				if d != nil && d.dragging {
					p.toolLiftedCard(c, *d)
				}
			})
		})
		top, bottom, _ := scrollEdges(&p.scroll, contentH)
		a.scrollMelt(c, "tools", top, bottom, 0)
	})
}

// toolCell draws one packed cell: the + tile, a parked card (in-flow,
// possibly mid-reorder), or the placeholder the dragged card left.
func (p *toolsPage) toolCell(c *ui.Context, cell toolCell, editing, live bool) {
	a := p.host
	if cell.plus {
		plus := ui.Column(c.Key("tc-plus")).Absolute().Left(cell.x).Top(cell.y).
			Width(cell.w).Height(toolCellH).Radius(14).Center().
			BorderWidth(1.5).BorderColor(ui.RGBA(255, 255, 255, 0.25)).
			BorderStyle(ui.BorderDashed).Label("添加卡片")
		if live {
			plus.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputPointerUp && p.editing {
					p.picking = true
					return true
				}
				return false
			})
			plus.PointerPosition()
		}
		plus.Children(func() {
			ui.Icon(c, plusSymbol).Size(22, 22).TextColor(ink3)
		})
		return
	}
	def := toolCardDefFor(cell.id)
	spec := layout.Card{ID: cell.id, Span: 1}
	if def != nil {
		spec.Span = def.span
	}
	if i := slices.IndexFunc(a.toolsLayout().Cards, func(cc layout.Card) bool { return cc.ID == cell.id }); i >= 0 {
		spec.Span = a.toolsLayout().Cards[i].Span
	}
	d := p.press
	// The dragged card's own cell: while it is dragged its slot holds the
	// invisible parked copy that keeps the pointer session; while it
	// settles that copy flies visibly from the pointer to the cell.
	parked := d != nil && d.id == cell.id && (d.dragging || d.settling)
	card := a.toolCard(c, spec, surfFor(editing))
	switch {
	case parked && d.settling:
		p.toolSettle(card, d)
		card.Absolute().Width(cell.w).Height(toolCellH)
	case parked:
		// The parked element stays the pointer's press target; its
		// animated slot position converts card-local events to stage
		// coordinates.
		cx := card.AnimateTo("cx", cell.x, ui.Snappy)
		cy := card.AnimateTo("cy", cell.y, ui.Snappy)
		d.cardX, d.cardY = cx, cy
		card.Absolute().Left(cx).Top(cy).Width(cell.w).Height(toolCellH).Opacity(0)
	case editing:
		card.Absolute().Width(cell.w).Height(toolCellH)
		card.Left(card.AnimateTo("cx", cell.x, ui.Snappy)).
			Top(card.AnimateTo("cy", cell.y, ui.Snappy))
		p.toolEditChrome(c, card, cell, live)
	default:
		card.Absolute().Left(cell.x).Top(cell.y).Width(cell.w).Height(toolCellH)
	}
	if live {
		p.toolCardInput(c, card, cell, editing, live)
	}
	if !parked {
		p.toolCardWobble(c, card, cell, editing)
	}
}

// surfFor picks the card surface: glyph-only while editing.
func surfFor(editing bool) toolCardSurface {
	if editing {
		return cardEdit
	}
	return cardPanel
}

// toolLiftedCard draws the dragged copy above the grid at the pointer.
// It takes no input: the parked card keeps the press so release always
// lands on the same element the down did.
func (p *toolsPage) toolLiftedCard(c *ui.Context, d toolPress) {
	a := p.host
	spec := layout.Card{ID: d.id, Span: 1}
	if def := toolCardDefFor(d.id); def != nil {
		spec.Span = def.span
	}
	w := toolColW(spec.Span)
	card := a.toolCard(c, spec, cardEdit, "lift")
	card.Absolute().Left(d.px-d.grabDX).Top(d.py-d.grabDY).
		Width(w).Height(toolCellH).Scale(1.06, 1.06).
		TransformOrigin(0.5, 0.5).PassThrough()
}

// toolCardInput wires the pointer: down starts a press, move past the
// slop reorders in edit or kills the click outside it, up drops or
// lands, and the long-press deadline turns editing on.
func (p *toolsPage) toolCardInput(c *ui.Context, card ui.Element, cell toolCell, editing, live bool) {
	a := p.host
	card.PointerPosition() // interactive: a hit-test and tracking target
	card.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if p.press != nil {
				return false
			}
			d := &toolPress{
				id: cell.id, downAt: ev.Time,
				downX: cell.x + ev.X, downY: cell.y + ev.Y,
				px: cell.x + ev.X, py: cell.y + ev.Y,
				grabDX: ev.X, grabDY: ev.Y,
				cardX: cell.x, cardY: cell.y,
				origCell: cell,
			}
			p.press = d
			c.AnimationFrame() // the long-press deadline needs frames
			return true
		case ui.InputPointerMove:
			d := p.press
			if d == nil || d.id != cell.id || d.settling {
				return d != nil && d.id == cell.id && d.settling
			}
			d.px, d.py = d.cardX+ev.X, d.cardY+ev.Y
			if d.dragging {
				if ins := toolInsertIndex(a.toolsLayout().Cards, d.id, d.index, d.px, d.py); ins >= 0 {
					d.index = ins
				}
				return true
			}
			if hypot(d.px-d.downX, d.py-d.downY) <= toolDragSlop {
				return true
			}
			if p.editing {
				d.dragging = true
				d.index = toolInsertIndex(a.toolsLayout().Cards, d.id, -1, d.px, d.py)
				if d.index < 0 {
					d.index = len(a.toolsLayout().Cards) - 1
				}
			} else {
				d.suppress = true
			}
			return true
		case ui.InputPointerUp:
			d := p.press
			if d == nil || d.id != cell.id {
				return false
			}
			if d.settling {
				return true
			}
			if d.dragging {
				d.px, d.py = d.cardX+ev.X, d.cardY+ev.Y
				p.toolCardDrop(d)
				return true
			}
			p.skipClick = d.suppress // a moved press never becomes a tap
			p.press = nil
			return true
		}
		return false
	})
	// The long-press deadline is checked at build time: a still press asks
	// for every frame until it crosses the slop or editing starts, so the
	// deadline fires on the frame clock even when nothing else animates.
	if d := p.press; d != nil && d.id == cell.id && !d.dragging && !d.settling && !d.suppress && !p.editing {
		c.AnimationFrame()
		if c.Now().Sub(d.downAt) >= toolLongPress {
			p.editing, p.editAt = true, c.Now()
			d.suppress = true // this press only entered edit; no click, no drag yet
		}
	}
	if !editing && live && card.Clicked() {
		if p.skipClick {
			p.skipClick = false
		} else {
			p.toolCardTapped(cell.id)
		}
	}
}

// toolCardTapped runs the card on the tray's selection (design §3:
// 对托盘页当前选中的项目执行), jiggling with the hint when nothing
// applies.
func (p *toolsPage) toolCardTapped(id string) {
	a := p.host
	if p.editing {
		return
	}
	if a.runToolCard(id) {
		return
	}
	p.jiggleID, p.jiggleAt = id, time.Now()
	a.toolCardTapFeedback()
}

// toolCardTapFeedback is shared by tools-page and desktop-card taps.
func (a *shelf) toolCardTapFeedback() {
	if a.conv != nil && a.conv.busy() {
		a.toastStatus("一次只跑一个任务，请稍候", time.Now().Add(2*time.Second), statusInfo)
		return
	}
	a.toastStatus("把文件拖到这里", time.Now().Add(2*time.Second), statusInfo)
}

func hypot(dx, dy float32) float64 {
	return math.Hypot(float64(dx), float64(dy))
}

// toolCardDrop ends a reorder: inside the stage it commits the index,
// outside the panel it pins the card to the desktop (task B, design §2
// 把卡片拖出面板边缘 = 固定到桌面), elsewhere it flies back.
func (p *toolsPage) toolCardDrop(d *toolPress) {
	a := p.host
	// The drop lands inside the stage when the release is within the
	// viewport the scroll shows — the stage's own height. scrollEdges
	// would answer contentH−MaxY, which shrinks as the content grows and
	// misreads releases near the viewport's bottom.
	viewH := float32(a.currentStageH())
	inside := d.px >= 0 && d.px <= toolStageW &&
		d.py >= p.scroll.Y && d.py <= p.scroll.Y+viewH
	target := d.origCell
	if inside {
		if ins := d.index; ins >= 0 {
			a.mutateTools(func(l *layout.Layout) { l.MoveCard(d.id, ins) })
			// The committed cell is where the settle flight aims.
			cells := packToolCards(a.toolsLayout().Cards, true)
			for _, cell := range cells {
				if cell.id == d.id {
					target = cell
				}
			}
		}
	} else {
		// A release beyond the panel's drawn body pins the card at the
		// cursor (grab point preserved so the window lands under it);
		// a release on another pane of the panel still flies back.
		cursor := cursorPoint()
		panel := mygo.Rectangle{X: a.x, Y: a.y, Width: panelWidth, Height: a.panelH()}
		if !insideRect(cursor, panel) {
			w := pinCardW
			// The grid's live span is authoritative — the user may have
			// resized the card after the catalog default was read.
			span := 0
			for _, c := range a.toolsLayout().Cards {
				if c.ID == d.id {
					span = c.Span
					break
				}
			}
			if span == 0 {
				if def := toolCardDefFor(d.id); def != nil {
					span = def.span
				}
			}
			if span == 2 {
				w = pinCardW2
			}
			disp := displayIDAt(cursor, 0)
			area := pinArea(disp)
			edge := pinReleaseEdge(cursor, area)
			frame := mygo.Rectangle{X: cursor.X - int(d.grabDX), Y: cursor.Y - int(d.grabDY), Width: w, Height: pinCardH}
			if edge != pinFree {
				frame = pinStripFrame(edge, area, cursor.Y) // the 24 pt edge rule, same as pin drags
			}
			if a.addPin(d.id, span, disp, edge, false, frame) != nil {
				p.press = nil // the parked cell is gone: no settle back
				return
			}
			// The failed write kept the grid unchanged; fly back.
		}
	}
	d.settling, d.dragging = true, false
	d.settleX, d.settleY = target.x, target.y
}

// toolSettle animates the released card from the pointer to its cell.
// The press clears once the flight rests: Settled, not Arrived — Arrived
// reports only the frame a moving value comes to rest, so a flight that
// starts at its goal (an edit drag released back where it began, index
// unchanged) never moves and never arrives, and the press would stick.
func (p *toolsPage) toolSettle(card ui.Element, d *toolPress) {
	if !d.settleJump {
		card.Jump("cx", d.px-d.grabDX)
		card.Jump("cy", d.py-d.grabDY)
		card.Jump("sc", 1.06)
		d.settleJump = true
	}
	cx := card.AnimateTo("cx", d.settleX, ui.Animation{Spring: ui.SpringOptions{Duration: toolSettleAnim, Bounce: 0.12}})
	cy := card.AnimateTo("cy", d.settleY, ui.Animation{Spring: ui.SpringOptions{Duration: toolSettleAnim, Bounce: 0.12}})
	sc := card.AnimateTo("sc", 1, ui.Animation{Spring: ui.SpringOptions{Duration: toolSettleAnim, Bounce: 0.12}})
	card.Left(cx).Top(cy).Scale(sc, sc).TransformOrigin(0.5, 0.5)
	d.cardX, d.cardY = cx, cy
	if card.Settled("cx") && card.Settled("cy") && card.Settled("sc") {
		p.press = nil
	}
}

// toolEditChrome adds the editing decorations: the ⊖ badge, the span
// handle on variable cards, and the wobble is applied by toolCardWobble.
func (p *toolsPage) toolEditChrome(c *ui.Context, card ui.Element, cell toolCell, live bool) {
	a := p.host
	card.Children(func() {
		// ⊖: 18pt #3A3A3C circle ringed 1.5pt black with a white dash
		// (design §2 .minus), sticking out 6pt at the top-left.
		badge := ui.Row(c.Key("minus")).Absolute().Top(-6).Left(-6).Size(18, 18).Radius(9).
			Background(ui.Hex("#3A3A3C")).Border(1.5, ui.Hex("#000000")).
			Center().Label("移除")
		if live {
			badge.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputPointerUp {
					a.mutateTools(func(l *layout.Layout) { l.RemoveCard(cell.id) })
					return true
				}
				return true // swallow the down too so no card press starts
			})
			badge.PointerPosition()
		}
		badge.Children(func() {
			ui.Box(c).Size(8, 1.6).Radius(1).Background(ink)
		})
		if def := toolCardDefFor(cell.id); def != nil && def.variableSpan {
			cur := 1
			if i := slices.IndexFunc(a.toolsLayout().Cards, func(cc layout.Card) bool { return cc.ID == cell.id }); i >= 0 {
				cur = a.toolsLayout().Cards[i].Span
			}
			next := 2
			if cur == 2 {
				next = 1
			}
			// The span handle is the corner bracket of design §2 .rs:
			// 1.6pt ink2 lines along the card's bottom-right corner. It
			// tucks inside the card (5pt in), tapping it toggles the span.
			handle := ui.Box(c.Key("span")).Absolute().Bottom(5).Right(5).Size(12, 12).
				BorderWidth(0, 1.6, 1.6, 0).BorderColor(ink2).
				Label(fmt.Sprintf("改为 %d 列", next))
			if live {
				handle.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind == ui.InputPointerUp {
						a.mutateTools(func(l *layout.Layout) { l.SetSpan(cell.id, next) })
						return true
					}
					return true
				})
				handle.PointerPosition()
			}
		}
	})
}

// toolCardWobble is the ±0.7° alternating wobble of edit mode, phased by
// cell column so neighbours rock against each other. ReduceMotion and
// headless runs keep the cards still.
func (p *toolsPage) toolCardWobble(c *ui.Context, card ui.Element, cell toolCell, editing bool) {
	if c.Preferences().ReduceMotion {
		return
	}
	if p.jiggleID == cell.id {
		t := float32(time.Since(p.jiggleAt)) / float32(toolJiggleSpan)
		if t >= 1 {
			p.jiggleID = ""
		} else {
			card.Rotate(float32(math.Sin(float64(t*3*math.Pi)) * 3.0 * float64(1-t)))
			card.TransformOrigin(0.5, 0.5)
		}
		return
	}
	if !editing {
		return
	}
	col := int(cell.x) / int(toolColW(1)+toolCellGap)
	phase := float64(time.Since(p.editAt)) / float64(toolWobblePeriod) * 2 * math.Pi
	if col%2 == 1 {
		phase += math.Pi
	}
	card.Rotate(float32(math.Sin(phase) * toolWobbleDeg))
	card.TransformOrigin(0.5, 0.5)
	c.AnimationFrame()
}

// pickerBody is the catalogue (design §3): a #1C1C1E card holding two
// groups — 动作 and 小组件 — each a 2-column list of 30pt rows (icon,
// name, size label). A tap adds the card and returns to the edit grid;
// placed entries are greyed and marked · 已添加.
func (p *toolsPage) pickerBody(c *ui.Context, live bool) ui.Element {
	a := p.host
	l := a.toolsLayout()
	placed := map[string]bool{}
	for _, card := range l.Cards {
		placed[card.ID] = true
	}
	for _, pin := range l.Pins {
		placed[pin.ID] = true
	}
	return ui.Column(c.Key("tools-pick")).Grow(1).MinHeight(0).Children(func() {
		sheet := ui.Column(c.Key("picker")).FillWidth().Radius(14).
			Background(ui.Hex("#1C1C1E")).Padding(10, 12, 12, 12)
		sheet.Children(func() {
			for gi, group := range p.pickerGroups() {
				title := "动作"
				if gi == 1 {
					title = "小组件"
				}
				ui.Text(c, title).SingleLine().FontSize(11).FontWeight(600).
					TextColor(ink3).Margin(2, 0, 6, 0)
				ui.Grid(c.Key("plist-"+title)).ColumnTracks(ui.Fr(1), ui.Fr(1)).
					GapY(4).GapX(10).FillWidth().Children(func() {
					for _, def := range group {
						p.pickRow(c, def, placed[def.id], live)
					}
				})
				if gi == 0 {
					ui.Box(c).Height(8)
				}
			}
		})
	})
}

// pickRow is one catalogue row: 15pt icon, name, right-aligned size
// (1 列 / 2 列 / 1 / 2 列). Placed cards dim to ink3 with · 已添加 and
// cannot be tapped twice; a usable row taps in and returns to editing.
func (p *toolsPage) pickRow(c *ui.Context, def toolCardDef, added, live bool) ui.Element {
	a := p.host
	row := ui.Row(c.Key("pi-"+def.id)).Height(toolPickRowH).Radius(8).
		AlignItems(ui.Center).Gap(8).Padding(0, 8, 0, 8).Label(def.title)
	if !added && live {
		if row.Hovered() {
			row.Background(fill2)
		}
		row.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind == ui.InputPointerUp {
				a.mutateTools(func(l *layout.Layout) { l.AddCard(def.id, def.span) })
				p.picking = false // 加完回到编辑网格（设计稿 §3 说明）
				return true
			}
			return false
		})
		row.PointerPosition()
	}
	row.Children(func() {
		iconCol, textCol := ink, ink
		if added {
			iconCol, textCol = ink3, ink3
		}
		ui.Icon(c, def.icon).Size(15, 15).TextColor(iconCol)
		ui.Text(c, def.title).SingleLine().FontSize(12).FontWeight(500).TextColor(textCol)
		ui.Box(c).Grow(1)
		size := "1 列"
		if def.variableSpan {
			size = "1 / 2 列"
		} else if def.span == 2 {
			size = "2 列"
		}
		if added {
			size += " · 已添加"
		}
		ui.Text(c, size).SingleLine().FontSize(10).TextColor(ink3)
	})
	return row
}
