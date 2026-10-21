//go:build darwin

package main

// Completion feedback: the panel's bottom toast only exists while the panel
// is open, so anything that finishes with the panel closed grows a capsule
// out of the hardware notch instead — a 16pt status dot and the toast's own
// words inside the same black shell. The capsule is a peek state of the
// resident window, not a window of its own: the shell, the spring and the
// pass-through rules are the ones the hover pill already uses.
//
// Status colours are the one exception to the panel's single accent: they
// appear only inside the capsule's black shell — the dot, never a border
// or a glow past its edge — and nowhere else.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// Status colours (DESIGN.md's palette, used only by the capsule).
var (
	fbGreen  = ui.Hex("#30D158")
	fbOrange = ui.Hex("#FF9F0A")
	fbRed    = danger // #FF453A
)

const (
	fbHoldOK  = 1600 * time.Millisecond // a success stays this long after growing
	fbHoldErr = 2200 * time.Millisecond // a failure the same, after its shake
	fbShake   = 420 * time.Millisecond  // one decaying left-right shake
	fbShakeX  = 6                       // its first swing, in points
	fbBreathe = 1600 * time.Millisecond // one breath of a running job's status dot
	fbMaxW    = 380                     // the capsule ellipsizes past this width
	fbDot     = 16                      // the status dot
	fbGap     = 7                       // dot to text
	fbPadX    = 12                      // text to capsule edge
	fbBody    = 22                      // capsule height below the notch band
	fbExpand  = 380 * time.Millisecond  // the grow-in the hold is measured from
)

// The shell's springs. The panel opens with a small overshoot (Bounce
// 0.18) — the island's settle — while the completion capsule keeps the
// livelier Bounce 0.28 it arrived with; collapsing is critically damped
// and crisp; the hover shapes morph with a light bounce of their own.
var (
	panelOpenSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond, Bounce: 0.18}}
	contentSpring   = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}, Delay: 30 * time.Millisecond}
	openSpring      = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond, Bounce: 0.28}}
	closeSpring     = ui.Animation{Spring: ui.SpringOptions{Duration: 240 * time.Millisecond, Bounce: 0}}
	peekSpring      = ui.Animation{Spring: ui.SpringOptions{Duration: peekAnimation, Bounce: 0.15}}
	retractSpring   = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}}
	shakeSpring     = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}}
)

// openOvershoot is the room the frame keeps for a spring that is still on
// its way: the widest spring it must cover is the capsule's Bounce 0.28,
// which peaks near 4%, plus a frame's worth of catch-up. The panel itself
// opens on Bounce 0.18, which peaks near 1.1% (panelOpenOvershoot).
const (
	openOvershoot      = 0.045
	panelOpenOvershoot = 0.015
)

// peekKind is which content the peek shell is showing: the chevron or the
// 松开放入 pill, the hover player, or the completion capsule. Recorded
// each revealed frame so a fold can keep drawing that same content while
// it shrinks away.
type peekKind int

const (
	peekChevron peekKind = iota
	peekDrop
	peekPlayer
	peekCapsule
)

// shellMotion is what view() drew last: the drawn shell's size, for the
// window frame to hold while the springs are in flight, and whether a
// spring is still on its way (the frame keeps overshoot room until then).
type shellMotion struct {
	w, h, peekW, peekH float32
	openK              float32 // the drawn open interpolation: 0 = folded to the peek shape
	peekK              float32 // the drawn peek interpolation, for the close clamp
	peekKind           peekKind
	openMoving         bool // the opening spring has not come to rest
	sizeMoving         bool // a peek-shape morph has not come to rest
	peek               bool // w and h are the peek shape's, not the panel's
}

// feedback is the capsule's state: what it says, for how long, and how wide
// the last frame measured that text.
type feedback struct {
	text       string
	kind       statusKind
	live       bool // the words are a running job's progress, refreshed per frame
	shownAt    time.Time
	until      time.Time
	hold       time.Duration // visible span retained until the first drawn frame
	pillW      float32
	drawn      bool        // drawn in the last frame (the window's command-line tests read it)
	retracting bool        // the shell is folding back from the capsule
	armed      bool        // its clock (hold, shake) has started on the first drawn frame
	pausedAt   time.Time   // set while hover or a drag holds the peek shape: the hold rests
	job        *convertJob // the job whose progress the capsule shows
}

// up reports whether the capsule has something to say now.
func (f *feedback) up(now time.Time) bool {
	if f.text == "" {
		return false
	}
	return (!f.armed && f.hold > 0) || !f.pausedAt.IsZero() || f.until.IsZero() || now.Before(f.until)
}

// color is the status colour of what it says.
func (f *feedback) color() ui.Color {
	switch {
	case f.live:
		return fbOrange
	case f.kind == statusErr:
		return fbRed
	case f.kind == statusInfo:
		return ink2
	}
	return fbGreen
}

// glyph is the sign inside the dot. A running job has no verdict yet: the
// dot holds an ellipsis, as the design's waiting state does. A plain
// information dot carries no sign.
func (f *feedback) glyph() string {
	switch {
	case f.live:
		return "…"
	case f.kind == statusErr:
		return "!"
	case f.kind == statusInfo:
		return ""
	}
	return "✓"
}

// holdFloor is the shortest hold the capsule gives a toast after growing
// out, so even a deadline-free or short-deadline status stays readable.
func (k statusKind) holdFloor() time.Duration {
	if k == statusErr {
		return fbHoldErr
	}
	return fbHoldOK
}

// fbErrMinHold is the floor for a deadline-free error (report()): long
// enough to read and to finish the shake.
const fbErrMinHold = 4 * time.Second

// show turns a toast into a capsule. The caller's deadline wins: the
// capsule only floors it so the grow-out plus a readable hold always fit
// (and a deadline-free error stays at least fbErrMinHold). The hold is not
// counted from here: the panel may still be folding away, so the capsule
// arms its own clock the first frame it actually draws (see arm).
func (f *feedback) show(text string, kind statusKind, until time.Time, now time.Time) {
	// The capsule is one glanceable line: a multi-line toast (stacked
	// notices) shows its first line, never a mid-word cut.
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if f.text != text {
		f.pillW = 0
	}
	f.text, f.kind, f.live = text, kind, false
	f.shownAt = now
	f.hold = max(until.Sub(now), fbExpand+kind.holdFloor())
	if until.IsZero() && kind == statusErr {
		f.hold = max(f.hold, fbErrMinHold)
	}
	f.until = now.Add(f.hold)
	f.armed = false
	f.pausedAt = time.Time{}
}

// arm starts the capsule's own clock when its first frame draws — the hold
// and the shake are measured from the grow-out the user can see, not from
// the post, which can precede the end of the panel's close or a long hover.
// The caller's deadline still wins when it reaches further.
func (f *feedback) arm(now time.Time) {
	if f.armed || f.live {
		return
	}
	f.shownAt, f.until = now, now.Add(f.hold)
	f.armed = true
}

// follow points the capsule at the running conversion job, so a panel that
// is closed while a job runs keeps reporting it. A toast posted meanwhile
// keeps the capsule for its whole span — a new job never stomps a verdict
// the user has not seen; the progress returns after it.
func (f *feedback) follow(a *shelf, now time.Time) {
	var job *convertJob
	if a.conv != nil {
		job = a.conv.job
	}
	live := job != nil && a.conv.live()
	if !live {
		if f.job != nil {
			f.job = nil
			if f.live {
				f.live, f.text = false, ""
			}
		}
		return
	}
	if !f.live && f.up(now) {
		return // a posted toast holds the capsule for its whole span
	}
	text, ok := convertProgress(job)
	if !ok {
		return
	}
	if f.live && f.job == job && f.text == text {
		return // the step is unchanged: keep the measured width, redraw nothing
	}
	// Re-entering after a transient toast has expired keeps the same
	// job but must drop the toast's stale deadline — live has none.
	f.until = time.Time{}
	if f.job != job {
		f.shownAt = now
	}
	f.job = job
	if f.text != text {
		f.pillW = 0
	}
	f.text, f.kind, f.live = text, statusOK, true
	f.pausedAt = time.Time{}
}

// capsule is the shape the notch grows into: as wide as the text needs, but
// never narrower than the notch plus 60, and no wider than fbMaxW.
func (f *feedback) capsule(baseW, baseH float32) (w, h float32) {
	text := f.pillW
	if text == 0 {
		text = measureFallback(f.text)
	}
	w = text + fbDot + fbGap + 2*fbPadX
	w = min(max(w, baseW+60), fbMaxW)
	return w, baseH + fbBody
}

// measure is the width the text needs at the capsule's 12pt semibold.
func (f *feedback) measure(c *ui.Context) float32 {
	if f.text == "" {
		return 0
	}
	w, _ := c.MeasureText(0, ui.Span{Text: f.text, Size: 12, Weight: 600})
	return w
}

// measureFallback keeps the capsule sane before any frame has measured the
// text, and in tests that build a shelf without a view.
func measureFallback(text string) float32 {
	var w float32
	for _, r := range text {
		switch {
		case r > 0x2E80: // CJK and full-width punctuation
			w += 12
		default:
			w += 6.5
		}
	}
	return w
}

// shake is the capsule's horizontal offset now: one decaying left-right
// pass, left then right, that starts once it has grown out.
func (f *feedback) shake(now time.Time, reduceMotion bool) float32 {
	if f.kind != statusErr || f.live || reduceMotion {
		return 0
	}
	d := now.Sub(f.shownAt.Add(fbExpand))
	if d <= 0 || d >= fbShake {
		return 0
	}
	t := float32(d) / float32(fbShake)
	// One cycle, left first, its second swing at about half height —
	// the design's decaying keyframes in one pass.
	return -fbShakeX * (1 - 0.6*t) * float32(math.Sin(2*math.Pi*float64(t)))
}

// accent is the strength of the status dot's colour inside the shell: a
// steady 0.9 for a verdict, a slow breath (0.3–0.9) for a job that is
// still running. Plain information carries no accent: the neutral dot
// speaks for itself.
func (f *feedback) accent(now time.Time, reduceMotion bool) float32 {
	if !f.live && f.kind == statusInfo {
		return 0
	}
	if !f.live || reduceMotion {
		return 0.9
	}
	k := float32((now.UnixNano() % int64(fbBreathe))) / float32(fbBreathe)
	return 0.3 + 0.6*(0.5-0.5*float32(math.Cos(2*math.Pi*float64(k))))
}

// animating reports whether the capsule still moves by itself — only the
// failure shake needs per-frame redraws. A running job's dot instead
// breathes on the timed beats settle() hands to After, so progress costs
// a frame four times per breath, not one per display frame.
func (f *feedback) animating(now time.Time, reduceMotion bool) bool {
	if reduceMotion {
		return false
	}
	d := now.Sub(f.shownAt.Add(fbExpand))
	return !f.live && f.kind == statusErr && d >= 0 && d < fbShake
}

// settle is the time the capsule's fate changes by itself: the end of its
// hold, or the next breath of a running job's dot.
func (f *feedback) settle(now time.Time) time.Time {
	if f.live {
		return now.Add(fbBreathe / 4)
	}
	return f.until
}

// capsuleUp reports whether the notch should show the capsule: the panel is
// fully closed, no hover or file drag has claimed the peek shape, and there
// is something to say.
func (a *shelf) capsuleUp(now time.Time) bool {
	return !a.visible && a.progress <= 0.01 && !a.peeking &&
		!(a.dragging && !a.ownDrag) && a.fb.up(now)
}

// schedule wakes the view at the hold's deadline or the next breathing
// beat. Only the failure shake requests display-rate frames.
func (f *feedback) schedule(c *ui.Context, now time.Time) {
	if t := f.settle(now); !t.IsZero() {
		c.After(max(t.Sub(now), 0) + 10*time.Millisecond)
	}
	if f.animating(now, c.Preferences().ReduceMotion) {
		c.AnimationFrame()
	}
}

// clearExpired drops a toast whose time ran out once the shell has folded
// all the way back, so nothing is measured or said about it afterwards.
func (f *feedback) clearExpired(now time.Time, folded bool) {
	if folded && !f.up(now) {
		f.text, f.job = "", nil
	}
}

// openFrameRoom is the box the window needs for a panel of height expH
// while the opening spring is still in flight: room for the overshoot, plus
// a tick of slack so the frame never trails the shell. The width also holds
// the two ear corners (earR) the shell's top edge draws past its flanks,
// and keeps the parity of the settled width (panelWidth+2*earR): at Bounce
// 0.18 (~1.1% overshoot) the 16pt panel margin already absorbs the width
// overshoot, so w equals panelWidth+2*earR and the window never shifts X
// or changes width when the spring starts or comes to rest.
func openFrameRoom(expH float32) (w, h int) {
	expW := float32(panelWidth - 16)
	w = max(int(math.Ceil(float64(expW*(1+panelOpenOvershoot)))), panelWidth) + 2*earR
	if w%(panelWidth+2*earR)%2 != 0 {
		w++
	}
	return w, int(math.Ceil(float64(expH*(1+openOvershoot)))) + 2
}

// convertProgress is the activity strip's title and step for a running job,
// the words the capsule shows while the panel is closed. The strip in
// converter.go draws the same lines in its own row.
func convertProgress(j *convertJob) (string, bool) {
	if j == nil {
		return "", false
	}
	done, total := int(j.done.Load()), max(int(j.total.Load()), 1)
	title := "正在转成 " + strings.ToUpper(convertExt(j.target))
	switch j.target {
	case toJPEG:
		title = "正在转成 JPEG"
	case toWord:
		title = "正在转成 Word"
	case toMergedPDF:
		title = "正在合并 PDF"
	case toZip:
		title = "正在压缩为 zip"
	case toLarge, toMedium, toSmall:
		title = "正在缩小图片"
	case toFit500, toFit1M, toFit2M, toFit5M:
		title = "正在压缩图片"
	case toExtract:
		title = "正在解压"
	}
	step := fmt.Sprintf("%d/%d", min(done+1, total), total)
	if j.ocr.Load() {
		step = fmt.Sprintf("识别文字 %d/%d", min(done+1, total), total)
	}
	if j.cancel.Load() {
		step = "正在取消…"
	}
	return title + " · " + step, true
}
