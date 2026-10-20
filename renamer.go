//go:build darwin

package main

// Batch rename: the pure logic — computing new names, checking conflicts,
// and the two-phase executor — lives here so unit tests can run it without
// a window. The window and menus are in rename.go and actions.go.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/text/unicode/norm"

	"shelf/internal/store"
)

// renameMode picks which rule computes each new base name, like Finder's
// three rename actions.
type renameMode int

const (
	modeReplace renameMode = iota // 替换文字
	modeAdd                       // 添加文字
	modeFormat                    // 名称加序号
)

// renameParams are the window's inputs: find/replace, add text before or
// after, or a custom name with a numbered index (start, zero-padded width,
// index before or after the name).
type renameParams struct {
	mode       renameMode
	find       string
	replace    string
	addText    string
	addBefore  bool
	fmtName    string
	fmtStart   int
	fmtDigits  int
	fmtNumLast bool
}

// renameOp is one disk move: src to dst inside the same directory.
type renameOp struct {
	src, dst string
}

// renameRow is one preview line: the item, its current and computed base
// name, and why the row conflicts (empty when clean). The executor moves
// oldPath to newPath; rows only preview what planRename computed.
type renameRow struct {
	item    store.Item
	oldPath string
	oldName string // file name with extension
	newName string
	newPath string
	warn    string // conflict reason shown in red
}

// renamePlan is one preview pass over the chosen items.
type renamePlan struct {
	rows      []renameRow
	ops       []renameOp // rows without warn whose name changes, in order
	conflicts int        // rows that block 改名
	changed   int        // rows whose name would change
}

// renameNameMax is the file-name length limit checked at preview time
// (NAME_MAX on APFS/HFS+), in bytes.
const renameNameMax = 255

// renamePathMax is the longest full path accepted, in bytes. PATH_MAX is
// 1024 including the terminator and macOS accepts 1016 bytes but not 1017
// (measured on this machine), so that is the limit the preview checks.
const renamePathMax = 1016

// splitName splits a file name into the base the rename rules act on and
// the extension that is always kept, as Finder treats it: the last dot
// starts the extension, except a leading dot alone (.gitignore) which is
// part of the name, not an extension.
func splitName(name string) (base, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

// planRename computes every row's new path and flags the conflicts that
// block 改名: a name or path over the limit, an illegal name, one item
// sitting inside another, a target another row also wants, a target
// already on disk that no row in the batch gives up, or a source that is
// no longer there. Names are compared case-insensitively and in NFC,
// because that is how APFS treats them.
func planRename(items []store.Item, p renameParams) renamePlan {
	pl := renamePlan{rows: make([]renameRow, 0, len(items))}
	// First pass: new names and the checks that need only the name.
	for i, it := range items {
		old := filepath.Base(it.Path)
		base, ext := splitName(old)
		// A folder's dots are part of its name, as Finder treats them:
		// only files have an extension to keep out of the rules.
		if fi, err := os.Stat(it.Path); err == nil && fi.IsDir() {
			base, ext = old, ""
		}
		nb := newBaseName(base, i, p)
		row := renameRow{item: it, oldPath: it.Path, oldName: old, newName: nb + ext}
		row.newPath = filepath.Join(filepath.Dir(it.Path), row.newName)
		switch {
		case nb == "":
			row.warn = "名字不能为空"
		case strings.ContainsAny(nb, "/:"):
			row.warn = "名字不能包含 / 或 :"
		case len(row.newName) > renameNameMax:
			row.warn = "名字过长"
		case len(row.newPath) > renamePathMax:
			row.warn = "路径太长"
		}
		pl.rows = append(pl.rows, row)
	}
	// Second pass: rows that cannot move together, batch-internal
	// duplicates and on-disk targets.
	for i, row := range pl.rows {
		if row.warn != "" {
			continue
		}
		// A folder and something inside it cannot be renamed at once:
		// the folder's move takes the inner item's path away with it, so
		// the inner move would neither land nor be restorable.
		for j, other := range pl.rows {
			if j == i || !pathInside(other.oldPath, row.oldPath) {
				continue
			}
			pl.rows[i].warn = "不能同时改文件夹和它里面的项目"
			if other.warn == "" {
				pl.rows[j].warn = "不能同时改文件夹和它里面的项目"
			}
			break
		}
		row = pl.rows[i]
		if row.warn != "" {
			continue
		}
		for j, other := range pl.rows {
			if j == i || !sameNameInDir(row.newPath, other.newPath) {
				continue
			}
			row.warn = "和本批其他项目重名"
			if other.warn == "" {
				pl.rows[j].warn = "和本批其他项目重名"
			}
			break
		}
		if row.warn == "" {
			row.warn = diskConflict(pl.rows, i)
		}
		pl.rows[i] = row
	}
	for i := range pl.rows {
		row := &pl.rows[i]
		if row.warn != "" {
			pl.conflicts++
			continue
		}
		if row.newPath == row.oldPath {
			continue
		}
		// The phase-1 temp name is longer than most targets, so a path
		// that only fits until the rename starts must be refused here —
		// the executor would fail on it after the plan said it was fine.
		if len(filepath.Dir(row.oldPath))+1+renameTempNameLen(len(pl.ops)) > renamePathMax {
			row.warn = "路径太长"
			pl.conflicts++
			continue
		}
		pl.changed++
		pl.ops = append(pl.ops, renameOp{src: row.oldPath, dst: row.newPath})
	}
	return pl
}

// normName folds a file name to Unicode NFC, the form APFS compares by.
func normName(s string) string { return norm.NFC.String(s) }

// sameNameInDir reports whether two paths are the same file name in the
// same directory, as APFS sees them: case-insensitively and after
// normalizing both names and their directories.
func sameNameInDir(a, b string) bool {
	if a == b {
		return true
	}
	da, db := filepath.Dir(a), filepath.Dir(b)
	if da != db && normName(filepath.Clean(da)) != normName(filepath.Clean(db)) {
		return false
	}
	return strings.EqualFold(normName(filepath.Base(a)), normName(filepath.Base(b)))
}

// pathInside reports whether child sits strictly inside parent.
func pathInside(parent, child string) bool {
	p, c := filepath.Clean(parent), filepath.Clean(child)
	if p == c {
		return false
	}
	if p == string(filepath.Separator) {
		return strings.HasPrefix(c, p)
	}
	return strings.HasPrefix(c, p+string(filepath.Separator))
}

// diskConflict reports why row self's target cannot be taken, "" when it
// is free. A missing source is flagged here so a vanished file fails the
// plan instead of the batch. A target already on disk is free only when
// it is one of this batch's own sources that is itself moving (a swap, or
// the row's own case-only rename), recognized by identity, not spelling.
func diskConflict(rows []renameRow, self int) string {
	row := rows[self]
	if _, err := os.Lstat(row.oldPath); err != nil {
		if os.IsNotExist(err) {
			return "原文件不存在"
		}
		return "无法读取原文件：" + err.Error()
	}
	if row.newPath == row.oldPath {
		return "" // unchanged: the name it has is its own
	}
	st, err := os.Lstat(row.newPath)
	if err != nil {
		return "" // target is free (or unreadable for another reason: exec fails cleanly)
	}
	// The target exists. It is only free when it is one of this batch's
	// own sources that will move — recognized by identity, not spelling,
	// so a case-only rename or a symlinked spelling still counts.
	for i, other := range rows {
		if !sameNameInDir(other.oldPath, row.newPath) || !sameFile(other.oldPath, st) {
			continue
		}
		if i == self || other.newPath != other.oldPath {
			return ""
		}
	}
	return "已存在同名文件"
}

// sameFile reports whether st is the same directory entry as path.
func sameFile(path string, st os.FileInfo) bool {
	cur, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return os.SameFile(st, cur)
}

// newBaseName computes one base name (extension excluded) under the mode.
func newBaseName(base string, index int, p renameParams) string {
	switch p.mode {
	case modeAdd:
		if p.addBefore {
			return p.addText + base
		}
		return base + p.addText
	case modeFormat:
		num := formatSeq(p.fmtStart+index, p.fmtDigits)
		switch {
		case p.fmtName == "":
			return num
		case p.fmtNumLast:
			return p.fmtName + " " + num
		default:
			return num + " " + p.fmtName
		}
	default: // modeReplace
		if p.find == "" {
			return base
		}
		return strings.ReplaceAll(base, p.find, p.replace)
	}
}

// formatSeq renders n zero-padded to digits places (clamped 1–4, n ≥ 0).
func formatSeq(n, digits int) string {
	if digits < 1 {
		digits = 1
	}
	if digits > 4 {
		digits = 4
	}
	if n < 0 {
		n = 0
	}
	return fmt.Sprintf("%0*d", digits, n)
}

const (
	// renameTempMarker starts every phase-1 temp name; the name is the
	// marker, renameTempRandomHex hex digits, a dash and the move's index.
	renameTempMarker    = ".shelf-renaming-"
	renameTempRandomHex = 16
)

// renameTempPrefix is the marker with the batch's random digits, so a
// leftover from a crashed run can never collide with a new batch.
func renameTempPrefix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return renameTempMarker
	}
	return renameTempMarker + hex.EncodeToString(b[:]) + "-"
}

// renameTempNameLen is the length of the temp name the i-th move gets;
// the preview checks the whole temp path against renamePathMax with it.
func renameTempNameLen(i int) int {
	return len(renameTempMarker) + renameTempRandomHex + 1 + len(strconv.Itoa(i))
}

// stagedMove is one op tracked through the two-phase executor.
type stagedMove struct {
	src, tmp, dst string
	staged        bool // the file sits at tmp
	done          bool // tmp→dst finished
}

// renameNoClobber moves src to dst without ever overwriting an existing
// entry: renameatx_np with RENAME_EXCL fails with EEXIST instead of
// replacing. It is a seam so tests can fail specific moves
// deterministically.
var renameNoClobber = func(src, dst string) error {
	const renameatxNp = 488 // RENAME_EXCL move, never overwrites
	const renameExcl = 0x4
	atFdcwd := ^uintptr(1) // AT_FDCWD = -2 as uintptr
	srcB, err := syscall.BytePtrFromString(src)
	if err != nil {
		return err
	}
	dstB, err := syscall.BytePtrFromString(dst)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(renameatxNp, atFdcwd,
		uintptr(unsafe.Pointer(srcB)), atFdcwd, uintptr(unsafe.Pointer(dstB)), renameExcl, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// execRename performs the ops in two phases — every file first to a
// unique temp name in its own directory, then to its target — so swaps
// and chains (a→b, b→c) cannot clobber each other. No move ever
// overwrites (renameNoClobber): a conflict or any other failure rolls
// every finished move back before the error is returned. ops must be a
// sane plan (callers pass renamePlan.ops); dst paths are expected free or
// belonging to the batch itself.
func execRename(ops []renameOp) error {
	if len(ops) == 0 {
		return nil
	}
	prefix := renameTempPrefix()
	moves := make([]stagedMove, len(ops))
	for i, op := range ops {
		// The temp name is short on purpose: a 250-byte source name must
		// still fit under NAME_MAX after the marker is added.
		moves[i] = stagedMove{src: op.src, dst: op.dst,
			tmp: filepath.Join(filepath.Dir(op.src), prefix+strconv.Itoa(i))}
	}
	// Phase 1: everyone to its temp name.
	for i := range moves {
		m := &moves[i]
		if err := renameNoClobber(m.src, m.tmp); err != nil {
			return rollback(moves, fmt.Errorf("无法移动「%s」：%s", filepath.Base(m.src), renameErr(err)))
		}
		m.staged = true
	}
	// Phase 2: temp names to targets.
	for i := range moves {
		m := &moves[i]
		if err := renameNoClobber(m.tmp, m.dst); err != nil {
			return rollback(moves, fmt.Errorf("无法命名为「%s」：%s", filepath.Base(m.dst), renameErr(err)))
		}
		m.staged, m.done = false, true
	}
	return nil
}

// rollback restores a failed batch. Finished targets first go back to
// their own temp names, newest first: a later op's target may be an
// earlier op's original name (the chain a→xa→xxa→xxxa), so returning
// straight home would hit a file still sitting there. Then every temp
// that holds a file returns to the name it started with. Moves that never
// began are neither touched nor reported. A rollback failure is reported
// alongside the original error; the file is left where it is, never
// overwritten away.
func rollback(moves []stagedMove, cause error) error {
	var stranded []string
	for i := len(moves) - 1; i >= 0; i-- {
		m := &moves[i]
		if !m.done {
			continue
		}
		if err := renameNoClobber(m.dst, m.tmp); err != nil {
			stranded = append(stranded, filepath.Base(m.dst))
			continue
		}
		m.done, m.staged = false, true
	}
	for i := len(moves) - 1; i >= 0; i-- {
		m := &moves[i]
		if !m.staged {
			continue
		}
		if err := renameNoClobber(m.tmp, m.src); err != nil {
			stranded = append(stranded, filepath.Base(m.tmp))
			continue
		}
		m.staged = false
	}
	if len(stranded) > 0 {
		return fmt.Errorf("%v；恢复原状时 %d 项未能改回：%s", cause, len(stranded), strings.Join(stranded, "、"))
	}
	return fmt.Errorf("%v，已全部恢复原状", cause)
}

// renameErr words the no-clobber rename's common failures in Chinese.
func renameErr(err error) string {
	switch {
	case errors.Is(err, syscall.EEXIST):
		return "已存在同名文件"
	case errors.Is(err, syscall.ENOENT):
		return "文件不存在"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "没有权限"
	case errors.Is(err, syscall.ENAMETOOLONG):
		return "名字过长"
	default:
		return err.Error()
	}
}
