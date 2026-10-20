//go:build darwin

package main

import (
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

// ocrIndex recognizes text in clipboard images in the background: one at
// a time, newest first, with a pause between images so the Vision model
// never competes with the panel. The result lands on the item's OCRText
// (persisted, so it runs once per image ever) and the clipboard search
// can match it. Items added before this feature existed are picked up on
// the first tick after launch; items deleted mid-recognition drop the
// result (SetOCR reports the missing id). Everything is local — the
// recognizer is the same mac.RecognizeText the converter uses.
type ocrIndex struct {
	host *shelf
	// inflight marks item IDs a worker is recognizing now (the main
	// thread owns the map; workers only report back through dispatch).
	inflight map[string]bool
	// failed holds ids recognition or its write-back already failed for
	// this session, so a broken image is not retried every tick; the next
	// launch tries again.
	failed map[string]bool
	// nextAt gates the pace: one image per interval, never back to back.
	nextAt   time.Time
	interval time.Duration
	// Seams: tests substitute a fake recognizer, a synchronous dispatch
	// and an inline worker launch; nil falls back to the real ones.
	recognize func(path string) ([]string, error)
	dispatch  func(func())
	run       func(func())
	now       func() time.Time
}

// ocrGap is the pause between background recognitions.
const ocrGap = 2 * time.Second

// ocr returns the shelf's background recognizer, nil while it is not
// registered (unit tests that never go through ready()).
func (a *shelf) ocr() *ocrIndex {
	for _, m := range a.modules {
		if o, ok := m.(*ocrIndex); ok {
			return o
		}
	}
	return nil
}

func (o *ocrIndex) nowTime() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// tick picks the newest clipboard image still waiting for recognition —
// needs() is the whole rule, so captures from before this feature existed
// are filled in the same pass — and starts one worker for it. It runs on
// the main thread from the monitoring loop like every module.
func (o *ocrIndex) tick(time.Time) {
	a := o.host
	if a.quitting || a.store == nil || o.nowTime().Before(o.nextAt) {
		return
	}
	for _, it := range a.store.Items {
		if o.needs(it) {
			o.start(it, nil)
			return
		}
	}
}

// needs reports whether the background pass should recognize the item:
// clipboard-captured images only (tray files are recognized on demand
// from the menu), not yet done, not in flight, not failed this session,
// with its managed file still there.
func (o *ocrIndex) needs(it store.Item) bool {
	return it.Kind == store.KindImage && it.Source == store.SourceClipboard &&
		!it.OCRDone && !it.Missing && it.Path != "" &&
		!o.inflight[it.ID] && !o.failed[it.ID]
}

// busy reports whether a worker is recognizing item right now; the menu
// entry stays disabled while it is.
func (o *ocrIndex) busy(id string) bool { return o.inflight[id] }

// start launches the one-at-a-time worker for item. cb (nil for the
// background pass) runs on the main thread after the write-back attempt.
func (o *ocrIndex) start(it store.Item, cb func(string, error)) {
	if o.inflight == nil {
		o.inflight = map[string]bool{}
	}
	o.inflight[it.ID] = true
	recognize := o.recognize
	if recognize == nil {
		recognize = recognizeText
	}
	dispatch := o.dispatch
	if dispatch == nil {
		dispatch = mygo.RunOnMain
	}
	// run is the worker launch; tests run it inline so the whole round
	// trip — recognize, write-back, callback — stays synchronous.
	run := o.run
	if run == nil {
		run = func(f func()) { go f() }
	}
	host := o.host
	id, path := it.ID, it.Path
	run(func() {
		// Model loading makes the first call take seconds; it must never
		// run on the main thread — hence the worker.
		if host.stop != nil {
			select {
			case <-host.stop:
				return
			default:
			}
		}
		lines, err := recognize(path)
		dispatch(func() {
			o.finish(id, lines, err)
			if cb != nil {
				text := ""
				if err == nil {
					text = joinOCRLines(lines)
				}
				cb(text, err)
			}
		})
	})
}

// finish writes the worker's result onto the item (main thread). A
// recognition error is logged and remembered for the session; an item
// that vanished while being recognized drops the result.
func (o *ocrIndex) finish(id string, lines []string, err error) {
	delete(o.inflight, id)
	o.nextAt = o.nowTime().Add(o.gap())
	if err != nil {
		log.Printf("ocr: %v", err)
		o.markFailed(id)
		return
	}
	a := o.host
	if a.quitting || a.store == nil {
		return
	}
	if err := a.store.SetOCR(id, joinOCRLines(lines)); err != nil {
		// "no item" = deleted mid-recognition, the intended drop; anything
		// else is a save failure — don't retry every tick.
		o.markFailed(id)
		if !strings.Contains(err.Error(), "no item") {
			a.report(err)
		}
		return
	}
	if a.win != nil {
		a.win.Invalidate() // a hit row's snippet appears without a repaint wait
	}
}

func (o *ocrIndex) gap() time.Duration {
	if o.interval < 0 {
		return 0 // tests: no pacing
	}
	if o.interval > 0 {
		return o.interval
	}
	return ocrGap
}

func (o *ocrIndex) markFailed(id string) {
	if o.failed == nil {
		o.failed = map[string]bool{}
	}
	o.failed[id] = true
}

// recognizeNow is the menu path: a stored result answers at once (done
// includes "no text found"), a running one reports busy, anything else
// jumps the background queue and runs now. cb is invoked on the main
// thread either way.
func (o *ocrIndex) recognizeNow(it store.Item, cb func(text string, err error)) bool {
	if it.OCRDone {
		cb(it.OCRText, nil)
		return true
	}
	if o.busy(it.ID) || it.Missing || it.Path == "" {
		return false
	}
	o.start(it, cb)
	return true
}

// joinOCRLines stores recognized lines as one text with normalized CJK
// compatibility forms (the same normalization the Word converter applies).
func joinOCRLines(lines []string) string {
	return strings.Join(normalizeLines(lines), "\n")
}

// copyRecognizedText is 「识别文字并复制」: the recognized text of one
// image goes on the clipboard (copyText, so Shelf never records its own
// copy). A recognized item answers immediately; a fresh one is
// recognized on a worker thread while the menu item is disabled.
func (a *shelf) copyRecognizedText(item store.Item) {
	o := a.ocr()
	if o == nil {
		// No registered module (a shelf built outside ready()): serve the
		// copy through a throwaway recognizer instead of failing.
		o = &ocrIndex{host: a}
	}
	o.recognizeNow(item, func(text string, err error) {
		switch {
		case err != nil:
			a.report(err)
		case text == "":
			a.toastStatus("没有识别到文字", time.Now().Add(3*time.Second), false)
		case a.copyText(text):
			a.toastStatus(fmt.Sprintf("已复制识别出的文字 · %d 字", utf8.RuneCountInString(text)),
				time.Now().Add(3*time.Second), false)
		}
	})
}

// ocrBusy backs the menu's disabled state: while a worker recognizes the
// item the entry stays grey, so it cannot be triggered twice.
func (a *shelf) ocrBusy(item store.Item) bool {
	o := a.ocr()
	return o != nil && o.busy(item.ID)
}

// ocrSnippet finds query (case-insensitive, already trimmed by the
// caller's search) inside the image's recognized text and returns the
// parts the clipboard row shows: 命中词 plus about 12 runes of context on
// each side, ellipsized at the text's ends. ok is false when the query
// does not hit (the row then shows its ordinary meta line).
func ocrSnippet(text, query string) (before, hit, after string, ok bool) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", "", "", false
	}
	// Flatten the stored newlines: the row is single-line.
	flat := strings.Join(strings.Fields(text), " ")
	// strings.ToLower maps rune by rune (no expansions), so a byte hit in
	// the lowered string converts to rune offsets usable on the original.
	low := strings.ToLower(flat)
	lq := strings.ToLower(q)
	i := strings.Index(low, lq)
	if i < 0 {
		return "", "", "", false
	}
	runes := []rune(flat)
	lo := utf8.RuneCountInString(low[:i])
	hi := lo + utf8.RuneCountInString(lq)
	from := max(0, lo-12)
	to := min(len(runes), hi+12)
	before, hit, after = string(runes[from:lo]), string(runes[lo:hi]), string(runes[hi:to])
	if from > 0 {
		before = "…" + before
	}
	if to < len(runes) {
		after += "…"
	}
	return before, hit, after, true
}

// ocrSnippetLine draws the 「图中文字：…」 line of a clipboard row whose
// recognized text matched the search: context dim, the hit in ink.
func (a *shelf) ocrSnippetLine(c *ui.Context, item store.Item) ui.Element {
	before, hit, after, _ := ocrSnippet(item.OCRText, a.search)
	return ui.RichText(c,
		ui.Span{Text: "图中文字：", Color: ink2},
		ui.Span{Text: before, Color: ink2},
		ui.Span{Text: hit, Color: ink, Weight: 600},
		ui.Span{Text: after, Color: ink2},
	).SingleLine().FontSize(11)
}
