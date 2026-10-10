package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, root
}

// realRoot returns the canonical (symlink-resolved) form the store uses
// for Root, since Open resolves symlinks (e.g. macOS /var → /private/var).
func realRoot(t *testing.T, root string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", root, err)
	}
	return resolved
}

// symlinkTo creates a symlink pointing at dir and returns its path.
func symlinkTo(t *testing.T, dir string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "data-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	return link
}

func makePNG(t *testing.T, w, h int, seed color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, seed)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// pngHeaderOnly builds a valid signature + IHDR with arbitrary
// dimensions (no image data): DecodeConfig alone accepts it, which is
// exactly the truncated shape AddImage must reject via full decoding.
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Mode().Perm()
}

func TestTextHistoryLimitDedupAndOrder(t *testing.T) {
	s, _ := newStore(t)
	for i := range 35 {
		if err := s.AddText(fmt.Sprintf("clip-%02d", i)); err != nil {
			t.Fatalf("AddText %d: %v", i, err)
		}
	}
	textCount := 0
	for _, it := range s.Items {
		if it.Kind == KindText {
			textCount++
		}
	}
	if textCount != 30 {
		t.Fatalf("got %d text items, want 30", textCount)
	}
	if s.Items[0].Text != "clip-34" {
		t.Fatalf("newest item = %q, want clip-34", s.Items[0].Text)
	}
	if s.Items[len(s.Items)-1].Text != "clip-05" {
		t.Fatalf("oldest item = %q, want clip-05 (clip-00..04 evicted)", s.Items[len(s.Items)-1].Text)
	}
	// Re-adding existing text dedups and moves it to the front.
	midID := ""
	for _, it := range s.Items {
		if it.Text == "clip-10" {
			midID = it.ID
		}
	}
	if err := s.AddText("clip-10"); err != nil {
		t.Fatalf("re-AddText: %v", err)
	}
	if s.Items[0].Text != "clip-10" || s.Items[0].ID != midID {
		t.Fatalf("dedup did not move existing item to front keeping ID")
	}
	dup := 0
	for _, it := range s.Items {
		if it.Text == "clip-10" {
			dup++
		}
	}
	if dup != 1 {
		t.Fatalf("clip-10 appears %d times", dup)
	}
}

func TestTextLimitDoesNotEvictFiles(t *testing.T) {
	s, _ := newStore(t)
	f := filepath.Join(t.TempDir(), "keep-me.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{f}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	for i := range 35 {
		if err := s.AddText(fmt.Sprintf("t%d", i)); err != nil {
			t.Fatalf("AddText: %v", err)
		}
	}
	found := false
	for _, it := range s.Items {
		if it.Kind == KindFile && it.Path == f {
			found = true
		}
	}
	if !found {
		t.Fatal("file item was evicted by text limit")
	}
}

func TestAddFilesNormalizesAndDedups(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(dir, "sub", "..", "a.txt")
	if err := s.AddFiles([]string{dirty}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	want, _ := filepath.Abs(f)
	if s.Items[0].Path != want {
		t.Fatalf("path = %q, want normalized %q", s.Items[0].Path, want)
	}
	id := s.Items[0].ID
	// Same file via different spellings → same item moved to front.
	if err := s.AddText("separator"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFiles([]string{filepath.Join(dir, ".", "a.txt")}); err != nil {
		t.Fatalf("AddFiles dup: %v", err)
	}
	if len(s.Items) != 2 || s.Items[0].Kind != KindFile || s.Items[0].ID != id {
		t.Fatalf("dedup failed: %+v", s.Items[0])
	}
	// Never copies the original.
	if got := s.Items[0].Path; got != want {
		t.Fatalf("file item does not reference original: %q", got)
	}
}

func TestAddFilesOrderLastIsNewest(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	var paths []string
	for _, n := range []string{"1", "2", "3"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := s.AddFiles(paths); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	if s.Items[0].Path != paths[2] || s.Items[2].Path != paths[0] {
		t.Fatalf("order = [%s %s %s]", s.Items[0].Path, s.Items[1].Path, s.Items[2].Path)
	}
}

func TestAddImageValidatesDedupsAndStores(t *testing.T) {
	s, root := newStore(t)
	pngA := makePNG(t, 4, 4, color.RGBA{R: 255, A: 255})
	pngB := makePNG(t, 4, 4, color.RGBA{G: 200, A: 255})

	if err := s.AddImage(pngA); err != nil {
		t.Fatalf("AddImage A: %v", err)
	}
	img := s.Items[0]
	if img.Kind != KindImage || img.Path == "" {
		t.Fatalf("bad image item: %+v", img)
	}
	data, err := os.ReadFile(img.Path)
	if err != nil || !bytes.Equal(data, pngA) {
		t.Fatalf("managed png bytes mismatch: err=%v", err)
	}
	if p := filePerm(t, img.Path); p != 0o600 {
		t.Fatalf("managed png perm = %o, want 600", p)
	}
	if filepath.Dir(img.Path) != filepath.Join(realRoot(t, root), imagesDirName) {
		t.Fatalf("image not under images dir: %q", img.Path)
	}

	// Identical bytes → dedup, move to front, no extra file.
	if err := s.AddText("x"); err != nil {
		t.Fatal(err)
	}
	idA := img.ID
	if err := s.AddImage(pngA); err != nil {
		t.Fatalf("AddImage A dup: %v", err)
	}
	if s.Items[0].ID != idA || s.Items[0].Kind != KindImage {
		t.Fatalf("image dedup failed: %+v", s.Items[0])
	}
	nImg := 0
	for _, it := range s.Items {
		if it.Kind == KindImage {
			nImg++
		}
	}
	if nImg != 1 {
		t.Fatalf("%d image items after dup add", nImg)
	}
	ents, _ := os.ReadDir(filepath.Join(root, imagesDirName))
	if len(ents) != 1 {
		t.Fatalf("%d files in images dir after dup add", len(ents))
	}

	if err := s.AddImage(pngB); err != nil {
		t.Fatalf("AddImage B: %v", err)
	}
	if len(s.Items) != 3 || s.Items[0].Kind != KindImage {
		t.Fatalf("second image not added at front: %v", s.Items)
	}
}

func TestAddImageRejectsBadInput(t *testing.T) {
	s, root := newStore(t)
	for name, blob := range map[string][]byte{
		"empty":       {},
		"not-an-img":  []byte("hello world, not an image"),
		"header-only": pngHeaderOnly(10, 10),     // 合法签名+IHDR，没有像素数据
		"huge-dim":    pngHeaderOnly(20000, 10),  // width > maxImageDim
		"huge-pixels": pngHeaderOnly(8192, 8193), // dims OK, ~67 MP > 64 MP limit
		"zero-width":  pngHeaderOnly(0, 100),
	} {
		if err := s.AddImage(blob); err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
	}
	// Nothing was persisted or written.
	if len(s.Items) != 0 {
		t.Fatalf("rejected images left %d items", len(s.Items))
	}
	ents, err := os.ReadDir(filepath.Join(root, imagesDirName))
	if err != nil || len(ents) != 0 {
		t.Fatalf("images dir not empty: ents=%v err=%v", ents, err)
	}
}

// 尺寸正常但数据截断或损坏的 PNG 不得被当作成功
// 图片保存；必须报错、不新增历史、不遗留图片文件。
func TestAddImageRejectsTruncatedAndCorruptData(t *testing.T) {
	full := makePNG(t, 8, 8, color.RGBA{R: 1, B: 2, A: 255})

	// 从尾部截断，直到完整解码失败（DecodeConfig 仍然通过）。
	var truncated []byte
	for cut := 1; cut < len(full); cut++ {
		cand := full[:len(full)-cut]
		if _, _, err := image.DecodeConfig(bytes.NewReader(cand)); err != nil {
			continue
		}
		if _, _, err := image.Decode(bytes.NewReader(cand)); err != nil {
			truncated = cand
			break
		}
	}
	if truncated == nil {
		t.Fatal("could not build a header-valid truncated png")
	}

	// 中间数据被破坏（尺寸与头部正常）的版本。
	var corrupt []byte
	for off := 40; off < len(full)-8; off++ {
		cand := bytes.Clone(full)
		cand[off] ^= 0xff
		if _, _, err := image.DecodeConfig(bytes.NewReader(cand)); err != nil {
			continue
		}
		if _, _, err := image.Decode(bytes.NewReader(cand)); err != nil {
			corrupt = cand
			break
		}
	}
	if corrupt == nil {
		t.Fatal("could not build a corrupt png")
	}

	s, root := newStore(t)
	for name, blob := range map[string][]byte{
		"truncated": truncated,
		"corrupt":   corrupt,
	} {
		if err := s.AddImage(blob); err == nil {
			t.Fatalf("%s: expected error, got nil", name)
		}
	}
	if len(s.Items) != 0 {
		t.Fatalf("rejected images left %d items", len(s.Items))
	}
	ents, err := os.ReadDir(filepath.Join(root, imagesDirName))
	if err != nil || len(ents) != 0 {
		t.Fatalf("images dir not empty: ents=%v err=%v", ents, err)
	}
	if _, err := os.Stat(filepath.Join(root, stateFileName)); !os.IsNotExist(err) {
		t.Fatalf("state.json created by rejected images: %v", err)
	}
}

func TestOpenCleansNonexistentKeepsErrors(t *testing.T) {
	s, root := newStore(t)
	dir := t.TempDir()
	okFile := filepath.Join(dir, "ok.txt")
	goneFile := filepath.Join(dir, "gone.txt")
	target := filepath.Join(dir, "missing-target")
	link := filepath.Join(dir, "dangling")
	lockedDir := filepath.Join(dir, "locked")
	lockedFile := filepath.Join(lockedDir, "secret.txt")

	os.WriteFile(okFile, []byte("o"), 0o600)
	os.WriteFile(goneFile, []byte("g"), 0o600)
	os.Symlink(target, link)
	os.Mkdir(lockedDir, 0o700)
	os.WriteFile(lockedFile, []byte("s"), 0o600)

	if err := s.AddFiles([]string{okFile, goneFile, link, lockedFile}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	os.Remove(goneFile)
	os.Chmod(lockedDir, 0o000)
	t.Cleanup(func() { os.Chmod(lockedDir, 0o700) })

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if len(s2.Items) != 3 {
		t.Fatalf("got %d items, want 3 (gone cleaned): %+v", len(s2.Items), s2.Items)
	}
	byPath := map[string]Item{}
	for _, it := range s2.Items {
		byPath[it.Path] = it
	}
	if it, ok := byPath[okFile]; !ok || it.Missing {
		t.Fatalf("ok file: %+v present=%v", it, ok)
	}
	if it, ok := byPath[link]; !ok || !it.Missing {
		t.Fatalf("dangling symlink should be kept Missing: %+v present=%v", it, ok)
	}
	if it, ok := byPath[lockedFile]; !ok || !it.Missing {
		t.Fatalf("unreadable file should be kept Missing: %+v present=%v", it, ok)
	}
	if _, ok := byPath[goneFile]; ok {
		t.Fatal("nonexistent file was not cleaned")
	}
}

func TestCorruptStateErrorsAndNeverOverwritten(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, imagesDirName), 0o700)
	bad := []byte("{ this is not json !!!")
	if err := os.WriteFile(filepath.Join(root, stateFileName), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("expected error for corrupt state.json")
	}
	got, _ := os.ReadFile(filepath.Join(root, stateFileName))
	if !bytes.Equal(got, bad) {
		t.Fatal("corrupt state.json was overwritten")
	}
}

func TestUnsupportedVersionErrors(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, imagesDirName), 0o700)
	data, _ := json.Marshal(stateFile{Version: 99, Items: []Item{}})
	os.WriteFile(filepath.Join(root, stateFileName), data, 0o600)
	if _, err := Open(root); err == nil {
		t.Fatal("expected version error")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	s, root := newStore(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "doc.txt")
	os.WriteFile(f, []byte("d"), 0o600)
	s.AddImage(makePNG(t, 3, 3, color.RGBA{B: 7, A: 255}))
	s.AddText("persistent words")
	s.AddFiles([]string{f})

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if len(s2.Items) != len(s.Items) {
		t.Fatalf("item count %d, want %d", len(s2.Items), len(s.Items))
	}
	for i := range s.Items {
		a, b := s.Items[i], s2.Items[i]
		if a.ID != b.ID || a.Kind != b.Kind || a.Path != b.Path || a.Text != b.Text {
			t.Fatalf("item %d changed across restart: %+v vs %+v", i, a, b)
		}
		if b.Missing {
			t.Fatalf("item %d wrongly Missing after restart", i)
		}
	}
	// Order is load order: file added last is first.
	if s2.Items[0].Kind != KindFile || s2.Items[1].Kind != KindText || s2.Items[2].Kind != KindImage {
		t.Fatalf("load order wrong: %v", s2.Items)
	}
}

func TestRefreshFlagsMissingWithoutRemoving(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "later-gone.txt")
	os.WriteFile(f, []byte("x"), 0o600)
	s.AddFiles([]string{f})
	s.Refresh()
	if s.Items[0].Missing {
		t.Fatal("existing file flagged Missing")
	}
	os.Remove(f)
	s.Refresh()
	if !s.Items[0].Missing || len(s.Items) != 1 {
		t.Fatalf("missing flag/removal wrong: %+v len=%d", s.Items[0], len(s.Items))
	}
	// File comes back → flag clears; text items are never stat'd.
	os.WriteFile(f, []byte("x"), 0o600)
	s.AddText("t")
	s.Refresh()
	if s.Items[1].Missing {
		t.Fatal("restored file still Missing")
	}
	if len(s.Items) != 2 {
		t.Fatal("Refresh removed an item")
	}
}

func TestRemove(t *testing.T) {
	s, root := newStore(t)
	dir := t.TempDir()
	original := filepath.Join(dir, "precious.txt")
	os.WriteFile(original, []byte("keep me"), 0o600)
	s.AddFiles([]string{original})
	fileID := s.Items[0].ID

	if err := s.Remove("no-such-id"); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if err := s.Remove(fileID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatal("item not removed")
	}
	// Original user file is never deleted.
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("original file deleted: %v", err)
	}
	// Removal persisted.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if len(s2.Items) != 0 {
		t.Fatal("removal not persisted")
	}
}

func TestRemoveManagedImageDeletesFile(t *testing.T) {
	s, _ := newStore(t)
	s.AddImage(makePNG(t, 2, 2, color.RGBA{A: 1}))
	managed := s.Items[0].Path
	id := s.Items[0].ID
	if err := s.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("managed png still exists: %v", err)
	}
}

func TestRemoveManagedImageKeepsReferencedFile(t *testing.T) {
	s, _ := newStore(t)
	pngA := makePNG(t, 2, 2, color.RGBA{R: 9, A: 255})
	s.AddImage(pngA)
	managed := s.Items[0].Path
	imgID := s.Items[0].ID
	// A file-kind item ends up referencing the same path (e.g. user
	// dropped the managed file back in).
	if err := s.AddFiles([]string{managed}); err != nil {
		t.Fatalf("AddFiles managed path: %v", err)
	}
	if s.Items[0].Kind != KindFile || s.Items[0].Path != managed {
		t.Fatalf("file ref to managed path missing: %+v", s.Items[0])
	}
	if err := s.Remove(imgID); err != nil {
		t.Fatalf("Remove image: %v", err)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("referenced managed png wrongly deleted: %v", err)
	}
	// Remove the file ref too → next cleanup of an unreferenced
	// managed file would apply; the file itself stays until then.
	if err := s.Remove(s.Items[0].ID); err != nil {
		t.Fatalf("Remove file ref: %v", err)
	}
}

func TestRemoveNeverDeletesOutsideImages(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "not-ours.png")
	os.WriteFile(outside, []byte("x"), 0o600)
	// Crafted state: an image item pointing outside images/.
	sf := stateFile{Version: stateVersion, Items: []Item{{
		ID: "crafted", Kind: KindImage, Path: outside,
	}}}
	data, _ := json.Marshal(sf)
	os.MkdirAll(filepath.Join(root, imagesDirName), 0o700)
	os.WriteFile(filepath.Join(root, stateFileName), data, 0o600)

	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(s.Items) != 1 {
		t.Fatalf("item dropped: %+v", s.Items)
	}
	if err := s.Remove("crafted"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was deleted: %v", err)
	}
}

// 根目录不可写导致 Save 失败时，AddImage 只能清理
// 本次新建且回滚后无人引用的文件；原本已有、尤其仍被文件引用指着的
// 托管 PNG 必须原样保留，否则既有引用失效、托盘图片丢失。
func TestAddImageSaveFailureKeepsExistingReferencedPng(t *testing.T) {
	s, root := newStore(t)
	pngBytes := makePNG(t, 3, 3, color.RGBA{R: 42, A: 255})
	if err := s.AddImage(pngBytes); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	managed := s.Items[0].Path
	imgID := s.Items[0].ID

	// 文件项引用同一托管 PNG，然后移除图片项：文件因仍被引用而保留。
	if err := s.AddFiles([]string{managed}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	if err := s.Remove(imgID); err != nil {
		t.Fatalf("Remove image item: %v", err)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("precondition: managed png missing after Remove: %v", err)
	}
	stateBefore, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil {
		t.Fatal(err)
	}

	// 根目录不可写（images 子目录仍可写）→ Save 失败。
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if err := s.AddImage(pngBytes); err == nil {
		t.Fatal("expected save failure with read-only root")
	}
	// 原 PNG 必须还在且内容不变。
	got, err := os.ReadFile(managed)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("referenced png damaged: err=%v", err)
	}
	// 内存回滚：没有图片项，文件引用仍在。
	if len(s.Items) != 1 || s.Items[0].Kind != KindFile || s.Items[0].Path != managed {
		t.Fatalf("memory not rolled back: %+v", s.Items)
	}
	// 原历史 JSON 原样保留。
	stateAfter, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil || !bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("state.json changed on failed save: err=%v", err)
	}
}

// 已存在但当前无人引用的托管文件，失败清理同样不得删除：清理只区分
// “本次新建”与“原本已有”，与是否仍被引用无关。
func TestAddImageSaveFailureKeepsPreexistingOrphanFile(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	pngBytes := makePNG(t, 2, 2, color.RGBA{G: 5, A: 255})
	sum := sha256.Sum256(pngBytes)
	orphan := filepath.Join(root, imagesDirName, fmt.Sprintf("%x.png", sum))
	if err := os.WriteFile(orphan, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if err := s.AddImage(pngBytes); err == nil {
		t.Fatal("expected save failure with read-only root")
	}
	if got, err := os.ReadFile(orphan); err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("pre-existing file deleted or changed: err=%v", err)
	}
	if len(s.Items) != 0 {
		t.Fatalf("items not rolled back: %+v", s.Items)
	}
}

// 本次新建、回滚后无人引用的文件仍按原设计清理，不留孤儿。
func TestAddImageSaveFailureCleansOnlyNewFile(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if err := s.AddImage(makePNG(t, 2, 2, color.RGBA{B: 3, A: 255})); err == nil {
		t.Fatal("expected save failure with read-only root")
	}
	if len(s.Items) != 0 {
		t.Fatalf("items not rolled back: %+v", s.Items)
	}
	ents, err := os.ReadDir(filepath.Join(root, imagesDirName))
	if err != nil || len(ents) != 0 {
		t.Fatalf("new orphan not cleaned: ents=%v err=%v", ents, err)
	}
}

func TestSaveFailureRollsBackAndPreservesJSON(t *testing.T) {
	s, root := newStore(t)
	if err := s.AddText("first"); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if err := s.AddText("second"); err == nil {
		t.Fatal("expected save error with read-only root")
	}
	if len(s.Items) != 1 || s.Items[0].Text != "first" {
		t.Fatalf("memory not rolled back: %+v", s.Items)
	}
	after, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("previous state.json lost/corrupted on failed save")
	}
	// Remove also rolls back.
	if err := s.Remove(s.Items[0].ID); err == nil {
		t.Fatal("expected save error on Remove")
	}
	if len(s.Items) != 1 {
		t.Fatal("Remove memory not rolled back")
	}
}

func TestIDsUniqueAndStable(t *testing.T) {
	s, _ := newStore(t)
	for i := range 100 {
		if err := s.AddText(fmt.Sprintf("u%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, it := range s.Items {
		if it.ID == "" {
			t.Fatal("empty ID")
		}
		if seen[it.ID] {
			t.Fatalf("duplicate ID %q", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestEmptyDirAndPermSetup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deep", "root")
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open nested root: %v", err)
	}
	if len(s.Items) != 0 {
		t.Fatal("fresh store not empty")
	}
	if p := filePerm(t, filepath.Join(root, imagesDirName)); p != 0o700 {
		t.Fatalf("images dir perm = %o, want 700", p)
	}
	if err := s.AddText("x"); err != nil {
		t.Fatal(err)
	}
	if p := filePerm(t, filepath.Join(root, stateFileName)); p != 0o600 {
		t.Fatalf("state.json perm = %o, want 600", p)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// 同一数据目录只能被一个打开着的 Store 使用；
// 第二个实例必须明确失败而不是共享写入；不同目录互不影响；Close 或
// 进程退出后可以重新打开（flock 由内核在 fd 关闭时释放，无永久残留）。
func TestExclusiveLockSameProcess(t *testing.T) {
	root := t.TempDir()
	a, err := Open(root)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := Open(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open error = %v, want ErrLocked", err)
	}

	// 不同数据目录仍允许分别运行。
	b, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("different root: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close second root: %v", err)
	}

	// 释放后可以重新打开。
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	c, err := Open(root)
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// 审查复现：第二个实例写入 B 后重开只剩 B。锁生效后第二个实例必须
// 打不开目录，第一个实例的历史保持完整。
func TestLockedSecondInstanceCannotOverwrite(t *testing.T) {
	root := t.TempDir()
	first, err := Open(root)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := first.AddText("A"); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	if _, err := Open(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Open error = %v, want ErrLocked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := Open(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()
	if len(re.Items) != 1 || re.Items[0].Text != "A" {
		t.Fatalf("first instance history lost: %+v", re.Items)
	}
}

// lockHelperEnv marks the re-executed test binary that only opens the
// store to hold the directory lock for TestExclusiveLockAcrossProcesses.
const lockHelperEnv = "SHELF_TEST_LOCK_HELPER_ROOT"

func TestStoreLockHelperProcess(t *testing.T) {
	root := os.Getenv(lockHelperEnv)
	if root == "" {
		t.Skip("helper process for TestExclusiveLockAcrossProcesses")
	}
	s, err := Open(root)
	if err != nil {
		fmt.Printf("HELPER_OPEN_FAILED %v\n", err)
		return
	}
	fmt.Println("LOCKED")
	// 持有锁，直到父进程关闭 stdin。
	_, _ = io.Copy(io.Discard, os.Stdin)
	s.Close()
	fmt.Println("RELEASED")
}

// 真实多进程验证：子进程持有锁期间父进程打不开同一目录；子进程退出
// （包括被杀死）后锁自动释放，可以重新打开。
func TestExclusiveLockAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreLockHelperProcess$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting helper: %v", err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			stdin.Close()
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	scan := bufio.NewScanner(stdout)
	sawLocked := false
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "LOCKED" {
			sawLocked = true
			break
		}
		if strings.HasPrefix(line, "HELPER_OPEN_FAILED") {
			t.Fatalf("helper could not open the store: %s", line)
		}
	}
	if !sawLocked {
		t.Fatalf("helper never reported LOCKED (err=%v)", scan.Err())
	}

	if _, err := Open(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("Open while helper holds the lock = %v, want ErrLocked", err)
	}

	// 让 helper 正常退出释放锁。
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper exit: %v", err)
	}
	waited = true
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open after helper exit: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// 在某个工作目录下用相对 root 写入图片后，换
// 工作目录再用绝对路径打开同一数据目录，图片项必须还在；托管图片以
// 相对 images 目录的文件名落盘。
func TestRelativeRootPersistsImagesAcrossCWDChange(t *testing.T) {
	parent := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}

	s, err := Open("data")
	if err != nil {
		t.Fatalf("Open relative: %v", err)
	}
	if !filepath.IsAbs(s.Root) {
		t.Fatalf("Root not normalized: %q", s.Root)
	}
	pngBytes := makePNG(t, 2, 2, color.RGBA{R: 7, A: 255})
	if err := s.AddImage(pngBytes); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	imgPath := s.Items[0].Path
	if !filepath.IsAbs(imgPath) {
		t.Fatalf("in-memory image path not absolute: %q", imgPath)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 落盘格式：图片路径相对 images 目录（仅文件名，不含工作目录）。
	data, err := os.ReadFile(filepath.Join(parent, "data", stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatal(err)
	}
	if len(sf.Items) != 1 || sf.Items[0].Kind != KindImage {
		t.Fatalf("persisted items: %+v", sf.Items)
	}
	if got := sf.Items[0].Path; got == "" || filepath.IsAbs(got) || got != filepath.Base(got) {
		t.Fatalf("image path not relative to images dir: %q", got)
	}

	// 换工作目录，用绝对路径重开同一数据目录：图片项必须恢复。
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(filepath.Join(parent, "data"))
	if err != nil {
		t.Fatalf("Open absolute: %v", err)
	}
	defer s2.Close()
	if len(s2.Items) != 1 || s2.Items[0].Kind != KindImage || s2.Items[0].Missing {
		t.Fatalf("image item lost after cwd change: %+v", s2.Items)
	}
	got, err := os.ReadFile(s2.Items[0].Path)
	if err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("resolved image unreadable: err=%v", err)
	}
}

// 旧版 state.json 兼容：绝对路径与“按当时数据根拼出的相对路径”都必须
// 解析到当前根下的同一受管文件；迁移前保留 state.json.bak，下一次
// 保存改写为相对 images 目录的新格式，且不丢项。
func TestLegacyImagePathsResolveAndBackUp(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, imagesDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	pngA := makePNG(t, 2, 2, color.RGBA{G: 9, A: 255})
	pngB := makePNG(t, 2, 2, color.RGBA{B: 11, A: 255})
	sumA := sha256.Sum256(pngA)
	sumB := sha256.Sum256(pngB)
	nameA := fmt.Sprintf("%x.png", sumA)
	nameB := fmt.Sprintf("%x.png", sumB)
	if err := os.WriteFile(filepath.Join(root, imagesDirName, nameA), pngA, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, imagesDirName, nameB), pngB, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := stateFile{Version: stateVersion, Items: []Item{
		{
			ID: "legacy-relative", Kind: KindImage,
			// 旧版本在某工作目录下用相对 root 写入的路径。
			Path: filepath.Join("that-cwd", "data", imagesDirName, nameA),
		},
		{
			ID: "legacy-absolute", Kind: KindImage,
			// 现有数据里的绝对路径也要继续可用。
			Path: filepath.Join(root, imagesDirName, nameB),
		},
	}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, stateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if len(s.Items) != 2 {
		t.Fatalf("legacy items lost: %+v", s.Items)
	}
	byID := map[string]Item{}
	for _, it := range s.Items {
		byID[it.ID] = it
	}
	canonical := realRoot(t, root)
	if it := byID["legacy-relative"]; it.Missing || it.Path != filepath.Join(canonical, imagesDirName, nameA) {
		t.Fatalf("relative legacy path not resolved: %+v", it)
	}
	if it := byID["legacy-absolute"]; it.Missing || it.Path != filepath.Join(canonical, imagesDirName, nameB) {
		t.Fatalf("absolute legacy path not kept: %+v", it)
	}

	// 迁移前保留可恢复原件。
	bak, err := os.ReadFile(filepath.Join(root, stateFileName+".bak"))
	if err != nil || !bytes.Equal(bak, data) {
		t.Fatalf("legacy state backup missing/changed: err=%v", err)
	}

	// 下一次保存写入新格式（仅文件名），不丢任何一项。
	if err := s.AddText("migrate"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if err := json.Unmarshal(reloaded, &sf); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range sf.Items {
		if it.Kind == KindImage {
			got[it.ID] = it.Path
		}
	}
	if got["legacy-relative"] != nameA || got["legacy-absolute"] != nameB {
		t.Fatalf("migrated image paths wrong: %+v", got)
	}
}

// 同一数据目录经软链接路径打开时，删除图片项
// 不得误删仍被文件引用指着的托管 PNG。
func TestSymlinkedRootRemoveKeepsReferencedPng(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	pngBytes := makePNG(t, 3, 3, color.RGBA{R: 21, A: 255})
	if err := s.AddImage(pngBytes); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	managed := s.Items[0].Path
	imgID := s.Items[0].ID
	// 把托管 PNG 再加为普通文件引用。
	if err := s.AddFiles([]string{managed}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 经指向同一数据目录的软链接重新打开。
	link := symlinkTo(t, root)
	s2, err := Open(link)
	if err != nil {
		t.Fatalf("Open via symlink: %v", err)
	}
	defer s2.Close()
	if len(s2.Items) != 2 {
		t.Fatalf("items after symlinked reopen: %+v", s2.Items)
	}

	if err := s2.Remove(imgID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// PNG 必须仍在（剩余文件引用仍指向它）。
	if got, err := os.ReadFile(managed); err != nil || !bytes.Equal(got, pngBytes) {
		t.Fatalf("referenced png deleted via symlinked root: err=%v", err)
	}
	s2.Refresh()
	if len(s2.Items) != 1 || s2.Items[0].Kind != KindFile || s2.Items[0].Missing {
		t.Fatalf("remaining file reference broken: %+v", s2.Items)
	}
}

// 旧版 state.json 存绝对图片路径：经目录软链接打开后再次 AddImage 同一张
// 图片，必须只保留一条图片项且 ID 唯一（旧行为会因路径拼写不同重复添加）。
func TestSymlinkedRootLegacyAbsoluteImageDedups(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, imagesDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	pngBytes := makePNG(t, 2, 2, color.RGBA{B: 33, A: 255})
	sum := sha256.Sum256(pngBytes)
	name := fmt.Sprintf("%x.png", sum)
	if err := os.WriteFile(filepath.Join(root, imagesDirName, name), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := stateFile{Version: stateVersion, Items: []Item{{
		ID: "legacy-img", Kind: KindImage,
		Path: filepath.Join(root, imagesDirName, name),
	}}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, stateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	link := symlinkTo(t, root)
	s, err := Open(link)
	if err != nil {
		t.Fatalf("Open via symlink: %v", err)
	}
	defer s.Close()
	if len(s.Items) != 1 || s.Items[0].Missing {
		t.Fatalf("legacy image not resolved: %+v", s.Items)
	}

	if err := s.AddImage(pngBytes); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	ids := map[string]bool{}
	imgCount := 0
	for _, it := range s.Items {
		if ids[it.ID] {
			t.Fatalf("duplicate item id %q: %+v", it.ID, s.Items)
		}
		ids[it.ID] = true
		if it.Kind == KindImage {
			imgCount++
		}
	}
	if imgCount != 1 || len(s.Items) != 1 {
		t.Fatalf("image not deduplicated across symlinked root: %+v", s.Items)
	}
	ents, err := os.ReadDir(filepath.Join(root, imagesDirName))
	if err != nil || len(ents) != 1 {
		t.Fatalf("images dir has %d files (err=%v)", len(ents), err)
	}
}

// 确实无人引用的托管图片，经软链接打开时仍应被正常清理。
func TestSymlinkedRootRemoveUnreferencedStillCleans(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.AddImage(makePNG(t, 2, 2, color.RGBA{G: 44, A: 255})); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	managed := s.Items[0].Path
	imgID := s.Items[0].ID
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	link := symlinkTo(t, root)
	s2, err := Open(link)
	if err != nil {
		t.Fatalf("Open via symlink: %v", err)
	}
	defer s2.Close()
	if err := s2.Remove(imgID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Fatalf("unreferenced managed png not cleaned: %v", err)
	}
}

// 删除保护按实际文件身份判断：文件引用用软链接拼写指向同一张 PNG 时也
// 不得删除（不能只比较路径字符串）。
func TestRemoveKeepsPngReferencedViaSymlinkSpelling(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.AddImage(makePNG(t, 2, 2, color.RGBA{R: 55, G: 5, A: 255})); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	managed := s.Items[0].Path
	imgID := s.Items[0].ID

	link := symlinkTo(t, root)
	spelled := filepath.Join(link, imagesDirName, filepath.Base(managed))
	if err := s.AddFiles([]string{spelled}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	if err := s.Remove(imgID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(managed); err != nil {
		t.Fatalf("png referenced via symlink spelling deleted: %v", err)
	}
	s.Refresh()
	found := false
	for _, it := range s.Items {
		if it.Kind == KindFile {
			found = true
			if it.Missing {
				t.Fatalf("file reference broken: %+v", it)
			}
		}
	}
	if !found {
		t.Fatalf("file reference lost: %+v", s.Items)
	}
}

// 删除保护缺口（本轮）：检查剩余项目时，引用项一侧的 Stat 因权限/IO
// 错误失败，属于“无法确认是否仍被引用”，必须保守保留目标 PNG，不能
// 进入删除分支；正常清理行为不受影响。
func TestRemoveKeepsPngWhenReferenceUnreadable(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	pngKeep := makePNG(t, 3, 3, color.RGBA{R: 91, A: 255})
	if err := s.AddImage(pngKeep); err != nil {
		t.Fatalf("AddImage keep: %v", err)
	}
	keepPath := s.Items[0].Path
	keepID := s.Items[0].ID
	pngClean := makePNG(t, 2, 2, color.RGBA{G: 92, A: 255})
	if err := s.AddImage(pngClean); err != nil {
		t.Fatalf("AddImage clean: %v", err)
	}
	cleanPath := s.Items[0].Path
	cleanID := s.Items[0].ID

	// 经软链接拼写把 keep PNG 加入为普通文件引用。
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "data-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	spelled := filepath.Join(link, imagesDirName, filepath.Base(keepPath))
	if err := s.AddFiles([]string{spelled}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}

	// 让引用路径的 Stat 报权限错误：父目录 000，托管 PNG 原路径仍可读。
	if err := os.Chmod(linkParent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(linkParent, 0o700) })
	if _, err := os.Stat(spelled); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("precondition: stat(%q) = %v, want permission error", spelled, err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("precondition: managed png unreadable: %v", err)
	}

	if err := s.Remove(keepID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// PNG 字节必须完整，文件引用必须仍在。
	got, err := os.ReadFile(keepPath)
	if err != nil || !bytes.Equal(got, pngKeep) {
		t.Fatalf("png deleted or damaged while a reference was unreadable: err=%v", err)
	}
	fileRefs := 0
	for _, it := range s.Items {
		if it.Kind == KindFile {
			fileRefs++
		}
	}
	if fileRefs != 1 {
		t.Fatalf("file reference lost: %+v", s.Items)
	}

	// 恢复权限后引用应有效（Refresh 不再 Missing）。
	if err := os.Chmod(linkParent, 0o700); err != nil {
		t.Fatal(err)
	}
	s.Refresh()
	for _, it := range s.Items {
		if it.Kind == KindFile && it.Missing {
			t.Fatalf("reference still missing after permissions restored: %+v", it)
		}
	}

	// 正常清理不受影响：无引用的图片项删除后文件应被清理。
	if err := s.Remove(cleanID); err != nil {
		t.Fatalf("Remove clean: %v", err)
	}
	if _, err := os.Stat(cleanPath); !os.IsNotExist(err) {
		t.Fatalf("unreferenced managed png not cleaned: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("kept png vanished: %v", err)
	}
}

// writeLegacyStateWithImage prepares a root whose state.json uses the old
// absolute image path format (triggering migration on Open); it returns
// the original state.json bytes and the managed image file name.
func writeLegacyStateWithImage(t *testing.T, root string, seed color.RGBA) ([]byte, string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, imagesDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	pngBytes := makePNG(t, 2, 2, seed)
	sum := sha256.Sum256(pngBytes)
	name := fmt.Sprintf("%x.png", sum)
	if err := os.WriteFile(filepath.Join(root, imagesDirName, name), pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	sf := stateFile{Version: stateVersion, Items: []Item{
		{ID: "legacy-img", Kind: KindImage, Path: filepath.Join(root, imagesDirName, name)},
		{ID: "t-keep", Kind: KindText, Text: "保留的老文字"},
	}}
	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, stateFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data, name
}

// 常规备份名被目录占用时，应改用独立的新
// 备份文件保留本次迁移前的原件，再继续迁移；绝不能在无恢复点的情况下
// 覆盖原件，也不得覆盖占位者。
func TestMigrationBackupUsesAlternateWhenBakOccupied(t *testing.T) {
	root := t.TempDir()
	original, _ := writeLegacyStateWithImage(t, root, color.RGBA{R: 61, A: 255})
	if err := os.Mkdir(filepath.Join(root, stateFileName+".bak"), 0o700); err != nil {
		t.Fatal(err)
	}

	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.MigrationError() != nil {
		t.Fatalf("alternate backup should be a usable recovery point: %v", s.MigrationError())
	}
	if err := s.AddText("新文字"); err != nil {
		t.Fatalf("AddText with alternate backup should succeed: %v", err)
	}

	alt := filepath.Join(root, stateFileName+".bak-2")
	if got, err := os.ReadFile(alt); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("alternate backup missing or changed: err=%v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, stateFileName+".bak")); err != nil || !fi.IsDir() {
		t.Fatalf("occupied path overwritten: err=%v", err)
	}
	// 迁移不丢项：老文字与图片项都还在，新文字在最前。
	if len(s.Items) != 3 || s.Items[0].Kind != KindText || s.Items[0].Text != "新文字" {
		t.Fatalf("items after migration: %+v", s.Items)
	}
	for _, it := range s.Items {
		if it.ID == "legacy-img" && (it.Missing || !filepath.IsAbs(it.Path)) {
			t.Fatalf("legacy image lost in migration: %+v", it)
		}
	}
}

// 备份创建彻底失败（全部候选名不可用）时：Open 不报致命错误但停止写入，
// 原件字节不变、不伪装成功；恢复正常条件后重新打开可以安全继续，
// 备份内容完整且迁移不丢项。
func TestMigrationWithoutAnyBackupBlocksWrites(t *testing.T) {
	root := t.TempDir()
	original, _ := writeLegacyStateWithImage(t, root, color.RGBA{G: 71, A: 255})
	for _, name := range migrationBackupNames {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.MigrationError() == nil {
		t.Fatal("expected migration blocked without any backup")
	}
	if err := s.AddText("写不进去"); err == nil {
		t.Fatal("AddText must fail without a recovery point")
	}
	for _, it := range s.Items {
		if it.Kind == KindText && it.Text == "写不进去" {
			t.Fatalf("failed write leaked into memory: %+v", s.Items)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("original state.json changed without recovery point: err=%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// 恢复正常条件后可以安全继续。
	for _, name := range migrationBackupNames {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	s2, err := Open(root)
	if err != nil {
		t.Fatalf("reopen after recovery: %v", err)
	}
	defer s2.Close()
	if s2.MigrationError() != nil {
		t.Fatalf("still blocked after recovery: %v", s2.MigrationError())
	}
	if err := s2.AddText("恢复后写入"); err != nil {
		t.Fatalf("AddText after recovery: %v", err)
	}
	bak, err := os.ReadFile(filepath.Join(root, stateFileName+".bak"))
	if err != nil || !bytes.Equal(bak, original) {
		t.Fatalf("backup after recovery missing/changed: err=%v", err)
	}
	// 无半成品后备文件残留。
	if _, err := os.Stat(filepath.Join(root, stateFileName+".bak-2")); !os.IsNotExist(err) {
		t.Fatalf("leftover alternate backup: %v", err)
	}
	reloaded, err := os.ReadFile(filepath.Join(root, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var sf stateFile
	if err := json.Unmarshal(reloaded, &sf); err != nil {
		t.Fatal(err)
	}
	if len(sf.Items) != 3 {
		t.Fatalf("items after recovered migration: %+v", sf.Items)
	}
}

// 已有的 .bak 与本次待迁移原件不一致（不完整/损坏）时不视为有效恢复点：
// 不覆盖它、也不复用它，另建独立备份保存本次原件。
func TestMigrationKeepsExistingIncompleteBackup(t *testing.T) {
	root := t.TempDir()
	original, _ := writeLegacyStateWithImage(t, root, color.RGBA{B: 81, A: 255})
	incomplete := bytes.Clone(original[:len(original)/2])
	if err := os.WriteFile(filepath.Join(root, stateFileName+".bak"), incomplete, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.AddText("继续迁移"); err != nil {
		t.Fatalf("AddText: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, stateFileName+".bak")); err != nil || !bytes.Equal(got, incomplete) {
		t.Fatalf("existing backup overwritten: err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, stateFileName+".bak-2")); err != nil || !bytes.Equal(got, original) {
		t.Fatalf("independent backup missing/changed: err=%v", err)
	}
	if len(s.Items) != 3 {
		t.Fatalf("items after migration: %+v", s.Items)
	}
}
