//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/layout"
)

var pinTestArea = mygo.Rectangle{X: 0, Y: 25, Width: 1512, Height: 920}

// withPinDisplays feeds the geometry a fixed display set.
func withPinDisplays(t *testing.T, ds []mygo.Display) {
	t.Helper()
	old := pinDisplays
	pinDisplays = func() []mygo.Display { return ds }
	t.Cleanup(func() { pinDisplays = old })
}

var pinTestDisplays = []mygo.Display{
	{ID: 1, Primary: true, Bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1512, Height: 982}, WorkArea: pinTestArea},
}

// pinRegister adds a windowless pin: everything the state machine owns,
// nothing native. The window opens in addPin/show on the real path.
func (a *shelf) pinRegister(it *pinItem) *pinItem {
	it.owner = a
	a.pins = append(a.pins, it)
	return it
}

func TestPinReleaseEdgeBoundary(t *testing.T) {
	for _, c := range []struct {
		x    int
		want pinEdge
	}{
		{0, pinLeft}, {pinSnapDist, pinLeft}, {pinSnapDist + 1, pinFree},
		{-pinSnapDist, pinLeft}, // a hair past the edge still docks
		{pinTestArea.Width - pinSnapDist, pinRight},
		{pinTestArea.Width - pinSnapDist - 1, pinFree},
		{pinTestArea.Width + pinSnapDist, pinRight},
		{pinTestArea.Width / 2, pinFree},
	} {
		if got := pinReleaseEdge(mygo.Point{X: c.x, Y: 400}, pinTestArea); got != c.want {
			t.Errorf("x=%d: got edge %v, want %v", c.x, got, c.want)
		}
	}
}

func TestPinExpandAndStripFrames(t *testing.T) {
	r := pinExpandFrame(pinLeft, pinTestArea, pinCardW, 500)
	if r.X != 0 || r.Width != pinCardW || r.Height != pinCardH || r.Y != 500-pinCardH/2 {
		t.Fatalf("left expand frame wrong: %+v", r)
	}
	r = pinExpandFrame(pinRight, pinTestArea, pinCardW, 500)
	if r.X != pinTestArea.Width-pinCardW || r.Y != 500-pinCardH/2 {
		t.Fatalf("right expand frame wrong: %+v", r)
	}
	s := pinStripFrame(pinLeft, pinTestArea, 500)
	if s.X != 0 || s.Width != pinStripW || s.Height != pinStripH || s.Y != 500-pinStripH/2 {
		t.Fatalf("left strip frame wrong: %+v", s)
	}
	s = pinStripFrame(pinRight, pinTestArea, 500)
	if s.X != pinTestArea.Width-pinStripW || s.Y != 500-pinStripH/2 {
		t.Fatalf("right strip frame wrong: %+v", s)
	}
	// Centres above the menu bar / below the work area clamp back inside.
	if s := pinStripFrame(pinLeft, pinTestArea, 0); s.Y != pinTestArea.Y {
		t.Fatalf("strip at the top edge should clamp to %d, got %+v", pinTestArea.Y, s)
	}
	if s := pinStripFrame(pinLeft, pinTestArea, 2000); s.Y+pinStripH != pinTestArea.Y+pinTestArea.Height {
		t.Fatalf("strip at the bottom edge should end at %d, got %+v", pinTestArea.Y+pinTestArea.Height, s)
	}
}

func TestPinExpandCollapseTiming(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1, edge: pinLeft,
		frame: pinStripFrame(pinLeft, pinTestArea, 500),
	})
	t0 := time.Now()
	over := mygo.Point{X: 4, Y: 500}
	// Hover the strip: it expands flush to the edge on the next tick.
	a.pinTick(t0, over, false, false)
	if !it.expanded || it.frame.X != 0 || it.frame.Width != pinCardW || it.frame.Y != 500-pinCardH/2 {
		t.Fatalf("hover expand: expanded=%v frame=%+v", it.expanded, it.frame)
	}
	// Pointer leaves: the collapse waits pinHideDelay, not less.
	out := mygo.Point{X: 500, Y: 500}
	t1 := t0.Add(300 * time.Millisecond)
	a.pinTick(t1, out, false, false) // the leave itself starts the countdown
	a.pinTick(t1.Add(pinHideDelay-50*time.Millisecond), out, false, false)
	if !it.expanded {
		t.Fatal("collapsed before pinHideDelay")
	}
	a.pinTick(t1.Add(pinHideDelay+50*time.Millisecond), out, false, false)
	if it.expanded || it.frame != pinStripFrame(pinLeft, pinTestArea, 500) {
		t.Fatalf("collapse after delay: expanded=%v frame=%+v", it.expanded, it.frame)
	}
	// Re-hover mid-countdown cancels it; a fresh leave starts it over.
	it.expanded = true
	it.frame = pinExpandFrame(pinLeft, pinTestArea, it.cardW, 500)
	a.pinTick(t0.Add(2*time.Second), over, false, false)
	a.pinTick(t0.Add(2*time.Second+pinHideDelay), out, false, false)
	if !it.expanded {
		t.Fatal("collapsing on the leave tick alone — the countdown is off")
	}
	a.pinTick(t0.Add(2*time.Second+2*pinHideDelay), out, false, false)
	if it.expanded {
		t.Fatal("second collapse did not happen on time")
	}
}

func TestPinFileDragWakesAndHoldsStrip(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1, edge: pinRight,
		frame: pinStripFrame(pinRight, pinTestArea, 400),
	})
	t0 := time.Now()
	// A file drag pinWakeDist from the strip opens it for the drop; an
	// empty-handed pointer at the same distance does not.
	near := mygo.Point{X: pinTestArea.Width - pinStripW - pinWakeDist + 1, Y: 400}
	a.pinTick(t0, near, false, true)
	if !it.expanded || it.frame.X != pinTestArea.Width-pinCardW {
		t.Fatalf("file drag near strip: expanded=%v frame=%+v", it.expanded, it.frame)
	}
	a.pinTick(t0.Add(pinHideDelay*2), near, false, false)
	if !it.expanded {
		t.Fatal("an empty pointer merely near should not hold the card open")
	}
	// A drag that stays near keeps it open past the hide delay.
	a.pinTick(t0.Add(3*time.Second), near, false, true)
	if !it.expanded {
		t.Fatal("a file drag near the card let it collapse mid-drag")
	}
}

func TestPinAlwaysExpanded(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1, edge: pinLeft, always: true,
		frame: pinStripFrame(pinLeft, pinTestArea, 300),
	})
	a.pinTick(time.Now(), mygo.Point{X: 900, Y: 900}, false, false)
	if !it.expanded || it.frame.Width != pinCardW {
		t.Fatalf("always-expanded pin stayed a strip: %+v", it.frame)
	}
	// …and the hide delay never collapses it.
	it.leftAt = time.Now().Add(-time.Hour)
	a.pinTick(time.Now(), mygo.Point{X: 900, Y: 900}, false, false)
	if !it.expanded {
		t.Fatal("always-expanded pin collapsed")
	}
}

func TestPinStagger(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	pins := []*pinItem{
		{display: 1, edge: pinLeft, frame: pinStripFrame(pinLeft, pinTestArea, 300)},
		{display: 1, edge: pinLeft, frame: pinStripFrame(pinLeft, pinTestArea, 320)}, // overlaps the first
		{display: 1, edge: pinRight, frame: pinStripFrame(pinRight, pinTestArea, 300)},
	}
	pinStagger(pins)
	if pins[1].frame.Y != pins[0].frame.Y+pinStripH+pinStripGap {
		t.Fatalf("same-edge strips did not stagger: %+v %+v", pins[0].frame, pins[1].frame)
	}
	if pins[2].frame.Y != 300-pinStripH/2 {
		t.Fatalf("other-edge strip moved: %+v", pins[2].frame)
	}
}

func TestPinMoveSnapAndReturn(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{dataDir: t.TempDir()}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1,
		frame: mygo.Rectangle{X: 600, Y: 400, Width: pinCardW, Height: pinCardH},
	})
	it.pressed, it.pressWas = true, true
	t0 := time.Now()
	// Press goes far → the window follows the cursor.
	a.pinTick(t0, mygo.Point{X: 620, Y: 420}, true, false)
	if !it.moving {
		t.Fatal("a 20-pt press move did not start a window move")
	}
	a.pinTick(t0, mygo.Point{X: 30, Y: 500}, true, false)
	if it.frame.X != 30 || it.frame.Y != 500 {
		t.Fatalf("card did not follow the cursor: %+v", it.frame)
	}
	// Release near the left edge: docked as a strip.
	a.pinTick(t0, mygo.Point{X: 10, Y: 500}, false, false)
	if it.edge != pinLeft || it.expanded || it.frame != pinStripFrame(pinLeft, pinTestArea, 500) {
		t.Fatalf("release at the edge: edge=%v frame=%+v", it.edge, it.frame)
	}
}

func TestPinReturnToNotch(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{dataDir: t.TempDir(), notchW: 200}
	a.layout = layout.Layout{}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1, homeIndex: 0,
		frame: mygo.Rectangle{X: 600, Y: 400, Width: pinCardW, Height: pinCardH},
	})
	a.layout.Pins = []layout.Pin{{ID: "zip", Span: 1}}
	it.pressed, it.pressWas = true, true
	t0 := time.Now()
	a.pinTick(t0, mygo.Point{X: 700, Y: 60}, true, false)
	if !it.moving {
		t.Fatal("drag did not start")
	}
	a.pinTick(t0, mygo.Point{X: pinTestArea.Width/2 - 40, Y: 30}, false, false)
	if len(a.pins) != 0 {
		t.Fatalf("release on the notch kept the pin: %+v", it.frame)
	}
	// putBack: the card went back to the tools page at its home slot.
	if len(a.layout.Cards) != 1 || a.layout.Cards[0].ID != "zip" {
		t.Fatalf("the card did not return to the grid: %+v", a.layout.Cards)
	}
}

func TestPinUnpinSemantics(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	a := &shelf{dataDir: t.TempDir()}
	// 放回刘海: the card rejoins the grid at its home slot.
	a.layout = layout.Layout{Cards: []layout.Card{{ID: "awake", Span: 1}}}
	it := a.pinRegister(&pinItem{
		cardID: "topdf", cardW: pinCardW, display: 1, homeIndex: 1,
		frame: mygo.Rectangle{X: 600, Y: 400, Width: pinCardW, Height: pinCardH},
	})
	a.layout.Pins = []layout.Pin{{ID: "topdf", Span: 1}}
	a.unpin(it, true)
	if len(a.layout.Cards) != 2 || a.layout.Cards[0].ID != "awake" || a.layout.Cards[1].ID != "topdf" {
		t.Fatalf("放回刘海 did not restore the home slot: %+v", a.layout.Cards)
	}
	// 从桌面移除: the pin goes, the card stays off the page.
	it2 := a.pinRegister(&pinItem{
		cardID: "awake", cardW: pinCardW, display: 1, homeIndex: 0,
		frame: mygo.Rectangle{X: 600, Y: 400, Width: pinCardW, Height: pinCardH},
	})
	a.layout.Pins = []layout.Pin{{ID: "awake", Span: 1}}
	a.layout.Cards = a.layout.Cards[1:] // awake was pinned out of the grid; topdf stays
	a.unpin(it2, false)
	if len(a.layout.Cards) != 1 || a.layout.Cards[0].ID != "topdf" {
		t.Fatalf("从桌面移除 put the card back: %+v", a.layout.Cards)
	}
	// A home slot inserts at that index (design §4 原位/被占放末尾由
	// RemovePin 的 putBack clamp 处理).
	it3 := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 1, homeIndex: 0,
		frame: mygo.Rectangle{X: 600, Y: 400, Width: pinCardW, Height: pinCardH},
	})
	a.layout.Pins = []layout.Pin{{ID: "zip", Span: 1}}
	a.unpin(it3, true)
	if len(a.layout.Cards) != 2 || a.layout.Cards[0].ID != "zip" {
		t.Fatalf("home slot insert wrong: %+v", a.layout.Cards)
	}
}

func TestPinDisplaysChanged(t *testing.T) {
	ext := mygo.Display{ID: 2, Bounds: mygo.Rectangle{X: 1512, Y: 0, Width: 1920, Height: 1080},
		WorkArea: mygo.Rectangle{X: 1512, Y: 25, Width: 1920, Height: 1055}}
	withPinDisplays(t, append([]mygo.Display{pinTestDisplays[0]}, ext))
	a := &shelf{dataDir: t.TempDir()}
	it := a.pinRegister(&pinItem{
		cardID: "zip", cardW: pinCardW, display: 2, edge: pinRight,
		frame: mygo.Rectangle{X: 1512 + 1920 - pinStripW, Y: 400, Width: pinStripW, Height: pinStripH},
	})
	// The external display goes away: the pin keeps its edge on the primary.
	withPinDisplays(t, pinTestDisplays)
	a.pinDisplaysChanged()
	if it.display != 1 || it.edge != pinRight ||
		it.frame.X != pinTestArea.Width-pinStripW {
		t.Fatalf("unplugged display: display=%d frame=%+v", it.display, it.frame)
	}
}

func TestPinLayoutRoundTrip(t *testing.T) {
	withPinDisplays(t, pinTestDisplays)
	dir := t.TempDir()
	a := &shelf{dataDir: dir}
	a.layout = layout.Layout{Version: layout.Version}
	a.pinRegister(&pinItem{
		cardID: "awake", span: 1, cardW: pinCardW, display: 1, edge: pinRight, always: true,
		frame: mygo.Rectangle{X: 1512 - 8, Y: 300, Width: pinStripW, Height: pinStripH},
	})
	a.savePins()
	// layout.json carries the pin; restore rebuilds it (positions are
	// offsets inside the display's work area).
	data, err := os.ReadFile(filepath.Join(dir, layout.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var f layout.Layout
	if err := json.Unmarshal(data, &f); err != nil || len(f.Pins) != 1 ||
		f.Pins[0].ID != "awake" || f.Pins[0].Edge != layout.EdgeRight || !f.Pins[0].AlwaysOpen {
		t.Fatalf("layout.json wrong: %v %+v", err, f.Pins)
	}
	b := &shelf{dataDir: dir}
	b.layout = layout.LoadQuiet(dir)
	if len(b.layout.Pins) != 1 || b.layout.Pins[0].ID != "awake" {
		t.Fatalf("restored pins wrong: %+v", b.layout.Pins)
	}
}

func TestPinReceiveActionAndFallback(t *testing.T) {
	// Files the card accepts run its drop action; other drops take the
	// tray intake so a release on a pin never loses files. runToolCardDrop
	// needs the full converter; the seam check here is the class gate.
	def := toolCardDefFor("toword")
	if def == nil {
		t.Skip("no catalog")
	}
	img := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(img, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !def.accepts(classifyDrop([]string{img})) {
		t.Fatal("toword should accept an image drop")
	}
	// The tray fallback path: zip takes any class, tojpeg does not take
	// archives — the gate chooses the intake.
	a := &shelf{}
	it := a.pinRegister(&pinItem{cardID: "tojpeg", cardW: pinCardW, display: 1,
		frame: mygo.Rectangle{X: 0, Y: 0, Width: pinCardW, Height: pinCardH}})
	zip := filepath.Join(t.TempDir(), "b.zip")
	if err := os.WriteFile(zip, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = it
	if def := toolCardDefFor("tojpeg"); def.accepts(classifyDrop([]string{zip})) {
		t.Fatal("tojpeg must not accept archives; pinReceive would shelve instead")
	}
}

// pinViewShelf is a headless shelf whose pin view builds in a Tester.
func pinViewShelf(t *testing.T, cardID string, expanded bool) (*shelf, *pinItem, *ui.Tester) {
	t.Helper()
	a := &shelf{dataDir: t.TempDir()}
	it := a.pinRegister(&pinItem{
		cardID: cardID, cardW: pinCardW, display: 1, edge: pinLeft, expanded: expanded,
		frame: mygo.Rectangle{X: 0, Y: 300, Width: pinCardW, Height: pinCardH},
	})
	return a, it, ui.NewTester(func(c *ui.Context) { it.view(c) }, pinCardW, pinCardH)
}

func TestPinViewMenuItems(t *testing.T) {
	_, it, tr := pinViewShelf(t, "zip", true)
	tr.RightClickAt(40, 40)
	menu := tr.Menu()
	want := []string{"始终展开", "-", "放回刘海", "从桌面移除"}
	if len(menu) != len(want) {
		t.Fatalf("menu = %v, want %v", menu, want)
	}
	for i := range want {
		if menu[i] != want[i] {
			t.Fatalf("menu = %v, want %v", menu, want)
		}
	}
	// 从桌面移除 marks the pin for the tick's destroy without putBack.
	if err := tr.ChooseMenuItem("从桌面移除"); err != nil {
		t.Fatal(err)
	}
	if !it.wantKill || it.putBack {
		t.Fatalf("从桌面移除: wantKill=%v putBack=%v", it.wantKill, it.putBack)
	}
}

var _ = transfer.Copy // transfer stays imported for the drop plumbing
