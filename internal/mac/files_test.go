//go:build darwin

package mac

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// 用 Foundation 实际产生 Finder 所用的文件引用 URL，而非硬编码 inode。
func TestFinderFileReferenceResolvesToOriginalPath(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "含空格 文件.txt")
	if err := os.WriteFile(path, []byte("Shelf drag regression"), 0600); err != nil {
		t.Fatal(err)
	}
	var raw string
	withPool(func() {
		file := send(class("NSURL"), "fileURLWithPath:", uintptr(nsString(path)))
		raw = goString(send(send(file, "fileReferenceURL"), "absoluteString"))
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveFilePath(u.Path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, resolved) {
		t.Fatalf("Finder reference %q points to a different file: %q", raw, got)
	}
	content, err := os.ReadFile(got)
	if err != nil || string(content) != "Shelf drag regression" {
		t.Fatalf("resolved file content %q: %v", content, err)
	}
}
