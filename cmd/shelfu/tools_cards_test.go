//go:build darwin

package main

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	"shelf/internal/layout"
	"shelf/internal/store"
)

// TestToolCatalogShapes checks the catalog invariants the picker and the
// grid rely on: unique ids, spans in range, every entry available-gated
// or not but never nil where it must answer.
func TestToolCatalogShapes(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range toolCatalog() {
		if def.id == "" || seen[def.id] {
			t.Fatalf("bad or duplicated id %q", def.id)
		}
		seen[def.id] = true
		if def.span != 1 && def.span != 2 {
			t.Fatalf("%s: span %d", def.id, def.span)
		}
		if def.title == "" || def.icon == nil {
			t.Fatalf("%s: missing title or icon", def.id)
		}
		if def.accepts == nil || def.available == nil {
			t.Fatalf("%s: missing predicates", def.id)
		}
	}
}

// TestToolCardAccepts checks each card's file-class rule against the
// design table (§3): image actions take only images, 转 Word images or
// PDFs, 解压 archives, zip / 隔空投送 everything non-empty, widgets
// nothing.
func TestToolCardAccepts(t *testing.T) {
	want := map[string][4]bool{
		"ocr":     {true, false, false, false},
		"fit1m":   {true, false, false, false},
		"topdf":   {true, false, false, false},
		"tojpeg":  {true, false, false, false},
		"toword":  {true, true, false, false},
		"extract": {false, false, true, false},
		"resize":  {true, false, false, false},
		"zip":     {true, true, true, true},
		"airdrop": {true, true, true, true},
		"playing": {false, false, false, false},
		"awake":   {false, false, false, false},
		"recent":  {false, false, false, false},
		"clip":    {false, false, false, false},
		"usage":   {false, false, false, false},
	}
	classes := []dropClass{classImages, classPDFs, classArchives, classMixed}
	for id, expect := range want {
		def := toolCardDefFor(id)
		if def == nil {
			t.Fatalf("%s not in the catalog", id)
		}
		for i, class := range classes {
			if got := def.accepts(class); got != expect[i] {
				t.Fatalf("%s accepts %v = %v, want %v", id, class, got, expect[i])
			}
		}
	}
}

// TestToolCardDropAction checks the card→drop-zone-cell mapping: the
// conversion target a release runs (设计 §3: 规则和投放区动作格相同).
func TestToolCardDropAction(t *testing.T) {
	a := &shelf{}
	one := []string{"/tmp/a.png"}
	two := []string{"/tmp/a.png", "/tmp/b.png"}
	cases := []struct {
		id     string
		paths  []string
		target convertTarget
		air    bool
		ocr    bool
	}{
		{"ocr", one, 0, false, true},
		{"fit1m", one, toFit1M, false, false},
		{"tojpeg", one, toJPEG, false, false},
		{"resize", one, toMedium, false, false},
		{"topdf", one, toPDF, false, false},
		{"topdf", two, toMergedPDF, false, false}, // images merge past the first
		{"toword", one, toWord, false, false},
		{"extract", one, toExtract, false, false},
		{"zip", two, toZip, false, false},
		{"airdrop", one, 0, true, false},
	}
	for _, tc := range cases {
		act := a.toolCardDropAction(tc.id, tc.paths)
		if act.isAirDrop != tc.air {
			t.Fatalf("%s: airdrop flag wrong", tc.id)
		}
		if act.isOCRCopy != tc.ocr {
			t.Fatalf("%s: isOCRCopy = %v, want %v", tc.id, act.isOCRCopy, tc.ocr)
		}
		if !tc.air && !tc.ocr && act.target != tc.target {
			t.Fatalf("%s: target %v, want %v", tc.id, act.target, tc.target)
		}
	}
}

// TestToolCardTarget checks the tap conversion table.
func TestToolCardTarget(t *testing.T) {
	a := &shelf{}
	if a.toolCardTarget("topdf", 1) != toPDF || a.toolCardTarget("topdf", 3) != toMergedPDF {
		t.Fatal("topdf target wrong")
	}
	if a.toolCardTarget("resize", 1) != toMedium {
		t.Fatal("resize maps to toMedium (1600px)")
	}
	if a.toolCardTarget("airdrop", 1) != -1 || a.toolCardTarget("clip", 1) != -1 {
		t.Fatal("non-converting cards must answer -1")
	}
}

// TestToolCardTargets checks the tray-selection filter: a card applies
// only to selected items it could run on, exactly the classes a file
// drop on it takes.
func TestToolCardTargets(t *testing.T) {
	dir := t.TempDir()
	png := writeFile(t, dir, "a.png", testPNGBytes(t))
	pdf := writeFile(t, dir, "b.pdf", []byte("%PDF"))
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s}
	// A selection of one image + one PDF: the PDF card keeps only the
	// image, toWord keeps both, zip keeps both.
	for i, it := range []store.Item{
		{ID: "i1", Kind: store.KindFile, Path: png},
		{ID: "i2", Kind: store.KindFile, Path: pdf},
	} {
		s.Items = append(s.Items, it)
		_ = i
	}
	a.selection.onlyAll([]string{"i1", "i2"})
	if got := a.toolCardTargets("topdf"); len(got) != 1 || got[0].ID != "i1" {
		t.Fatalf("topdf targets = %v", got)
	}
	if got := a.toolCardTargets("toword"); len(got) != 2 {
		t.Fatalf("toword targets = %v", got)
	}
	if got := a.toolCardTargets("extract"); len(got) != 0 {
		t.Fatalf("extract targets = %v", got)
	}
	if got := a.toolCardTargets("zip"); len(got) != 2 {
		t.Fatalf("zip targets = %v", got)
	}
	// Nothing selected → nothing to run: the tap jiggles instead.
	a.selection.clear()
	if got := a.toolCardTargets("zip"); len(got) != 0 {
		t.Fatalf("empty selection produced targets: %v", got)
	}
}

// TestLayoutDefaultCards checks the persisted default names real catalog
// ids — a typo here ships a dead cell.
func TestLayoutDefaultCards(t *testing.T) {
	for _, card := range layout.Default().Cards {
		def := toolCardDefFor(card.ID)
		if def == nil {
			t.Fatalf("default card %q not in the catalog", card.ID)
		}
		if card.Span != def.span {
			t.Fatalf("default %s span %d, catalog %d", card.ID, card.Span, def.span)
		}
	}
}

// TestOCRCardTapStartsScreenOCR wires the card's tap to task C's entry:
// runToolCard("ocr") must call startScreenOCR — the capture seam runs
// and the recognized text lands on the clipboard through the shared
// finish path.
func TestOCRCardTapStartsScreenOCR(t *testing.T) {
	ocrSeams(t)
	ocrPreflight = func() bool { return true }
	ocrCapture = func(path string) error { writePixelPNG(t, path); return nil }
	ocrRecognize = func(string) ([]string, error) { return []string{"识别出的文字"}, nil }
	var copied atomic.Value
	a := &shelf{visible: false, write: recordClipFunc(func(text string) error { copied.Store(text); return nil })}
	if !a.runToolCard("ocr") {
		t.Fatal("tapping 框选文字 must answer true — it ran, there is nothing to jiggle")
	}
	got, _ := copied.Load().(string)
	if got != "识别出的文字" {
		t.Fatalf("the card tap never reached capture+copy: %q", got)
	}
}

// TestOCRCardDropCopiesText wires the card's file drop to 识别文字并复
// 制: the images' text goes to the clipboard, nothing is shelved and the
// panel stays on the tools page (design §3's 拖文件到卡片上 column).
func TestOCRCardDropCopiesText(t *testing.T) {
	ocrSeams(t)
	ocrRecognize = func(p string) ([]string, error) {
		return []string{"from " + filepath.Base(p)}, nil
	}
	var copied atomic.Value
	a := &shelf{tab: pageTools, visible: true,
		write: recordClipFunc(func(text string) error { copied.Store(text); return nil })}
	a.drop.files = []string{"/tmp/one.png", "/tmp/two.png"}
	a.runDropAction(a.toolCardDropAction("ocr", a.drop.files), fileDrop(t, a.drop.files...))
	got, _ := copied.Load().(string)
	if got != "from one.png\nfrom two.png" {
		t.Fatalf("dropped images must be recognized in order: %q", got)
	}
	if a.tab != pageTools {
		t.Fatalf("an OCR copy must not jump to the tray page: %v", a.tab)
	}
}
