//go:build darwin

package main

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/settings"
)

// fakeActivityModule is a module that answers the island's activity query
// with whatever the test set.
type fakeActivityModule struct {
	act  activity
	live bool
	seen int
}

func (m *fakeActivityModule) tick(time.Time) { m.seen++ }

func (m *fakeActivityModule) activity(time.Time) (activity, bool) { return m.act, m.live }

func TestCurrentActivityPriorityAndExpiry(t *testing.T) {
	now := time.Now()
	low := &fakeActivityModule{act: activity{ID: "music", Priority: 1}, live: true}
	high := &fakeActivityModule{act: activity{ID: "volume", Priority: 5}, live: true}
	off := &fakeActivityModule{act: activity{ID: "off", Priority: 99}, live: false}
	expired := &fakeActivityModule{act: activity{ID: "old", Priority: 99, Until: now.Add(-time.Second)}, live: true}

	act, ok := currentActivity([]module{low, high, off, expired}, now)
	if !ok || act.ID != "volume" {
		t.Fatalf("highest live priority must win, got %+v (ok=%v)", act, ok)
	}
	// Ties keep module order: the earlier module is found first and a
	// strictly greater priority is what replaces it.
	tie := &fakeActivityModule{act: activity{ID: "tie", Priority: 5}, live: true}
	act, _ = currentActivity([]module{tie, high}, now)
	if act.ID != "tie" {
		t.Fatalf("a priority tie must keep module order, got %q", act.ID)
	}
	// A transient activity stays live until its Until passes.
	soon := &fakeActivityModule{act: activity{ID: "flash", Priority: 9, Until: now.Add(time.Second)}, live: true}
	if act, _ := currentActivity([]module{soon}, now); act.ID != "flash" {
		t.Fatalf("an unexpired transient must be live, got %q", act.ID)
	}
	if _, ok := currentActivity([]module{off, expired}, now); ok {
		t.Fatal("dead and expired providers must not yield an activity")
	}
}

// The island's state machine: appear with an animation, swap on an id
// change (fold shut, swap, grow back), fold out when the activity ends.
func TestIslandTickAppearSwapFold(t *testing.T) {
	a := &shelf{}
	m := &fakeActivityModule{act: activity{ID: "battery", Trailing: "87%"}, live: true}
	a.modules = []module{m}
	now := time.Now()

	a.islandTick(now)
	if !a.hasIsland() || a.island.Trailing != "87%" {
		t.Fatalf("a live activity must appear: %+v", a.island)
	}
	if a.islandStart.IsZero() {
		t.Fatal("appearing must animate the wings")
	}
	pump := func() {
		for i := 0; i < 80 && !a.islandStart.IsZero(); i++ {
			now = now.Add(10 * time.Millisecond)
			a.animate(now)
		}
	}
	pump()
	if a.islandK != 1 {
		t.Fatalf("wings must settle fully open, islandK=%v", a.islandK)
	}
	// A steady activity neither re-animates nor forgets the content.
	a.islandTick(now)
	if !a.islandStart.IsZero() {
		t.Fatal("an unchanged activity must not restart the animation")
	}
	m.act.Trailing = "86%"
	a.islandTick(now)
	if a.island.Trailing != "86%" {
		t.Fatalf("changed trailing text must update the content: %+v", a.island)
	}
	if !a.islandStart.IsZero() {
		t.Fatal("a value-only change repaints; it must not re-animate the wings")
	}
	// A new source swaps in: fold shut, then grow back with the new content.
	m.act = activity{ID: "volume", Trailing: "50%", Level: 0.5, Priority: 2}
	a.islandTick(now)
	if !a.islandSwap || a.islandNext.ID != "volume" {
		t.Fatalf("an id change must fold before swapping: swap=%v next=%+v", a.islandSwap, a.islandNext)
	}
	for i := 0; i < 200 && (a.islandSwap || !a.islandStart.IsZero()); i++ {
		now = now.Add(10 * time.Millisecond)
		a.animate(now)
	}
	if a.islandSwap || a.island.ID != "volume" || a.islandK != 1 {
		t.Fatalf("swap did not settle: swap=%v island=%+v k=%v", a.islandSwap, a.island, a.islandK)
	}
	// The source goes away: the wings fold back into the notch, keeping
	// the outgoing content visible until the fold completes.
	m.live = false
	a.islandTick(now)
	if !a.islandSwap || !a.hasIsland() {
		t.Fatalf("a dead provider must fold out, not pop: swap=%v island=%+v", a.islandSwap, a.island)
	}
	if a.islandStart.IsZero() {
		t.Fatal("disappearing must animate the wings shut")
	}
	for i := 0; i < 200 && (a.islandSwap || !a.islandStart.IsZero()); i++ {
		now = now.Add(10 * time.Millisecond)
		a.animate(now)
	}
	if a.hasIsland() || a.islandK != 0 || a.islandSwap {
		t.Fatalf("fold-out did not settle: island=%+v k=%v swap=%v", a.island, a.islandK, a.islandSwap)
	}
}

// Idle with a live activity: the shell widens into wings around the fake
// notch, trailing text is laid out, and nothing paints outside the shell.
func TestIslandIdleRendersWingsAndTrailing(t *testing.T) {
	a := &shelf{
		win: &mygo.Window{}, x: 470, y: 0, h: 380,
		notchX: 645, notchY: 0, notchW: 180, notchH: 32,
		islandK: 1,
		island:  activity{ID: "battery", Trailing: "87%", Level: 0.6, Priority: 1},
	}
	width := a.islandStripW()
	view := ui.NewTester(a.view, width, 72)
	if !view.HasText("87%") {
		t.Fatal("the island must show the activity's trailing text")
	}
	if view.HasText("点击展开 Shelf") {
		t.Fatal("the island state must not draw the peek pill")
	}
	img := view.Image()
	b := img.Bounds()
	// Row 16 crosses the wings: opaque run has to equal the drawn shell.
	first, last := -1, -1
	for x := 0; x < b.Dx(); x++ {
		if _, _, _, alpha := img.At(x, 16).RGBA(); alpha > 0 {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	wantWf, _ := a.shellSize(0)
	wantW := int(wantWf + 0.5)
	if got := last - first + 1; got != wantW {
		t.Fatalf("island shell width = %d, want %d (wings must widen the notch)", got, wantW)
	}
	if first != (b.Dx()-wantW)/2 {
		t.Fatalf("shell must stay centered in the strip: first=%d width=%d strip=%d", first, wantW, b.Dx())
	}
	if _, _, _, alpha := img.At(2, 16).RGBA(); alpha == 0 {
		t.Fatal("left wing (leading glyph side) is not drawn")
	}
	if _, _, _, alpha := img.At(b.Dx()-3, 16).RGBA(); alpha == 0 {
		t.Fatal("right wing (trailing side) is not drawn")
	}
}

// Repeated paints of the same activity re-measure nothing: the trailing
// text's width and its truncation are both cached (islandMeasures only
// counts real MeasureText calls).
func TestIslandRepaintMeasuresOnce(t *testing.T) {
	a := &shelf{
		win: &mygo.Window{}, x: 470, y: 0, h: 380,
		notchX: 645, notchY: 0, notchW: 180, notchH: 32,
		islandK: 1,
		island:  activity{ID: "battery", Trailing: "87%", Level: 0.6, Priority: 1},
	}
	view := ui.NewTester(a.view, a.islandStripW(), 72)
	first := a.islandMeasures
	if first == 0 {
		t.Fatal("the first paint must measure the trailing text")
	}
	for range 5 {
		view.Frame()
	}
	if a.islandMeasures != first {
		t.Fatalf("repaints re-measured: %d -> %d calls", first, a.islandMeasures)
	}
	// A changed value measures once more, then settles again.
	a.island.Trailing = "86%"
	view.Frame()
	got := a.islandMeasures
	if got <= first {
		t.Fatal("a new trailing text must be measured")
	}
	view.Frame()
	if a.islandMeasures != got {
		t.Fatalf("the settled repaint re-measured: %d -> %d calls", got, a.islandMeasures)
	}
}

// Without an activity the collapsed shelf draws nothing at all: the
// resident strip keeps today's invisible idle state.
func TestIslandIdleWithoutActivityDrawsNothing(t *testing.T) {
	a := &shelf{
		win: &mygo.Window{}, x: 470, y: 0, h: 380,
		notchX: 645, notchY: 0, notchW: 180, notchH: 32,
	}
	view := ui.NewTester(a.view, a.islandStripW(), 72)
	img := view.Image()
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha != 0 {
				t.Fatalf("idle shelf painted at %d,%d without an activity", x, y)
			}
		}
	}
}

// The hit region follows the drawn shell: the bare notch when idle, the
// notch plus wings while an activity shows, and never the strip's rest
// (menu-bar items beside the notch stay clickable).
func TestIslandHitGeometry(t *testing.T) {
	a := &shelf{x: 470, y: 0, h: 380, notchX: 645, notchY: 0, notchW: 180, notchH: 32}
	center := a.stripCenter()
	if w, _ := a.idleBand(); w != a.islandRect().Width {
		t.Fatalf("idle band width = %d, want the bare notch %d", w, a.islandRect().Width)
	}
	if w, _ := a.idleBand(); w != max(180, a.notchW) {
		t.Fatalf("bare notch band must stay notch-wide, got %d", w)
	}
	// No activity: the wings are neither drawn nor clickable.
	if a.overIsland(mygo.Point{X: center - a.islandStripW()/2 + 4, Y: 16}) {
		t.Fatal("wing area must not be an island target without an activity")
	}
	a.islandK, a.island = 1, activity{ID: "battery", Trailing: "87%"}
	if w, _ := a.idleBand(); w != a.islandStripW() {
		t.Fatalf("island band width = %d, want the drawn wings' %d", w, a.islandStripW())
	}
	left := mygo.Point{X: center - a.islandRect().Width/2 + 4, Y: 16}
	right := mygo.Point{X: center + a.islandRect().Width/2 - 4, Y: 16}
	for _, p := range []mygo.Point{left, right} {
		if !a.overIsland(p) {
			t.Fatalf("point %+v on a drawn wing must count as the island", p)
		}
	}
	if a.overIsland(mygo.Point{X: center - a.islandStripW()/2 - 40, Y: 16}) {
		t.Fatal("a point left of the wings must keep passing through")
	}
	if a.overIsland(mygo.Point{X: center, Y: a.islandRect().Height + 8}) {
		t.Fatal("a point below the island must keep passing through")
	}
	// The resident strip still fits the wings, and still covers the
	// drag-guard corridor (notchTriggerWidth wide) around the notch.
	if got := a.desiredFrame().Width; got < a.islandStripW() {
		t.Fatalf("resident strip %d does not fit the island %d", got, a.islandStripW())
	}
	strip := a.desiredFrame()
	if strip.X+strip.Width < center+notchTriggerWidth/2 || strip.X > center-notchTriggerWidth/2 {
		t.Fatalf("resident strip %+v must cover the drag corridor", strip)
	}
}

// The wings fold away as the shell becomes the peek pill or the expanded
// panel: the drawn width follows the shared interpolation, no overlap.
func TestIslandFoldsIntoPeekAndPanel(t *testing.T) {
	a := &shelf{x: 470, y: 0, h: 380, notchX: 645, notchY: 0, notchW: 180, notchH: 32, islandK: 1}
	base := float32(max(180, a.notchW))
	if w, _ := a.shellSize(0); w != base+2*a.islandWingW() {
		t.Fatalf("collapsed width = %v, want the wings open at %v", w, base+2*a.islandWingW())
	}
	a.peekProgress = 1
	if w, _ := a.shellSize(0); w != base+56 {
		t.Fatalf("peek width = %v, want the wingless pill at %v", w, base+56)
	}
	a.peekProgress = 0
	if w, _ := a.shellSize(1); w != float32(panelWidth-16) {
		t.Fatalf("expanded width = %v, want %v", w, panelWidth-16)
	}
	// Wings fade out as either state takes over, so their content is gone
	// before the pill/panel is drawn.
	if k := a.islandContentK(1); k != 0 {
		t.Fatalf("wing content must be hidden while expanded (k=%v)", k)
	}
	a.peekProgress = 1
	if k := a.islandContentK(0); k != 0 {
		t.Fatalf("wing content must be hidden while peeking (k=%v)", k)
	}
}

// A click on a wing expands the shelf even when WindowServer handed the
// press to the menu bar (the wings sit in its empty centre).
func TestIslandWingClickExpandsShelf(t *testing.T) {
	a := &shelf{x: 470, y: 0, h: 380, notchX: 645, notchY: 0, notchW: 180, notchH: 32, islandK: 1, island: activity{ID: "battery", Trailing: "87%"}}
	left := mygo.Point{X: a.stripCenter() - a.islandRect().Width/2 + 4, Y: 16}
	a.mouseDown(false, left)
	if !a.visible {
		t.Fatal("clicking the island's wing must expand the shelf")
	}
	// With no activity the same point is outside the drawn shell and must
	// not expand anything.
	b := &shelf{x: 470, y: 0, h: 380, notchX: 645, notchY: 0, notchW: 180, notchH: 32}
	b.mouseDown(false, left)
	if b.visible {
		t.Fatal("a click outside the drawn shell must not expand the shelf")
	}
}

// The first module whose panelCard reports "drawn" gets the tray tab's
// slot above the list; later cards and the clipboard tab are untouched.
func TestPanelCardCallsFirstDrawnModule(t *testing.T) {
	first := &fakeCard{label: "第一个卡片", drawn: false}
	second := &fakeCard{label: "第二个卡片", drawn: true}
	third := &fakeCard{label: "第三个卡片", drawn: true}
	a := &shelf{tab: tabTray}
	a.modules = []module{first, second, third}

	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if view.HasText("第一个卡片") || view.HasText("第三个卡片") {
		t.Fatal("a card that reports not-drawn must not show, and later ones must not be asked")
	}
	if !view.HasText("第二个卡片") {
		t.Fatal("the first card that draws must show above the list")
	}
	if !first.asked || !second.asked || third.asked {
		t.Fatalf("cards must be asked in order until one draws: %v %v %v", first.asked, second.asked, third.asked)
	}
	// The clipboard tab keeps its list without any card.
	second.asked = false
	a.tab = tabClipboard
	view = ui.NewTester(a.view, panelWidth, panelHeight)
	if second.asked || view.HasText("第二个卡片") {
		t.Fatal("the clipboard tab must not draw panel cards")
	}
}

type fakeCard struct {
	label string
	drawn bool
	asked bool
}

func (c *fakeCard) tick(time.Time) {}

func (c *fakeCard) card(view *ui.Context) bool {
	c.asked = true
	if !c.drawn {
		return false
	}
	ui.Text(view, c.label).FontSize(12).TextColor(textPrimary)
	return true
}

// The island menu's check items save their setting and are disabled while
// settings.json could not be read (so a broken file is never overwritten).
func TestIslandMenuSavesToggles(t *testing.T) {
	dir := t.TempDir()
	a := &shelf{dataDir: dir, settings: settings.Default()}
	a.islandMenu()[0].Click(nil, nil)
	saved, err := settings.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.IslandNowPlaying {
		t.Fatal("unchecking 正在播放 must save islandNowPlaying=false")
	}
	if !saved.IslandBattery || !saved.IslandVolume {
		t.Fatalf("one toggle changed the others: %+v", saved)
	}
	if a.settings.IslandNowPlaying {
		t.Fatal("the running setting must follow the saved one")
	}
	if a.islandMenu()[0].Checked {
		t.Fatal("a rebuilt menu must show the saved state")
	}
	// A broken settings file is never overwritten: the item is disabled
	// and its click must not write anything.
	broken := &shelf{dataDir: t.TempDir(), settingsBroken: true, settings: settings.Default()}
	for _, it := range broken.islandMenu() {
		if !it.Disabled {
			t.Fatalf("menu item %q must be disabled while settings are broken", it.Label)
		}
		it.Click(nil, nil)
	}
	if _, err := os.Stat(filepath.Join(broken.dataDir, settings.FileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("broken settings were written: %v", err)
	}
}

// islandSmokeProvider is injected by the gated smoke test: a live activity
// with an icon, trailing text and a level bar, so a screenshot of the real
// window shows the whole island.
type islandSmokeProvider struct{}

func (p *islandSmokeProvider) tick(time.Time) {}

func (p *islandSmokeProvider) activity(time.Time) (activity, bool) {
	return activity{ID: "smoke", Priority: 1, Leading: shelfSymbol, LeadingTint: textPrimary, Trailing: "87%", Level: 0.6}, true
}

// TestIslandSmokeScreenshot draws the island in the real notch window,
// screenshots it with `screencapture -l <id>` and checks the picture: the
// drawn pixels must be wider than the notch (wings) and sit on both sides
// of its centre. Run with SHELF_BASKET_SMOKE=1 (see basket_test.go).
// SHELF_ISLAND_HOLD=1 keeps the island up for an external screenshot.
func TestIslandSmokeScreenshot(t *testing.T) {
	if os.Getenv("SHELF_BASKET_SMOKE") != "1" {
		t.Skip("set SHELF_BASKET_SMOKE=1 to run the native island smoke test")
	}
	a := smokeShelf
	if a.win == nil {
		onMain(func() { a.newNotchWindow() })
	}
	if a.win == nil {
		t.Fatal("no notch window")
	}
	onMain(func() {
		a.modules = append(a.modules, &islandSmokeProvider{})
		a.place(mygo.Screen.CursorScreenPoint())
		a.syncMousePassThrough()
		a.win.ShowInactive()
	})
	// Step the island animation by hand: the harness has no monitor ticker.
	for i := 0; i < 100; i++ {
		settled := false
		onMain(func() {
			now := time.Now()
			a.islandTick(now)
			a.animate(now)
			settled = a.islandK > 0.99 && a.islandStart.IsZero()
		})
		if settled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !a.hasIsland() || a.islandK <= 0.99 {
		t.Fatalf("island never opened in the real window: has=%v k=%v", a.hasIsland(), a.islandK)
	}
	num := mac.WindowNumber(a.win.NativeHandle())
	if num == 0 {
		t.Fatal("notch window has no CGWindowID")
	}
	time.Sleep(200 * time.Millisecond) // let the shot see the settled frame
	shot := filepath.Join(os.TempDir(), "shelf-island-smoke.png")
	out, err := exec.Command("screencapture", "-x", "-o", fmt.Sprintf("-l%d", num), shot).CombinedOutput()
	if err != nil {
		t.Fatalf("screencapture failed (%v): %s", err, out)
	}
	f, err := os.Open(shot)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatalf("decode %s: %v", shot, err)
	}
	box := opaqueBounds(img)
	if box.Dx() <= max(180, a.notchW) {
		t.Fatalf("screenshot %s shows %dpt of paint; wings must widen the notch", shot, box.Dx())
	}
	center := box.Min.X + box.Dx()/2
	left := countOpaque(img, box.Min.X, center)
	right := countOpaque(img, center, box.Max.X)
	if left == 0 || right == 0 {
		t.Fatalf("picture has paint only on one side of the notch (left=%d right=%d)", left, right)
	}
	t.Logf("island CGWindowID=%d shot=%s drawn=%v (left=%d right=%d)", num, shot, box, left, right)
	if os.Getenv("SHELF_ISLAND_HOLD") != "" {
		time.Sleep(8 * time.Second)
	}
}

// opaqueBounds is the bounding box of the non-transparent pixels.
func opaqueBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	box := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha > 0 {
				if box.Empty() {
					box = image.Rect(x, y, x+1, y+1)
					continue
				}
				if x < box.Min.X {
					box.Min.X = x
				}
				if y < box.Min.Y {
					box.Min.Y = y
				}
				if x+1 > box.Max.X {
					box.Max.X = x + 1
				}
				if y+1 > box.Max.Y {
					box.Max.Y = y + 1
				}
			}
		}
	}
	return box
}

// countOpaque counts opaque pixels in the vertical band [x0, x1).
func countOpaque(img image.Image, x0, x1 int) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := x0; x < x1; x++ {
			if _, _, _, alpha := img.At(x, y).RGBA(); alpha > 0 {
				n++
			}
		}
	}
	return n
}
