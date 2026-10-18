//go:build darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"shelf/internal/docx"
	"shelf/internal/mac"
	"shelf/internal/store"
)

// convertFakes replaces the native converters with fakes that write a
// small marker file, and runs jobs synchronously.
type convertFakes struct {
	formats  []mac.ImageFormat
	pdfSrcs  [][]string
	docText  [][]string
	lines    []string
	failFor  string // convertImage fails for this source
	onConver func()
}

func fakeConverters(t *testing.T) *convertFakes {
	t.Helper()
	f := &convertFakes{}
	origs := []any{convertImage, imagesToPDF, recognizeText, imageSize, writeDocx, convertWork, convertDispatch}
	convertImage = func(src, dst string, format mac.ImageFormat) error {
		if f.onConver != nil {
			f.onConver()
		}
		if src == f.failFor {
			return errors.New("cannot read")
		}
		f.formats = append(f.formats, format)
		return os.WriteFile(dst, []byte("img"), 0o600)
	}
	imagesToPDF = func(srcs []string, dst string) error {
		f.pdfSrcs = append(f.pdfSrcs, srcs)
		return os.WriteFile(dst, []byte("%PDF"), 0o600)
	}
	recognizeText = func(string) ([]string, error) { return f.lines, nil }
	imageSize = func(string) (int, int, error) { return 40, 20, nil }
	writeDocx = func(dst string, paragraphs []string, pic *docx.Picture) error {
		f.docText = append(f.docText, paragraphs)
		if pic == nil || len(pic.Data) == 0 {
			return errors.New("no picture")
		}
		return os.WriteFile(dst, []byte("docx"), 0o600)
	}
	convertWork = func(fn func()) { fn() }
	convertDispatch = func(fn func()) { fn() }
	t.Cleanup(func() {
		convertImage = origs[0].(func(string, string, mac.ImageFormat) error)
		imagesToPDF = origs[1].(func([]string, string) error)
		recognizeText = origs[2].(func(string) ([]string, error))
		imageSize = origs[3].(func(string) (int, int, error))
		writeDocx = origs[4].(func(string, []string, *docx.Picture) error)
		convertWork = origs[5].(func(func()))
		convertDispatch = origs[6].(func(func()))
	})
	return f
}

// convertShelf is a headless shelf whose tray holds the named files (in
// the given order, first frontmost).
func convertShelf(t *testing.T, names ...string) (*shelf, *ui.Tester, []string) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dir := t.TempDir()
	var paths []string
	for i := len(names) - 1; i >= 0; i-- {
		p := filepath.Join(dir, names[i])
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := s.AddFiles([]string{p}); err != nil {
			t.Fatal(err)
		}
		paths = append([]string{p}, paths...)
	}
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	a.conv = &converter{host: a}
	a.modules = []module{a.conv}
	return a, ui.NewTester(a.view, panelWidth, panelHeight), paths
}

func selectAll(a *shelf) {
	var ids []string
	for _, it := range a.visibleItems() {
		ids = append(ids, it.ID)
	}
	a.selection.onlyAll(ids)
}

func outputNames(a *shelf) []string {
	var out []string
	for _, it := range a.store.Items {
		if it.Converted {
			out = append(out, filepath.Base(it.Path))
		}
	}
	return out
}

// Conversion is offered for images only, and never for a mixed selection
// (which would convert only part of what the user picked).
func TestConvertMenuOnlyForImages(t *testing.T) {
	fakeConverters(t)
	a, view, _ := convertShelf(t, "a.png", "note.txt")
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(view.Menu(), "转换为") {
		t.Fatalf("an image's menu lacks 转换为: %v", view.Menu())
	}
	view.CloseMenu()
	if err := view.RightClick("note.txt"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(view.Menu(), "转换为") {
		t.Fatalf("a text file must not offer conversion: %v", view.Menu())
	}
	view.CloseMenu()
	selectAll(a)
	view.Frame()
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	for _, l := range view.Menu() {
		if strings.HasPrefix(l, "转换") || l == "合并为一个 PDF" {
			t.Fatalf("a mixed selection must not offer conversion: %v", view.Menu())
		}
	}
}

// Converting several images writes one output each, puts them at the
// front of the tray in display order, selects them and says so; the
// format they already have cannot be chosen.
func TestConvertSelectedImagesToJPEG(t *testing.T) {
	f := fakeConverters(t)
	a, view, paths := convertShelf(t, "a.png", "b.png")
	selectAll(a)
	view.Frame()
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换 2 张为", "PNG（当前格式）"); err == nil && len(outputNames(a)) > 0 {
		t.Fatal("converting to the format the images already have must not run")
	}
	view.CloseMenu()
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换 2 张为", "JPEG"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"a.jpg", "b.jpg"}) {
		t.Fatalf("outputs = %v, want a.jpg, b.jpg at the front in order", got)
	}
	if !a.store.Items[0].Converted || !a.store.Items[1].Converted {
		t.Fatal("outputs must lead the tray")
	}
	if a.selection.count() != 2 || !a.selection.has(a.store.Items[0].ID) || !a.selection.has(a.store.Items[1].ID) {
		t.Fatal("the outputs must be selected so one drag takes them")
	}
	if !slices.Equal(f.formats, []mac.ImageFormat{mac.FormatJPEG, mac.FormatJPEG}) {
		t.Fatalf("formats = %v", f.formats)
	}
	if !strings.HasPrefix(a.status, "已转换 2 项") {
		t.Fatalf("status = %q", a.status)
	}
	for _, p := range paths {
		if b, err := os.ReadFile(p); err != nil || string(b) != "x" {
			t.Fatalf("original %s touched: %q %v", p, b, err)
		}
	}
	if a.conv.busy() {
		t.Fatal("the job must be over")
	}
}

// Merging makes one PDF whose pages follow the tray's order, named after
// the first image.
func TestMergeImagesIntoOnePDF(t *testing.T) {
	f := fakeConverters(t)
	a, view, paths := convertShelf(t, "a.png", "b.jpg", "c.heic")
	selectAll(a)
	view.Frame()
	if err := view.RightClick("b.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("合并为一个 PDF"); err != nil {
		t.Fatal(err)
	}
	if len(f.pdfSrcs) != 1 || !slices.Equal(f.pdfSrcs[0], paths) {
		t.Fatalf("pages = %v, want %v", f.pdfSrcs, paths)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"a 等 3 张.pdf"}) {
		t.Fatalf("outputs = %v", got)
	}
}

// Word gets the recognized lines and the picture; with nothing recognized
// the document still holds the picture and the user is told.
func TestConvertToWord(t *testing.T) {
	f := fakeConverters(t)
	f.lines = []string{"周五下午三点开会", "4B 会议室"}
	a, view, _ := convertShelf(t, "board.png")
	if err := view.RightClick("board.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换为", "Word（识别文字）"); err != nil {
		t.Fatal(err)
	}
	if len(f.docText) != 1 || !slices.Equal(f.docText[0], f.lines) {
		t.Fatalf("document text = %v", f.docText)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"board.docx"}) {
		t.Fatalf("outputs = %v", got)
	}
	f.lines = nil
	view.Frame()
	if err := view.RightClick("board.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换为", "Word（识别文字）"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Contains(got, "board 2.docx") {
		t.Fatalf("a second output must not overwrite the first: %v", got)
	}
	if a.status != "没有识别到文字，只放入了图片" {
		t.Fatalf("status = %q", a.status)
	}
	if entries, _ := os.ReadDir(a.store.ConvertedDir()); len(entries) != 2 {
		t.Fatalf("the temporary picture must not stay behind: %v", entries)
	}
}

// One image failing does not lose the others, and the summary says so.
func TestConvertFailureKeepsTheRest(t *testing.T) {
	f := fakeConverters(t)
	a, view, paths := convertShelf(t, "a.png", "b.png")
	f.failFor = paths[0]
	selectAll(a)
	view.Frame()
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换 2 张为", "HEIC"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"b.heic"}) {
		t.Fatalf("outputs = %v", got)
	}
	if a.status != "1 项成功，1 项失败" || !a.statusErr {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
	if entries, _ := os.ReadDir(a.store.ConvertedDir()); len(entries) != 1 {
		t.Fatalf("a failed output must not stay on disk: %v", entries)
	}
}

// Cancelling keeps what is already done and stops the rest.
func TestConvertCancelKeepsFinished(t *testing.T) {
	f := fakeConverters(t)
	a, view, _ := convertShelf(t, "a.png", "b.png", "c.png")
	calls := 0
	f.onConver = func() {
		calls++
		if calls == 1 {
			a.conv.job.cancel.Store(true)
		}
	}
	selectAll(a)
	view.Frame()
	if err := view.RightClick("a.png"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("转换 3 张为", "JPEG"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"a.jpg"}) {
		t.Fatalf("outputs = %v, want only the one finished before cancelling", got)
	}
	if !strings.HasPrefix(a.status, "已取消") {
		t.Fatalf("status = %q", a.status)
	}
}

// A clipboard screenshot converts too; its output is named after the
// screenshot without the clock's colons, and the tray comes forward.
func TestConvertClipboardScreenshot(t *testing.T) {
	fakeConverters(t)
	a, _, _ := convertShelf(t)
	if err := a.store.AddClipImage(testPNG(t), "com.apple.screencaptureui"); err != nil {
		t.Fatal(err)
	}
	a.setTab(tabClipboard)
	shot := a.store.Items[0]
	a.startConvert([]store.Item{shot}, toPDF)
	if a.tab != tabTray {
		t.Fatal("the outputs live on the tray, so the tray must come forward")
	}
	got := outputNames(a)
	if len(got) != 1 || strings.Contains(got[0], ":") || !strings.HasPrefix(got[0], "截图 ") || !strings.HasSuffix(got[0], ".pdf") {
		t.Fatalf("outputs = %v", got)
	}
}
