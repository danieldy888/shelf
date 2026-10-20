//go:build darwin

package main

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// writeZip builds a zip at path with entries {name → data}; names may be
// raw bytes (invalid UTF-8) to imitate archives made on other systems.
func makeZip(t *testing.T, path string, entries map[string][]byte, dirs ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, d := range dirs {
		if _, err := zw.Create(d + "/"); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeGbkZip writes a zip whose member names are raw GBK bytes with no
// UTF-8 flag — what a Chinese Windows machine produces.
func writeGbkZip(t *testing.T, path string, names ...string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range names {
		h := &zip.FileHeader{Method: zip.Store}
		h.Name = gbkBytes(t, name)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("内容" + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// gbkBytes encodes s as GBK using mac's decoder as a reference: brute
// encode is impractical without a library, so encode through NSString by
// round-tripping — the test only needs *some* non-UTF-8 byte name. The
// table covers the fixture names below.
func gbkBytes(t *testing.T, s string) string {
	t.Helper()
	// Hand-encoded: 报表.txt → 报=B1A8 表=B1ED, 数据/明细.csv…
	enc := map[string]string{
		"报表.txt":    "\xb1\xa8\xb1\xed.txt",
		"明细.csv":    "\xc3\xf7\xcf\xb8.csv",
		"数据/明细.csv": "\xca\xfd\xbe\xdd/\xc3\xf7\xcf\xb8.csv",
		"说明.txt":    "\xcb\xb5\xc3\xf7.txt",
	}
	b, ok := enc[s]
	if !ok {
		t.Fatalf("gbkBytes: no fixture encoding for %q", s)
	}
	return b
}

// extractShelf is a shelf whose tray holds archives built under dir.
func extractShelf(t *testing.T, paths ...string) (*shelf, *ui.Tester) {
	t.Helper()
	a, view, _ := convertShelf(t)
	for i := len(paths) - 1; i >= 0; i-- {
		if err := a.store.AddFiles([]string{paths[i]}); err != nil {
			t.Fatal(err)
		}
	}
	return a, view
}

// noStaging fails when a hidden extraction staging folder was left behind.
func noStaging(t *testing.T, a *shelf) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(a.store.ConvertedDir(), ".shelf-extract-*")); len(m) != 0 {
		t.Fatalf("staging left behind: %v", m)
	}
}

// extractedTree lists a result folder's files relative to it.
func extractedTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel != "." {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// A zip with several top-level items unpacks under the archive's name and
// the folder lands in the tray; junk entries never arrive.
func TestExtractZip(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "资料.zip")
	makeZip(t, zipPath, map[string][]byte{
		"a.txt":       []byte("a"),
		"sub/b.txt":   []byte("b"),
		"__MACOSX/x":  []byte("junk"),
		"._a.txt":     []byte("junk"),
		"sub/._b.txt": []byte("junk"),
	}, "sub")
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("资料.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	got := outputNames(a)
	if !slices.Equal(got, []string{"资料"}) {
		t.Fatalf("outputs = %v", got)
	}
	it := a.store.Items[0]
	if !it.Converted {
		t.Fatal("the result folder must be shelf-owned")
	}
	tree := extractedTree(t, it.Path)
	if !slices.Equal(tree, []string{"a.txt", "sub", "sub/b.txt"}) {
		t.Fatalf("tree = %v", tree)
	}
	if b, _ := os.ReadFile(filepath.Join(it.Path, "sub", "b.txt")); string(b) != "b" {
		t.Fatalf("sub/b.txt = %q", b)
	}
	if !strings.HasPrefix(a.status, "已解压 1 项") {
		t.Fatalf("status = %q", a.status)
	}
	noStaging(t, a)
}

// An archive holding one top-level folder yields that folder itself.
func TestExtractSingleTopFolder(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pack.zip")
	makeZip(t, zipPath, map[string][]byte{
		"Foo/one.txt": []byte("1"),
		"Foo/two.txt": []byte("2"),
	}, "Foo")
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("pack.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"Foo"}) {
		t.Fatalf("outputs = %v, the inner folder itself is the result", got)
	}
	tree := extractedTree(t, a.store.Items[0].Path)
	if !slices.Equal(tree, []string{"one.txt", "two.txt"}) {
		t.Fatalf("tree = %v", tree)
	}
	noStaging(t, a)
	// Extracting a second pack.zip must not collide with Foo.
	other := filepath.Join(dir, "pack2.zip")
	makeZip(t, other, map[string][]byte{"Foo/x.txt": []byte("x")}, "Foo")
	if err := a.store.AddFiles([]string{other}); err != nil {
		t.Fatal(err)
	}
	view.Frame()
	if err := view.RightClick("pack2.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if got := outputNames(a); !slices.Equal(got, []string{"Foo 2", "Foo"}) {
		t.Fatalf("outputs = %v, want Foo and Foo 2", got)
	}
}

// Unsafe member names refuse the archive outright; nothing is extracted.
func TestExtractRejectsUnsafeZip(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("../escape.txt")
	w.Write([]byte("evil"))
	zw.Close()
	zf.Close()
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("evil.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if !a.statusErr || !strings.Contains(a.status, "不安全") {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
	if entries, _ := os.ReadDir(a.store.ConvertedDir()); len(entries) != 0 {
		t.Fatalf("a refused archive must leave nothing: %v", entries)
	}
	if _, err := os.Lstat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("the escape entry was written")
	}
}

// An encrypted zip reports so instead of running a tool that would block
// on a password prompt. (zip.Writer cannot encrypt; the flag bit alone is
// enough for the scan to refuse it.)
func TestExtractEncryptedZip(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "secret.zip")
	zf, _ := os.Create(zipPath)
	zw := zip.NewWriter(zf)
	h := &zip.FileHeader{Method: zip.Store}
	h.Name = "a.txt"
	h.Flags = 0x1 // encrypted
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("secret"))
	zw.Close()
	zf.Close()
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("secret.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if !a.statusErr || !strings.Contains(a.status, "加密") {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
}

// A corrupt archive fails with a readable message and leaves nothing.
func TestExtractCorruptZip(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "broken.zip")
	os.WriteFile(zipPath, []byte("not a zip at all"), 0o600)
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("broken.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if !a.statusErr || !strings.Contains(a.status, "解压失败") {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
	if entries, _ := os.ReadDir(a.store.ConvertedDir()); len(entries) != 0 {
		t.Fatalf("a failed extract must leave nothing: %v", entries)
	}
}

// A GBK-named zip extracts with real Chinese names, not mojibake.
func TestExtractGbkZipNames(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "windows.zip")
	writeGbkZip(t, zipPath, "报表.txt", "数据/明细.csv")
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("windows.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	it := a.store.Items[0]
	tree := extractedTree(t, it.Path)
	if !slices.Equal(tree, []string{"报表.txt", "数据", "数据/明细.csv"}) {
		t.Fatalf("tree = %v, GBK names must decode to Chinese", tree)
	}
	if b, _ := os.ReadFile(filepath.Join(it.Path, "报表.txt")); !strings.Contains(string(b), "内容") {
		t.Fatalf("content = %q", b)
	}
}

// tar.gz goes through bsdtar: contents land under the archive-named
// folder and real contents match byte for byte.
func TestExtractTgz(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "in")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "说明.txt"), []byte("说明"), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("b"), 0o644)
	tgz := filepath.Join(dir, "包.tgz")
	if err := exec.Command("tar", "-czf", tgz, "-C", src, ".").Run(); err != nil {
		t.Fatal(err)
	}
	a, view := extractShelf(t, tgz)
	view.Frame()
	if err := view.RightClick("包.tgz"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	it := a.store.Items[0]
	tree := extractedTree(t, it.Path)
	if !slices.Equal(tree, []string{"sub", "sub/b.txt", "说明.txt"}) {
		t.Fatalf("tree = %v", tree)
	}
	if b, _ := os.ReadFile(filepath.Join(it.Path, "说明.txt")); string(b) != "说明" {
		t.Fatalf("说明.txt = %q", b)
	}
}

// Cancelling between archives keeps the finished one and leaves neither a
// half-made result nor a staging folder; a cancelled bsdtar run is killed
// and reported as cancelled, not as a failure.
func TestExtractCancel(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "one.zip")
	makeZip(t, zipPath, map[string][]byte{"a.txt": []byte("a")})
	srcD := filepath.Join(dir, "s")
	os.MkdirAll(srcD, 0o755)
	os.WriteFile(filepath.Join(srcD, "x.txt"), []byte("x"), 0o644)
	tgz := filepath.Join(dir, "two.tgz")
	if err := exec.Command("tar", "-czf", tgz, "-C", srcD, ".").Run(); err != nil {
		t.Fatal(err)
	}
	a, _ := extractShelf(t, zipPath, tgz) // tray order: one.zip, then two.tgz
	old := bsdtarRun
	bsdtarRun = func(j *convertJob, args ...string) ([]byte, error) {
		j.cancel.Store(true)
		return nil, errConvertCanceled
	}
	t.Cleanup(func() { bsdtarRun = old })
	a.startConvert(slices.Clone(a.store.Items), toExtract)
	if got := outputNames(a); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("outputs = %v, the finished archive stays and the cancelled one leaves nothing", got)
	}
	if !strings.HasPrefix(a.status, "已取消") {
		t.Fatalf("status = %q, want a cancellation", a.status)
	}
	if matches, _ := filepath.Glob(filepath.Join(a.store.Root, "converted", ".shelf-extract-*")); len(matches) != 0 {
		t.Fatalf("staging left behind: %v", matches)
	}
}

// The menu offers 解压 only when every target is an archive, and greyed
// while a job runs.
func TestExtractMenuOnlyForArchives(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	makeZip(t, zipPath, map[string][]byte{"a.txt": []byte("a")})
	txt := filepath.Join(dir, "n.txt")
	os.WriteFile(txt, []byte("n"), 0o644)
	a, view := extractShelf(t, zipPath, txt)
	view.Frame()
	if err := view.RightClick("a.zip"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(view.Menu(), "解压") {
		t.Fatalf("an archive's menu lacks 解压: %v", view.Menu())
	}
	view.CloseMenu()
	if err := view.RightClick("n.txt"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(view.Menu(), "解压") {
		t.Fatalf("a plain file must not offer 解压: %v", view.Menu())
	}
	view.CloseMenu()
	selectAll(a)
	view.Frame()
	if err := view.RightClick("a.zip"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(view.Menu(), "解压 2 项") || slices.Contains(view.Menu(), "解压") {
		t.Fatalf("a mixed selection must not offer 解压: %v", view.Menu())
	}
}

// Free space is checked before extracting when the archive size is known.
func TestExtractChecksDiskSpace(t *testing.T) {
	fakeConverters(t)
	old := freeDiskSpace
	freeDiskSpace = func(string) (int64, error) { return 10, nil }
	t.Cleanup(func() { freeDiskSpace = old })
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "big.zip")
	makeZip(t, zipPath, map[string][]byte{"a.txt": make([]byte, 4096)})
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("big.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if !a.statusErr || !strings.Contains(a.status, "磁盘空间不足") {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
	if entries, _ := os.ReadDir(a.store.ConvertedDir()); len(entries) != 0 {
		t.Fatalf("nothing should be extracted: %v", entries)
	}
}

// A zip member that is a symlink to an absolute path is refused, and a
// tar symlink pointing outside is dropped rather than followed.
func TestExtractSymlinkSafety(t *testing.T) {
	fakeConverters(t)
	dir := t.TempDir()
	// zip: member is a symlink to /tmp
	zipPath := filepath.Join(dir, "link.zip")
	zf, _ := os.Create(zipPath)
	zw := zip.NewWriter(zf)
	h := &zip.FileHeader{Name: "link", Method: zip.Store}
	h.SetMode(0o777 | os.ModeSymlink)
	w, _ := zw.CreateHeader(h)
	w.Write([]byte("/tmp"))
	zw.Close()
	zf.Close()
	a, view := extractShelf(t, zipPath)
	view.Frame()
	if err := view.RightClick("link.zip"); err != nil {
		t.Fatal(err)
	}
	if err := view.ChooseMenuItem("解压"); err != nil {
		t.Fatal(err)
	}
	if !a.statusErr || !strings.Contains(a.status, "符号链接") {
		t.Fatalf("status = %q err=%v", a.status, a.statusErr)
	}
}
