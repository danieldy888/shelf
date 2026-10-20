//go:build darwin

package main

// Batch rename (PLAN.md C): the pure logic — computing new names, checking
// conflicts, and the two-phase executor — lives here so unit tests can run
// it without a window. The window and menus are in rename.go and
// actions.go.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

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
// block 改名: an empty or illegal name, a name over the limit, a target
// another row also wants, a target already on disk that no row in the
// batch gives up, or a source that is no longer there. Names that collide
// are compared case-insensitively because the volumes users rename on
// almost always are.
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
		}
		pl.rows = append(pl.rows, row)
	}
	// Second pass: batch-internal duplicates and on-disk targets.
	// Second pass: batch-internal duplicates and on-disk targets. Rows
	// converge in pairs — flag both sides of a collision.
	for i, row := range pl.rows {
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
	for _, row := range pl.rows {
		if row.warn != "" {
			pl.conflicts++
			continue
		}
		if row.newPath != row.oldPath {
			pl.changed++
			pl.ops = append(pl.ops, renameOp{src: row.oldPath, dst: row.newPath})
		}
	}
	return pl
}

// sameNameInDir reports whether two paths are the same file name in the
// same directory, case-insensitively on the name (APFS's default).
func sameNameInDir(a, b string) bool {
	if a == b {
		return true
	}
	return filepath.Dir(a) == filepath.Dir(b) &&
		strings.EqualFold(filepath.Base(a), filepath.Base(b))
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

// renameTempPrefix is the marker the phase-1 temp names carry, random per
// batch so a leftover from a crashed run can never collide.
func renameTempPrefix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "shelf-renaming-"
	}
	return ".shelf-renaming-" + hex.EncodeToString(b[:]) + "-"
}

// stagedMove is one op tracked through the two-phase executor.
type stagedMove struct {
	src, tmp, dst string
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
			tmp: filepath.Join(filepath.Dir(op.src), fmt.Sprintf("%s%d", prefix, i))}
	}
	// Phase 1: everyone to its temp name.
	for i := range moves {
		m := &moves[i]
		if err := renameNoClobber(m.src, m.tmp); err != nil {
			return rollback(moves, fmt.Errorf("无法移动「%s」：%s", filepath.Base(m.src), renameErr(err)))
		}
	}
	// Phase 2: temp names to targets.
	for i := range moves {
		m := &moves[i]
		if err := renameNoClobber(m.tmp, m.dst); err != nil {
			return rollback(moves, fmt.Errorf("无法命名为「%s」：%s", filepath.Base(m.dst), renameErr(err)))
		}
		m.done = true
	}
	return nil
}

// rollback undoes the finished moves after a phase failed. Targets that
// already moved go back to their sources in reverse order first — a later
// op's dst may be an earlier op's src, so files return newest first —
// then the temps still holding files go home. A rollback failure is
// reported alongside the original error; the file is left where it is,
// never overwritten away.
func rollback(moves []stagedMove, cause error) error {
	var stranded []string
	for i := len(moves) - 1; i >= 0; i-- {
		m := &moves[i]
		if !m.done {
			continue
		}
		if err := renameNoClobber(m.dst, m.src); err != nil {
			stranded = append(stranded, filepath.Base(m.dst))
		}
	}
	for i := range moves {
		m := &moves[i]
		if m.done {
			continue
		}
		if err := renameNoClobber(m.tmp, m.src); err != nil {
			stranded = append(stranded, filepath.Base(m.tmp))
		}
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
