//go:build darwin

package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"shelf/internal/settings"
	"shelf/internal/store"
)

// testImagePNG is a small real PNG; hue distinguishes captures so the
// content-addressed store keeps them as separate items.
func testImagePNG(t *testing.T, hue uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: hue, G: uint8(16 * x), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testOCRShelf builds a headless shelf with a clipboard image capture and
// an ocrIndex whose recognizer and dispatch are fully synchronous.
func testOCRShelf(t *testing.T) (*shelf, *ocrIndex, *recordClip) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	rec := &recordClip{}
	a := &shelf{
		store: s, dataDir: dir, visible: true, settings: settings.Default(),
		thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{},
		appNames: map[string]string{}, write: rec,
	}
	o := &ocrIndex{host: a, interval: -1}
	o.dispatch = func(f func()) { f() }
	o.run = func(f func()) { f() }
	a.modules = append(a.modules, o)
	return a, o, rec
}

// addClipImage records a clipboard image in the store the way the
// clipboard module does.
func addClipImage(t *testing.T, a *shelf, hue uint8) store.Item {
	t.Helper()
	if err := a.store.AddClipImage(testImagePNG(t, hue), "com.example.Shot"); err != nil {
		t.Fatal(err)
	}
	return a.store.Items[0]
}

// TestOCRBackfillRecognizesOnce covers the background pass: an old
// clipboard image is recognized on tick, the result is stored, and a
// recognized item is never asked again.
func TestOCRBackfillRecognizesOnce(t *testing.T) {
	a, o, _ := testOCRShelf(t)
	addClipImage(t, a, 30)
	calls := 0
	o.recognize = func(path string) ([]string, error) {
		calls++
		return []string{"货架 Hello"}, nil
	}
	o.tick(time.Now())
	if calls != 1 {
		t.Fatalf("tick should recognize the pending image once, got %d", calls)
	}
	if a.store.Items[0].OCRText != "货架 Hello" || !a.store.Items[0].OCRDone {
		t.Fatalf("result not stored: %+v", a.store.Items[0])
	}
	o.tick(time.Now())
	if calls != 1 {
		t.Fatalf("recognized image must not be re-recognized")
	}
}

// TestOCRNewestFirst: a fresh capture is recognized before older pending
// ones.
func TestOCRNewestFirst(t *testing.T) {
	a, o, _ := testOCRShelf(t)
	addClipImage(t, a, 30) // older
	addClipImage(t, a, 60) // newer ends up at Items[0]
	var order []string
	o.recognize = func(path string) ([]string, error) {
		order = append(order, path)
		return nil, nil
	}
	o.tick(time.Now())
	if len(order) != 1 {
		t.Fatalf("one image per tick, got %d", len(order))
	}
	if order[0] != a.store.Items[0].Path {
		t.Fatalf("newest item must be recognized first")
	}
	o.tick(time.Now())
	if len(order) != 2 || order[1] != a.store.Items[1].Path {
		t.Fatalf("then the older one: %v", order)
	}
}

// TestOCRGapPacesRecognition: without spacing a second tick in the same
// interval must not launch another worker.
func TestOCRGapPacesRecognition(t *testing.T) {
	a, o, _ := testOCRShelf(t)
	addClipImage(t, a, 30)
	addClipImage(t, a, 60)
	now := time.Now()
	o.interval = 2 * time.Second
	o.now = func() time.Time { return now }
	calls := 0
	o.recognize = func(string) ([]string, error) { calls++; return nil, nil }
	o.tick(now)
	o.tick(now)
	if calls != 1 {
		t.Fatalf("the gap must hold the second image back, got %d", calls)
	}
	now = now.Add(3 * time.Second)
	o.tick(now)
	if calls != 2 {
		t.Fatalf("after the gap the next image runs, got %d", calls)
	}
}

// TestOCRRecognizeErrorSessionOnly: a failed recognition is not retried
// in the same session but the item stays un-done for the next launch.
func TestOCRRecognizeErrorSessionOnly(t *testing.T) {
	a, o, _ := testOCRShelf(t)
	addClipImage(t, a, 30)
	calls := 0
	o.recognize = func(string) ([]string, error) { calls++; return nil, errors.New("vision down") }
	o.tick(time.Now())
	o.tick(time.Now())
	if calls != 1 {
		t.Fatalf("failure must not hot-retry, got %d", calls)
	}
	if a.store.Items[0].OCRDone {
		t.Fatal("a failed recognition must not be marked done")
	}
}

// TestOCRDeletedWhileRecognizing: the item is removed between start and
// finish; the result is dropped without error and without a crash.
func TestOCRDeletedWhileRecognizing(t *testing.T) {
	a, o, _ := testOCRShelf(t)
	it := addClipImage(t, a, 30)
	// dispatch is captured so the result lands after the removal.
	var pending func()
	o.dispatch = func(f func()) { pending = f }
	o.recognize = func(string) ([]string, error) { return []string{"文字"}, nil }
	o.tick(time.Now())
	if err := a.store.Remove(it.ID); err != nil {
		t.Fatal(err)
	}
	pending()
	if len(a.store.Items) != 0 {
		t.Fatal("the removed item must stay removed")
	}
	if a.statusErr {
		t.Fatalf("a dropped result is not an error: %q", a.status)
	}
}

// TestClipboardSearchHitsOCRText is the search side: an image whose
// recognized text contains the query is listed, before and after.
func TestClipboardSearchHitsOCRText(t *testing.T) {
	a, _, _ := testOCRShelf(t)
	it := addClipImage(t, a, 30)
	p := &clipboardPage{host: a}
	a.search = "退货"
	if got := p.items(); len(got) != 0 {
		t.Fatalf("unrecognized image must not match: %v", got)
	}
	if err := a.store.SetOCR(it.ID, "请支持七天无理由退货 policy ABC"); err != nil {
		t.Fatal(err)
	}
	if got := p.items(); len(got) != 1 || got[0].ID != it.ID {
		t.Fatalf("OCR text must match the search: %v", got)
	}
	a.search = "POLICY" // case-insensitive on the stored text
	if got := p.items(); len(got) != 1 {
		t.Fatalf("matching is case-insensitive: %v", got)
	}
}

// TestOCRSnippet covers hit position, ~12-rune context and ellipsis on
// both ends, including a CJK query.
func TestOCRSnippet(t *testing.T) {
	text := strings.Repeat("甲", 20) + "货架 Shelf" + strings.Repeat("乙", 20)
	before, hit, after, ok := ocrSnippet(text, "Shelf")
	if !ok || hit != "Shelf" {
		t.Fatalf("hit missed: %q %q %q", before, hit, after)
	}
	if !strings.HasPrefix(before, "…") || !strings.HasSuffix(after, "…") {
		t.Fatalf("long text must ellipsize both sides: %q / %q", before, after)
	}
	if len([]rune(before)) != 13 || len([]rune(after)) != 13 { // 12 runes + …
		t.Fatalf("context should be ~12 runes: %q / %q", before, after)
	}
	// Start of the text: no leading ellipsis.
	before, hit, after, ok = ocrSnippet("货架上的商品", "货架")
	if !ok || before != "" || hit != "货架" || !strings.HasPrefix(after, "上") {
		t.Fatalf("start hit: %q %q %q", before, hit, after)
	}
	// Case-insensitive on ASCII, unchanged for CJK.
	if _, hit, _, ok = ocrSnippet("TypeScript Notes", "types"); !ok || hit != "TypeS" {
		t.Fatalf("case-insensitive hit: %q", hit)
	}
	if _, _, _, ok = ocrSnippet("没有任何命中", "不存在"); ok {
		t.Fatal("a miss must report ok=false")
	}
	// Newlines flatten for the single-line row.
	before, hit, _, ok = ocrSnippet("第一行\n第二行 hit here", "hit")
	if !ok || !strings.HasSuffix(before, "二行 ") {
		t.Fatalf("multi-line flattening: %q %q", before, hit)
	}
}

// TestRecognizeCopyMenuItem: the entry exists only for a single image,
// copies stored results without recognizing, and for an unrecognized
// image runs the recognizer then copies.
func TestRecognizeCopyMenuItem(t *testing.T) {
	a, o, rec := testOCRShelf(t)
	it := addClipImage(t, a, 30)
	if err := a.store.SetOCR(it.ID, "已识别的文字"); err != nil {
		t.Fatal(err)
	}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	a.setTab(pageClipboard)
	view.Frame()
	if err := view.RightClick(itemTitle(it)); err != nil {
		t.Fatal(err)
	}
	menu := view.Menu()
	found := false
	for _, l := range menu {
		if l == "识别文字并复制" {
			found = true
		}
	}
	if !found {
		t.Fatalf("image row lacks the item: %v", menu)
	}
	if err := view.ChooseMenuItem("识别文字并复制"); err != nil {
		t.Fatal(err)
	}
	if rec.text != "已识别的文字" {
		t.Fatalf("stored text should be copied, got %q", rec.text)
	}
	if !strings.Contains(a.status, "已复制识别出的文字 · 6 字") {
		t.Fatalf("toast: %q", a.status)
	}
	if o.busy(it.ID) {
		t.Fatal("a stored result must not start a worker")
	}
}

// TestRecognizeCopyRunsRecognizer covers the cold path: the item is
// recognized on demand, copied, and the result persisted for next time.
func TestRecognizeCopyRunsRecognizer(t *testing.T) {
	a, o, rec := testOCRShelf(t)
	addClipImage(t, a, 30)
	calls := 0
	o.recognize = func(string) ([]string, error) { calls++; return []string{"新鲜识别 Fresh"}, nil }
	a.copyRecognizedText(a.store.Items[0])
	if calls != 1 {
		t.Fatalf("recognizer should run once, got %d", calls)
	}
	if rec.text != "新鲜识别 Fresh" {
		t.Fatalf("copied %q", rec.text)
	}
	if a.store.Items[0].OCRText != "新鲜识别 Fresh" || !a.store.Items[0].OCRDone {
		t.Fatalf("menu recognition must persist: %+v", a.store.Items[0])
	}
}

// TestRecognizeCopyNoText shows the 「没有识别到文字」 toast and copies
// nothing when the recognizer finds nothing.
func TestRecognizeCopyNoText(t *testing.T) {
	a, o, rec := testOCRShelf(t)
	addClipImage(t, a, 30)
	o.recognize = func(string) ([]string, error) { return nil, nil }
	a.copyRecognizedText(a.store.Items[0])
	if rec.text != "" {
		t.Fatalf("nothing must be copied, got %q", rec.text)
	}
	if a.status != "没有识别到文字" {
		t.Fatalf("toast: %q", a.status)
	}
}

// TestRecognizeCopyMenuOnlyForImages: text rows and multi-selections do
// not offer the entry.
func TestRecognizeCopyMenuOnlyForImages(t *testing.T) {
	a, _, _ := testOCRShelf(t)
	if err := a.store.AddClipText("只是文字", "com.example.App"); err != nil {
		t.Fatal(err)
	}
	a.setTab(pageClipboard)
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if err := view.RightClick("只是文字"); err != nil {
		t.Fatal(err)
	}
	for _, l := range view.Menu() {
		if l == "识别文字并复制" {
			t.Fatalf("a text row must not offer it: %v", view.Menu())
		}
	}
}
