//go:build darwin

package main

import (
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
		//                    images pdfs archives mixed
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
	}{
		{"fit1m", one, toFit1M, false},
		{"tojpeg", one, toJPEG, false},
		{"resize", one, toMedium, false},
		{"topdf", one, toPDF, false},
		{"topdf", two, toMergedPDF, false}, // images merge past the first
		{"toword", one, toWord, false},
		{"extract", one, toExtract, false},
		{"zip", two, toZip, false},
		{"airdrop", one, 0, true},
	}
	for _, tc := range cases {
		act := a.toolCardDropAction(tc.id, tc.paths)
		if act.isAirDrop != tc.air {
			t.Fatalf("%s: airdrop flag wrong", tc.id)
		}
		if !tc.air && act.target != tc.target {
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
