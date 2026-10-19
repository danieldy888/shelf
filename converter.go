//go:build darwin

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/docx"
	"shelf/internal/mac"
	"shelf/internal/store"
)

// Image conversion (DESIGN.md「图片转换」): the item menu offers 转换为 ▸
// JPEG / HEIC / PNG / PDF / Word for images, and 合并为一个 PDF for several.
// A job runs off the main thread; its outputs are written into the store's
// converted/ folder, added to the front of the tray and selected, so one
// drag takes them all. Originals are never touched.

// convertTarget is one kind of output the menu offers.
type convertTarget int

const (
	toJPEG convertTarget = iota
	toHEIC
	toPNG
	toPDF       // one PDF per image
	toWord      // recognized text + the picture, one .docx per image
	toMergedPDF // every image a page of one PDF
	toZip       // every file and folder in one .zip
	toLarge     // each image's longer side at most 2560 px
	toMedium    // … 1600 px
	toSmall     // … 800 px
)

// resizePx is the longer-side cap of a resize target, 0 for the others.
func resizePx(t convertTarget) int {
	switch t {
	case toLarge:
		return 2560
	case toMedium:
		return 1600
	case toSmall:
		return 800
	}
	return 0
}

// convertExt is the output file extension of a target. A resized image
// keeps its format (resizeExt), so resize targets have none here.
func convertExt(t convertTarget) string {
	switch t {
	case toJPEG:
		return "jpg"
	case toHEIC:
		return "heic"
	case toPNG:
		return "png"
	case toWord:
		return "docx"
	case toZip:
		return "zip"
	default:
		return "pdf"
	}
}

// resizeExt is a resized image's extension: JPEG, PNG and HEIC keep
// their format, everything else (WebP, TIFF…, which ImageIO cannot
// always write) becomes JPEG.
func resizeExt(item store.Item) string {
	switch f := imageFormatOf(item); f {
	case "jpg", "png", "heic":
		return f
	}
	return "jpg"
}

// extFormat is the encoder for an output extension.
func extFormat(ext string) mac.ImageFormat {
	switch ext {
	case "jpg":
		return mac.FormatJPEG
	case "heic":
		return mac.FormatHEIC
	}
	return mac.FormatPNG
}

// convertShowAfter keeps instant conversions (a format change takes a few
// milliseconds) from flashing an activity into the strip.
const convertShowAfter = 250 * time.Millisecond

// Seams: tests run jobs synchronously against fakes.
var (
	convertImage    = mac.ConvertImage
	resizeImage     = mac.ResizeImage
	imagesToPDF     = mac.ImagesToPDF
	recognizeText   = mac.RecognizeText
	imageSize       = mac.ImageSize
	pdfPageCount    = mac.PDFPageCount
	pdfText         = mac.PDFText
	renderPDFPage   = mac.RenderPDFPage
	writeDocx       = docx.Write
	writeDocxPages  = docx.WritePages
	convertWork     = func(fn func()) { go fn() }
	convertDispatch = mygo.RunOnMain
)

// imageExts are the file extensions the converter accepts as images:
// what ImageIO reads on every supported macOS.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".heif": true,
	".webp": true, ".tif": true, ".tiff": true, ".gif": true, ".bmp": true, ".avif": true,
}

// convertible reports whether item is an image the converter can read: a
// screenshot or clipboard image, or an image file that is still there.
func convertible(item store.Item) bool {
	switch {
	case item.Missing:
		return false
	case item.Kind == store.KindImage:
		return true
	case item.Kind == store.KindFile:
		return imageExts[strings.ToLower(filepath.Ext(item.Path))]
	}
	return false
}

// isPDF reports whether item is a PDF file that is still there; PDFs
// convert to Word (their text) and to images (one per page).
func isPDF(item store.Item) bool {
	return item.Kind == store.KindFile && !item.Missing && strings.EqualFold(filepath.Ext(item.Path), ".pdf")
}

// imageFormatOf names an image item's format as an output extension
// ("png", "jpg", "heic"), so the menu can grey out 「当前格式」.
func imageFormatOf(item store.Item) string {
	if item.Kind == store.KindImage {
		return "png"
	}
	switch ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(item.Path), ".")); ext {
	case "jpeg":
		return "jpg"
	case "heif":
		return "heic"
	default:
		return ext
	}
}

// convertBaseName is the output's file name without extension: the
// item's own name, or the screenshot title with the clock's colons (which
// Finder shows as slashes) turned into dots.
func convertBaseName(item store.Item) string {
	if item.Kind == store.KindImage {
		return strings.ReplaceAll(itemTitle(item), ":", ".")
	}
	base := filepath.Base(item.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// convertJob is one run of the converter. total, done and the step flags
// are written by the worker and read by the main thread's drawing, hence
// atomics; target and unit never change.
type convertJob struct {
	target  convertTarget
	unit    string // what the step counts: 张 (images), 页 (PDF pages), 个 (PDFs), 个文件 (archive entries)
	total   atomic.Int32
	done    atomic.Int32
	ocr     atomic.Bool // the current step is text recognition
	cancel  atomic.Bool
	started time.Time
}

// errConvertCanceled stops a multi-page step when the user cancels; its
// output is discarded, not reported as a failure.
var errConvertCanceled = errors.New("canceled")

// converter is the module that runs conversion jobs and shows the running
// one in the activity strip. One job at a time.
type converter struct {
	host *shelf
	job  *convertJob
	seen bool // the running job has been drawn in the strip
}

func (m *converter) busy() bool { return m.job != nil }

// tick redraws once a job has run long enough to show, and keeps the
// strip's progress moving while it does.
func (m *converter) tick(time.Time) {
	if m.live() && m.host.win != nil {
		m.host.win.Invalidate()
	}
}

func (m *converter) live() bool {
	return m.job != nil && time.Since(m.job.started) >= convertShowAfter
}

// activity draws the running job: a progress ring around a document
// glyph, 「正在转成 Word」 and its step, and × to cancel (finished outputs
// are kept).
func (m *converter) activity(c *ui.Context, compact bool) {
	j := m.job
	if j == nil {
		return
	}
	m.seen = true
	done, total := int(j.done.Load()), max(int(j.total.Load()), 1)
	frac := float32(done) / float32(total)
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
	}
	step := fmt.Sprintf("第 %d / %d %s", min(done+1, total), total, j.unit)
	if j.ocr.Load() {
		step = fmt.Sprintf("识别文字 %d / %d", min(done+1, total), total)
	}
	if j.cancel.Load() {
		step = "正在取消…"
	}
	row := ui.Row(c.Key("converting")).Height(liveH).Grow(1).MinWidth(0).Radius(14).Background(fill1).
		Padding(0, 10, 0, 10).Gap(10).AlignItems(ui.Center)
	row.Children(func() {
		ring := ui.Row(c.Key("conv-ring")).Size(36, 36).Center()
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			cx, cy, rad := r.X+r.W/2, r.Y+r.H/2, r.W/2-2
			var track ui.Path
			track.Circle(cx, cy, rad)
			p.StrokePath(&track, 2.5, fill3)
			if frac > 0 {
				var arc ui.Path
				arc.Circle(cx, cy, rad).Trim(0, frac)
				p.StrokePath(&arc, 2.5, ink)
			}
		})
		ring.Children(func() { ui.Icon(c, fileSymbol).Size(16, 16).TextColor(ink) })
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Justify(ui.Center).Children(func() {
			ui.Text(c.Key("conv-title"), title).SingleLine().FontSize(13).FontWeight(600).TextColor(ink)
			ui.Text(c.Key("conv-step"), step).SingleLine().FontSize(11).TextColor(ink2)
		})
		b := ui.ButtonBase(c.Key("conv-cancel")).Size(24, 24).Radius(12).Center().Background(fill2).Label("取消转换")
		b.Children(func() { ui.Icon(c, closeSymbol).Size(10, 10).TextColor(ink2) })
		if b.Clicked() {
			j.cancel.Store(true)
		}
	})
	_ = compact // the strip's two-up layout already fits: one control only
}

// convertOut is one finished output (or failure) of a job.
type convertOut struct {
	path          string
	err           error
	noText        bool  // a Word output for which nothing was recognized
	before, after int64 // a resized image's source and output sizes
}

// convertTask is one output of a job: src converted to dst (page > 0:
// render that page of the PDF src); err is a failure found while planning.
type convertTask struct {
	src, dst string
	pdf      bool
	page     int
	err      error
}

// startConvert begins converting items — all images or all PDFs, or for
// toZip any files, folders and pictures — to target. Everything that
// touches the files, counting PDF pages included, runs on the worker so
// a large PDF never stalls the panel; outputs are named there too, which
// is safe because one job runs at a time.
func (a *shelf) startConvert(items []store.Item, target convertTarget) {
	if a.conv == nil || a.conv.busy() || a.store == nil || len(items) == 0 {
		return
	}
	dir := a.store.ConvertedDir()
	// Outputs are written straight into converted/; never into whatever a
	// symlink or stray file there points at.
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		a.report(fmt.Errorf("无法使用转换文件夹 %s", dir))
		return
	}
	srcs := make([]string, len(items))
	names := make([]string, len(items))
	exts := make([]string, len(items))
	px := resizePx(target)
	for i, it := range items {
		srcs[i], names[i], exts[i] = it.Path, convertBaseName(it), convertExt(target)
		if px > 0 {
			names[i] = fmt.Sprintf("%s %dpx", names[i], px)
			exts[i] = resizeExt(it)
		}
	}
	var roots []string
	if target == toZip {
		// Finder-like: 「报告.pdf.zip」, 「my.project 等 3 项.zip」 — a
		// file keeps its extension in the archive's name.
		roots = zipRoots(items)
		names[0] = roots[0]
		if len(items) > 1 {
			first := filepath.Base(items[0].Path)
			if items[0].Kind == store.KindImage {
				first = convertBaseName(items[0])
			}
			names[0] = fmt.Sprintf("%s 等 %d 项", first, len(items))
		}
	}
	pdf := isPDF(items[0]) && target != toZip
	unit := "张"
	switch {
	case target == toZip:
		unit = "个文件"
	case pdf && target == toWord:
		unit = "个"
	case pdf:
		unit = "页"
	}
	job := &convertJob{target: target, unit: unit, started: time.Now()}
	job.total.Store(int32(len(items)))
	a.conv.job, a.conv.seen = job, false
	convertWork(func() {
		tasks := planConvert(dir, srcs, names, exts, pdf, target, job)
		if target != toZip {
			job.total.Store(int32(len(tasks)))
		}
		format := mac.FormatPNG
		switch target {
		case toJPEG:
			format = mac.FormatJPEG
		case toHEIC:
			format = mac.FormatHEIC
		}
		var outs []convertOut
		for _, t := range tasks {
			if job.cancel.Load() {
				break
			}
			job.ocr.Store(false)
			out := convertOut{path: t.dst, err: t.err}
			switch {
			case out.err != nil:
			case target == toZip:
				out.err = writeZip(t.dst, srcs, roots, job)
			case target == toMergedPDF:
				out.err = imagesToPDF(srcs, t.dst)
			case t.pdf && target == toWord:
				out.noText, out.err = wordFromPDF(t.src, t.dst, job)
			case t.pdf:
				out.err = renderPDFPage(t.src, t.page, t.dst, format)
			case target == toPDF:
				out.err = imagesToPDF([]string{t.src}, t.dst)
			case target == toWord:
				job.ocr.Store(true)
				out.noText, out.err = wordFromImage(t.src, t.dst)
			case px > 0:
				out.err = resizeImage(t.src, t.dst, extFormat(strings.TrimPrefix(filepath.Ext(t.dst), ".")), px)
				out.before, out.after = fileSize(t.src), fileSize(t.dst)
			default:
				out.err = convertImage(t.src, t.dst, format)
			}
			if out.err != nil && t.err == nil {
				_ = os.Remove(t.dst)
			}
			if errors.Is(out.err, errConvertCanceled) {
				break
			}
			outs = append(outs, out)
			if target != toZip { // the archive counts its own entries
				job.done.Add(1)
			}
		}
		convertDispatch(func() { a.finishConvert(job, outs) })
	})
}

// fileSize is p's size in bytes, 0 when it cannot be read.
func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// formatBytes renders a size the way Finder does (decimal units): 「812 KB」,
// 「4.2 MB」.
func formatBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d 字节", n)
}

// zipRoots names each item at an archive's root: a file or folder by its
// own name, a screenshot by its title as a .png; a name already taken
// becomes 「名 2.ext」.
func zipRoots(items []store.Item) []string {
	roots := make([]string, len(items))
	taken := map[string]bool{}
	for i, it := range items {
		name := filepath.Base(it.Path)
		if it.Kind == store.KindImage {
			name = convertBaseName(it) + ".png"
		}
		stem, ext := name, filepath.Ext(name)
		if ext != name {
			stem = strings.TrimSuffix(name, ext)
		} else {
			ext = ""
		}
		candidate := name
		for n := 2; taken[strings.ToLower(candidate)]; n++ {
			candidate = fmt.Sprintf("%s %d%s", stem, n, ext)
		}
		taken[strings.ToLower(candidate)] = true
		roots[i] = candidate
	}
	return roots
}

// planConvert lists a job's outputs with their reserved names: one per
// source (named names[i].exts[i]), one merged PDF or archive, or for a
// PDF turned into images one per page (「名 第 2 页.png」, just 「名.png」
// for a one-page PDF). A PDF whose pages cannot be counted becomes a
// failed task. Counting stops when the job is cancelled.
func planConvert(dir string, srcs, names, exts []string, pdf bool, target convertTarget, job *convertJob) []convertTask {
	taken := map[string]bool{}
	var tasks []convertTask
	switch {
	case target == toZip:
		return []convertTask{{dst: uniquePath(dir, names[0], "zip", taken)}}
	case target == toMergedPDF:
		name := names[0]
		if len(srcs) > 1 {
			name = fmt.Sprintf("%s 等 %d 张", name, len(srcs))
		}
		return []convertTask{{dst: uniquePath(dir, name, "pdf", taken)}}
	case pdf && target == toWord:
		for i, src := range srcs {
			tasks = append(tasks, convertTask{src: src, pdf: true, dst: uniquePath(dir, names[i], "docx", taken)})
		}
	case pdf:
		for i, src := range srcs {
			if job.cancel.Load() {
				break
			}
			n, err := pdfPageCount(src)
			if err == nil && n < 1 {
				err = fmt.Errorf("%s 没有页面", filepath.Base(src))
			}
			if err != nil {
				tasks = append(tasks, convertTask{src: src, pdf: true, dst: filepath.Join(dir, names[i]), err: err})
				continue
			}
			for p := 1; p <= n; p++ {
				name := names[i]
				if n > 1 {
					name = fmt.Sprintf("%s 第 %d 页", names[i], p)
				}
				tasks = append(tasks, convertTask{src: src, pdf: true, page: p, dst: uniquePath(dir, name, convertExt(target), taken)})
			}
		}
	default:
		for i, src := range srcs {
			tasks = append(tasks, convertTask{src: src, dst: uniquePath(dir, names[i], exts[i], taken)})
		}
	}
	return tasks
}

// wordFromPDF writes dst as a text-only Word document with one page per
// page of the PDF src: the page's own text, or for a scanned page without
// a text layer, the text recognized in its picture. Cancelling stops it
// between pages with errConvertCanceled. It reports whether no page had
// any text.
func wordFromPDF(src, dst string, job *convertJob) (noText bool, err error) {
	pages, err := pdfText(src)
	if err != nil {
		return false, err
	}
	noText = true
	for i, lines := range pages {
		if job.cancel.Load() {
			return false, errConvertCanceled
		}
		if len(lines) == 0 {
			job.ocr.Store(true)
			tmp := filepath.Join(filepath.Dir(dst), fmt.Sprintf(".%s.page%d.png", filepath.Base(dst), i+1))
			err := renderPDFPage(src, i+1, tmp, mac.FormatPNG)
			if err == nil {
				lines, err = recognizeText(tmp)
			}
			os.Remove(tmp)
			job.ocr.Store(false)
			if err != nil {
				return false, err
			}
			pages[i] = normalizeLines(lines)
		}
		if len(pages[i]) > 0 {
			noText = false
		}
	}
	if job.cancel.Load() {
		return false, errConvertCanceled
	}
	return noText, writeDocxPages(dst, pages)
}

// normalizeLines maps recognized lines to ordinary characters (Vision can
// return CJK compatibility forms such as 「⼯」 for 「工」).
func normalizeLines(lines []string) []string {
	for i, l := range lines {
		lines[i] = mac.NormalizeText(l)
	}
	return lines
}

// wordFromImage writes dst as a Word document holding the text recognized
// in src and, below it, the picture itself (upright, as PNG). It reports
// whether no text was found.
func wordFromImage(src, dst string) (noText bool, err error) {
	lines, err := recognizeText(src)
	if err != nil {
		return false, err
	}
	lines = normalizeLines(lines)
	w, h, err := imageSize(src)
	if err != nil {
		return false, err
	}
	// The picture goes in as an upright PNG: Word does not reliably honour
	// a JPEG's orientation tag, and HEIC/WebP cannot be embedded at all.
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".png")
	defer os.Remove(tmp)
	if err := convertImage(src, tmp, mac.FormatPNG); err != nil {
		return false, err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return false, err
	}
	return len(lines) == 0, writeDocx(dst, lines, &docx.Picture{Data: data, Ext: "png", W: w, H: h})
}

// finishConvert adds a job's outputs to the front of the tray, selects
// them and says how it went.
func (a *shelf) finishConvert(job *convertJob, outs []convertOut) {
	if a.conv != nil && a.conv.job == job {
		a.conv.job = nil
	}
	if a.quitting {
		return
	}
	var paths []string
	failed, noText := 0, 0
	var firstErr error
	for _, o := range outs {
		if o.err != nil {
			failed++
			if firstErr == nil {
				firstErr = o.err
			}
			log.Printf("Shelf: 转换失败 %s：%v", filepath.Base(o.path), o.err)
			continue
		}
		if o.noText {
			noText++
		}
		paths = append(paths, o.path)
	}
	if len(paths) > 0 {
		ids, err := a.store.AddConverted(paths)
		if err != nil {
			for _, p := range paths {
				_ = os.Remove(p)
			}
			a.report(err)
			return
		}
		if a.tab != pageTray {
			a.setTab(pageTray)
		}
		a.selection.onlyAll(ids)
	}
	verb := "转换"
	if job.target == toZip {
		verb = "压缩"
	}
	switch {
	case len(paths) == 0 && failed > 0:
		a.report(fmt.Errorf("%s失败：%v", verb, firstErr))
	case failed > 0:
		a.toastStatus(fmt.Sprintf("%d 项成功，%d 项失败", len(paths), failed), time.Now().Add(4*time.Second), true)
	case job.cancel.Load():
		a.toastStatus(fmt.Sprintf("已取消 · 已完成 %d 项", len(paths)), time.Now().Add(3*time.Second), false)
	case noText > 0 && noText == len(paths) && job.unit == "个":
		a.toastStatus("没有找到文字，Word 文件是空白的", time.Now().Add(4*time.Second), false)
	case noText > 0 && noText == len(paths):
		a.toastStatus("没有识别到文字，只放入了图片", time.Now().Add(4*time.Second), false)
	case job.target == toZip:
		a.toastStatus(fmt.Sprintf("已压缩 · %s · 已选中，可直接拖出", formatBytes(fileSize(paths[0]))), time.Now().Add(3*time.Second), false)
	case resizePx(job.target) > 0:
		var before, after int64
		for _, o := range outs {
			before, after = before+o.before, after+o.after
		}
		a.toastStatus(fmt.Sprintf("已缩小 %d 张 · %s → %s", len(paths), formatBytes(before), formatBytes(after)), time.Now().Add(4*time.Second), false)
	case len(paths) > 0:
		a.toastStatus(fmt.Sprintf("已转换 %d 项 · 已选中，可直接拖出", len(paths)), time.Now().Add(3*time.Second), false)
	}
	a.syncWindowFrame()
	if a.win != nil {
		a.win.Invalidate()
	}
}

// uniquePath is dir/name.ext, or 「name 2.ext」, 「name 3.ext」… when that
// file exists or another output of the same job took it.
func uniquePath(dir, name, ext string, taken map[string]bool) string {
	for n := 1; ; n++ {
		candidate := name
		if n > 1 {
			candidate = fmt.Sprintf("%s %d", name, n)
		}
		p := filepath.Join(dir, candidate+"."+ext)
		if taken[p] {
			continue
		}
		if _, err := os.Lstat(p); err == nil {
			continue
		}
		taken[p] = true
		return p
	}
}

// convertMenu adds the conversion group to an item menu. Every target an
// image, or every target a PDF — a mixed selection is never half
// converted: images get 「转换为 ▸」 (「转换 N 张为 ▸」) JPEG / HEIC / PNG /
// PDF / Word, the format every target already has greyed as 「当前格式」,
// for several 「合并为一个 PDF」, and 「缩小图片 ▸」 by longer side, a size
// no target exceeds greyed; PDFs get 「转换为 ▸」 (「转换 N 个为 ▸」) Word,
// PNG, JPEG. Any files, folders and pictures still on disk get 「压缩为
// zip」. Everything is disabled while a job runs.
func (a *shelf) convertMenu(m *ui.Menu, items []store.Item) {
	if a.conv == nil || a.store == nil || len(items) == 0 {
		return
	}
	allImages, allPDFs, allFiles := true, true, true
	for _, it := range items {
		allImages = allImages && convertible(it)
		allPDFs = allPDFs && isPDF(it)
		allFiles = allFiles && !it.Missing && (it.Kind == store.KindFile || it.Kind == store.KindImage)
	}
	if !allFiles {
		return
	}
	busy := a.conv.busy()
	m.Separator()
	switch {
	case allPDFs:
		label := "转换为"
		if len(items) > 1 {
			label = fmt.Sprintf("转换 %d 个为", len(items))
		}
		m.Submenu(label, func(sm *ui.Menu) {
			if sm.Item("Word（提取文字）").Disabled(busy).Chosen() {
				a.startConvert(items, toWord)
			}
			sm.Separator()
			if sm.Item("PNG（每页一张）").Disabled(busy).Chosen() {
				a.startConvert(items, toPNG)
			}
			if sm.Item("JPEG（每页一张）").Disabled(busy).Chosen() {
				a.startConvert(items, toJPEG)
			}
		}).Disabled(busy)
	case allImages:
		a.imageConvertMenu(m, items, busy)
	}
	label := "压缩为 zip"
	if len(items) > 1 {
		label = fmt.Sprintf("压缩 %d 项为 zip", len(items))
	}
	if m.Item(label).Disabled(busy).Chosen() {
		a.startConvert(items, toZip)
	}
}

// imageConvertMenu is convertMenu's group for pictures.
func (a *shelf) imageConvertMenu(m *ui.Menu, items []store.Item, busy bool) {
	current := imageFormatOf(items[0])
	for _, it := range items[1:] {
		if imageFormatOf(it) != current {
			current = ""
		}
	}
	label := "转换为"
	if len(items) > 1 {
		label = fmt.Sprintf("转换 %d 张为", len(items))
	}
	m.Submenu(label, func(sm *ui.Menu) {
		for _, f := range []struct {
			t     convertTarget
			label string
		}{{toJPEG, "JPEG"}, {toHEIC, "HEIC"}, {toPNG, "PNG"}} {
			l, same := f.label, convertExt(f.t) == current
			if same {
				l += "（当前格式）"
			}
			if sm.Item(l).Disabled(busy || same).Chosen() {
				a.startConvert(items, f.t)
			}
		}
		sm.Separator()
		if sm.Item("PDF").Disabled(busy).Chosen() {
			a.startConvert(items, toPDF)
		}
		if sm.Item("Word（识别文字）").Disabled(busy).Chosen() {
			a.startConvert(items, toWord)
		}
	}).Disabled(busy)
	if len(items) > 1 {
		if m.Item("合并为一个 PDF").Disabled(busy).Chosen() {
			a.startConvert(items, toMergedPDF)
		}
	}
	// The longest side among the targets: a size it does not exceed would
	// only re-encode, so that choice is greyed.
	longest := 0
	for _, it := range items {
		if w, h, err := imageSize(it.Path); err == nil {
			longest = max(longest, w, h)
		}
	}
	label = "缩小图片"
	if len(items) > 1 {
		label = fmt.Sprintf("缩小 %d 张图片", len(items))
	}
	m.Submenu(label, func(sm *ui.Menu) {
		for _, f := range []struct {
			t     convertTarget
			label string
		}{{toLarge, "大"}, {toMedium, "中"}, {toSmall, "小"}} {
			px := resizePx(f.t)
			l := fmt.Sprintf("%s（长边 %d 像素）", f.label, px)
			if sm.Item(l).Disabled(busy || longest <= px).Chosen() {
				a.startConvert(items, f.t)
			}
		}
	}).Disabled(busy || longest <= resizePx(toSmall))
}
