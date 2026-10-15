//go:build darwin

package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"shelf/internal/store"
)

func TestLongTextPreviewKeepsFullPayload(t *testing.T) {
	text := "开头 😀\n\t" + strings.Repeat("正文🙂 ", 400) + "不可丢失的末尾"
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AddText(text); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("开头 😀") {
		t.Fatal("text preview lost its readable beginning")
	}
	for _, label := range view.Texts() {
		if !utf8.ValidString(label) {
			t.Fatal("text preview split a UTF-8 character")
		}
		if utf8.RuneCountInString(label) > 200 {
			t.Fatal("long clipboard text was exposed to layout instead of a bounded preview")
		}
		if strings.Contains(label, "不可丢失的末尾") {
			t.Fatal("preview rendered the distant tail of the full text")
		}
	}
	payload, err := a.dragData(s.Items[0]).Read(transfer.Text)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != text || s.Items[0].Text != text {
		t.Fatal("shortening the preview truncated the stored or exported text")
	}
}

// 有内容时头部出现"清空"，每项仍保留独立移除按钮；单项移除只删该项，
// 清空则删除全部、丢掉缩略图缓存、回到空态且不再显示"清空"。
func TestClearAllButtonAndPerItemRemove(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{note}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddText("一段文字"); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := s.AddImage(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	a := &shelf{store: s, thumbnails: map[string]*ui.Bitmap{}, thumbPending: map[string]bool{}}
	view := ui.NewTester(a.view, panelWidth, panelHeight)
	if !view.HasText("清空") {
		t.Fatal("clear-all button missing with items present")
	}
	for _, it := range s.Items {
		label := "移除 " + itemTitle(it)
		if !view.HasText(label) {
			t.Fatalf("per-item remove button %q missing", label)
		}
	}
	// 缩略图在途解码与缓存命中都必须在清空时失效。
	if len(a.thumbPending) == 0 {
		t.Fatal("expected in-flight thumbnail decodes for file and image items")
	}
	a.thumbnails["cached"] = &ui.Bitmap{}

	if err := view.Click("移除 note.txt"); err != nil {
		t.Fatalf("click per-item remove: %v", err)
	}
	if len(s.Items) != 2 {
		t.Fatalf("per-item remove should keep the rest, got %d items", len(s.Items))
	}
	for _, it := range s.Items {
		if it.Kind == store.KindFile {
			t.Fatal("per-item remove deleted the wrong item")
		}
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by remove: %v", err)
	}
	if !view.HasText("清空") {
		t.Fatal("clear-all button disappeared while items remain")
	}

	if err := view.Click("清空"); err != nil {
		t.Fatalf("click clear-all: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatalf("clear-all left %d items", len(s.Items))
	}
	if !view.HasText("把文件拖到这里") {
		t.Fatal("empty state not shown after clear-all")
	}
	if view.HasText("清空") {
		t.Fatal("clear-all button still rendered on empty shelf")
	}
	if len(a.thumbnails) != 0 {
		t.Fatal("cached thumbnails not invalidated by clear-all")
	}
	for id := range a.thumbPending {
		if !a.thumbDiscard[id] {
			t.Fatalf("in-flight thumbnail %s not marked for discard", id)
		}
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("original file deleted by clear-all: %v", err)
	}
}
