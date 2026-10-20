//go:build darwin

package main

// Completion feedback: the panel's bottom toast only exists while the panel
// is open, so anything that finishes with the panel closed grows a capsule
// out of the hardware notch instead — a 16pt status dot and the toast's own
// words, ringed by a glow of the status colour. The capsule is a peek state
// of the resident window, not a window of its own: the shell, the spring and
// the pass-through rules are the ones the hover pill already uses.
//
// Status colours are the one exception to the panel's single accent: they
// appear only while a capsule shows, and nowhere else.

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
	fbBreathe = 1600 * time.Millisecond // one breath of a running job's glow
	fbGlowPad = 34                      // frame margin for the glow's blur and spread
	fbMaxW    = 380                     // the capsule ellipsizes past this width
	fbDot     = 16                      // the status dot
	fbGap     = 7                       // dot to text
	fbPadX    = 12                      // text to capsule edge
	fbBody    = 22                      // capsule height below the notch band
	fbExpand  = 380 * time.Millisecond  // the grow-in the hold is measured from
)

// The shell's springs. Expanding overshoots by about 4% (Bounce 0.28) and
// settles back; collapsing is critically damped and crisp; the hover shapes
// morph with a light bounce of their own; the capsule gives way the same way
// it arrived.
var (
	openSpring    = ui.Animation{Spring: ui.SpringOptions{Duration: 380 * time.Millisecond, Bounce: 0.28}}
	closeSpring   = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}}
	peekSpring    = ui.Animation{Spring: ui.SpringOptions{Duration: peekAnimation, Bounce: 0.15}}
	retractSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}}
	shakeSpring   = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0}}
)

// openOvershoot is the room the frame keeps for a spring that is still on
// its way: Bounce 0.28 peaks near 4%, plus a frame's worth of catch-up.
const openOvershoot = 0.045

// shellMotion is what view() drew last: the drawn shell's size, for the
// window frame to hold while the springs are in flight, and whether the
// opening spring has come to rest.
type shellMotion struct {
	w, h       float32
	openMoving bool // the opening spring has not come to rest
	peek       bool // w and h are the peek shape's, not the panel's
}

// feedback is the capsule's state: what it says, for how long, and how wide
// the last frame measured that text.
type feedback struct {
	text       string
	isErr      bool
	live       bool // the words are a running job's progress, refreshed per frame
	shownAt    time.Time
	until      time.Time
	pillW      float32
	drawn      bool        // drawn in the last frame (the window's command-line tests read it)
	retracting bool        // the shell is folding back from the capsule
	job        *convertJob // the job whose progress the capsule shows
}

// up reports whether the capsule has something to say now.
func (f *feedback) up(now time.Time) bool {
	if f.text == "" {
		return false
	}
	return f.until.IsZero() || now.Before(f.until)
}

// color is the status colour of what it says.
func (f *feedback) color() ui.Color {
	switch {
	case f.live:
		return fbOrange
	case f.isErr:
		return fbRed
	}
	return fbGreen
}

// glyph is the sign inside the dot. A running job has no verdict yet: the
// dot holds an ellipsis, as the design's waiting state does.
func (f *feedback) glyph() string {
	switch {
	case f.live:
		return "…"
	case f.isErr:
		return "!"
	}
	return "✓"
}

// show turns a toast into a capsule: a success holds fbHoldOK and a failure
// fbHoldErr, both counted from the moment it has grown out of the notch.
// The text is measured again by the next frame.
func (f *feedback) show(text string, isErr bool, now time.Time) {
	f.text, f.isErr, f.live = text, isErr, false
	f.shownAt = now
	f.until = now.Add(fbExpand + fbHoldOK)
	if isErr {
		f.until = now.Add(fbExpand + fbHoldErr)
	}
	f.pillW = 0
}

// follow points the capsule at the running conversion job, so a panel that
// is closed while a job runs keeps reporting it. A toast posted meanwhile
// keeps the capsule while it lasts; the progress returns after it.
func (f *feedback) follow(a *shelf, now time.Time) {
	var job *convertJob
	if a.conv != nil {
		job = a.conv.job
	}
	live := job != nil && a.conv.live()
	switch {
	case live && (f.live || !f.up(now) || f.job == nil):
		text, ok := convertProgress(job)
		if !ok {
			return
		}
		if f.job != job {
			f.shownAt = now
			f.until = time.Time{}
		}
		f.job = job
		f.text, f.isErr, f.live = text, false, true
		f.pillW = 0
	case !live:
		if f.job != nil {
			f.job = nil
			if f.live {
				f.live, f.text = false, ""
			}
		}
	}
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
	w, _ := c.MeasureText(0, ui.Span{Text: strings.ReplaceAll(f.text, "\n", " "), Size: 12, Weight: 600})
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

// shake is the capsule's horizontal offset now: one decaying swing, left
// first, that starts once it has grown out.
func (f *feedback) shake(now time.Time, reduceMotion bool) float32 {
	if !f.isErr || reduceMotion {
		return 0
	}
	d := now.Sub(f.shownAt.Add(fbExpand))
	if d <= 0 || d >= fbShake {
		return 0
	}
	t := float32(d) / float32(fbShake)
	// One swing left first, then right, each 0.2 of the run apart, losing a
	// tenth of its size every step: the design's decaying keyframes.
	return -fbShakeX * (1 - t) * float32(math.Sin(2*math.Pi*float64(t)/0.4))
}

// glow is the glow's opacity: a steady 0.9 for a verdict, a slow breath for
// a job that is still running.
func (f *feedback) glow(now time.Time, reduceMotion bool) float32 {
	if !f.live || reduceMotion {
		return 0.9
	}
	k := float32((now.UnixNano() % int64(fbBreathe))) / float32(fbBreathe)
	return 0.35 + 0.65*(0.5-0.5*float32(math.Cos(2*math.Pi*float64(k))))
}

// settle is the time the capsule's fate changes by itself: the end of its
// hold, or the next breath of a running job's glow.
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

// schedule keeps frames coming while the capsule moves, and wakes the view
// when its hold runs out.
func (f *feedback) schedule(c *ui.Context, now time.Time) {
	if t := f.settle(now); !t.IsZero() {
		c.After(max(t.Sub(now), 0) + 10*time.Millisecond)
	}
	if c.Preferences().ReduceMotion {
		return
	}
	if f.live {
		c.AnimationFrame() // the glow breathes
	}
	if d := now.Sub(f.shownAt.Add(fbExpand)); f.isErr && d >= 0 && d < fbShake {
		c.AnimationFrame() // one decaying shake
	}
}

// openFrameRoom is the box the window needs for a panel of height expH
// while the opening spring is still in flight: room for the overshoot, plus
// a tick of slack so the frame never trails the shell.
func openFrameRoom(expH float32) (w, h int) {
	expW := float32(panelWidth - 16)
	return int(math.Ceil(float64(expW*(1+openOvershoot)))) + 2,
		int(math.Ceil(float64(expH*(1+openOvershoot)))) + 2
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
