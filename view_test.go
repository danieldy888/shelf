//go:build darwin

package main

import (
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
