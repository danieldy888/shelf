//go:build darwin

package main

// Batch rename (PLAN.md C): the 重新命名 window — Finder's "Rename N
// Items" for the tray's files. The window is a normal titled window like
// Settings: one instance, opened with the app active and the keyboard,
// and closing hands the keyboard back to whoever had it. Logic —
// name computation, conflicts, the safe two-phase move and rollback —
// lives in renamer.go; this file owns the window and its view.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shelf/internal/mac"
	"shelf/internal/store"
)

// renameWidth/renameHeight size the 重新命名 window: a normal titled
// window like Settings, roomy enough for the preview list.
const (
	renameWidth  = 480
	renameHeight = 470
)

// renameable reports whether an item can take part in a rename: only real
// files and folders still on disk. Shelf's own captures (screenshots,
// clipboard images, text) and its converted outputs in the data directory
// are never renamed — their paths are managed, not the user's.
func (a *shelf) renameable(it store.Item) bool {
	if it.Kind != store.KindFile || it.Missing || it.Converted || it.Path == "" {
		return false
	}
	if a.dataDir != "" && insideDataDir(a.dataDir, it.Path) {
		return false
	}
	return true
}

// insideDataDir reports whether p sits inside the data directory;
// symlinked spellings resolve first so a file reached through a link
// still counts as managed.
func insideDataDir(dir, p string) bool {
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		d = filepath.Clean(dir)
	}
	q, err := filepath.EvalSymlinks(p)
	if err != nil {
		q = filepath.Clean(p)
	}
	rel, err := filepath.Rel(d, q)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// the module can watch it while the window is up. Inputs live here, not
// in closures, so the view rebuild reads the latest text each frame.
type renameUI struct {
	host         *shelf
	win          *mygo.Window
	prevPID      int
	items        []store.Item
	mode         int // segmented index into renameMode
	find         string
	replace      string
	addText      string
	addSeg       int // 0 = 加在名称前, 1 = 名称后
	fmtName      string
	fmtStartText string
	digitsSeg    int // 0–3 → 1–4 位
	posSeg       int // 0 = 序号在名称前, 1 = 名称后
	scroll       ui.ScrollState
	booted       bool        // first frame focused the mode's first field
	done         *renameDone // last applied batch, while undo remains open
	err          string      // apply/undo failure shown inline
}

// renameDone remembers one applied batch for undo: the file moves in
// apply order and the item each path belongs to, so 撤销 can restore both
// disk names and tray paths.
type renameDone struct {
	ops []renameOp // src = name now on disk, dst = original name
	ids []string
}

// params reads the inputs into the planner's form. An unparsable 起始号
// is -1, which startOK rejects before 改名 is enabled.
func (r *renameUI) params() renameParams {
	p := renameParams{
		mode:       renameMode(r.mode),
		find:       r.find,
		replace:    r.replace,
		addText:    r.addText,
		addBefore:  r.addSeg == 0,
		fmtName:    r.fmtName,
		fmtDigits:  r.digitsSeg + 1,
		fmtNumLast: r.posSeg == 1,
	}
	if n, err := strconv.Atoi(r.fmtStartText); err == nil {
		p.fmtStart = n
	} else {
		p.fmtStart = -1
	}
	return p
}

// startOK reports whether 起始号 parses; the input defaults to "1" and
// any non-number shows as an inline hint and disables 改名.
func (r *renameUI) startOK() bool {
	n, err := strconv.Atoi(r.fmtStartText)
	return err == nil && n >= 0
}

// plan is the live preview of the current inputs over the chosen items.
func (r *renameUI) plan() renamePlan {
	return planRename(r.items, r.params())
}

// apply runs the previewed batch: disk moves first, then the tray paths.
// A disk failure already rolled back inside execRename; a store failure
// rolls the disk moves back so the tray never points at names that do not
// exist. On success the window shows its done state with 撤销.
func (r *renameUI) apply() {
	if r.win == nil || r.done != nil {
		return
	}
	pl := r.plan()
	if pl.conflicts > 0 || pl.changed == 0 {
		return
	}
	if err := execRename(pl.ops); err != nil {
		r.err = err.Error()
		return
	}
	updates := make(map[string]string, len(pl.ops))
	ids := make([]string, 0, len(pl.ops))
	for _, row := range pl.rows {
		if row.newPath == row.oldPath {
			continue
		}
		updates[row.item.ID] = row.newPath
		ids = append(ids, row.item.ID)
	}
	if _, err := r.host.store.UpdatePaths(updates); err != nil {
		undo := make([]renameOp, len(pl.ops))
		for i, op := range pl.ops {
			undo[i] = renameOp{src: op.dst, dst: op.src}
		}
		r.err = "无法保存托盘记录：" + err.Error()
		if rb := execRename(undo); rb != nil {
			r.err += "；" + rb.Error()
		}
		return
	}
	r.err = ""
	r.done = &renameDone{ops: pl.ops, ids: ids}
	for i, op := range pl.ops {
		r.done.ops[i] = renameOp{src: op.dst, dst: op.src} // undo order: current → original
	}
	r.host.toastStatus(fmt.Sprintf("已重新命名 %d 项", len(pl.ops)), time.Now().Add(10*time.Second), false)
	if r.win != nil {
		r.win.Invalidate()
	}
}

// undo moves the last batch's files back to their original names — never
// overwriting, so an original name taken since fails instead of losing
// the newer file — and returns the tray paths. Only the latest batch can
// be undone; a successful undo consumes it.
func (r *renameUI) undo() {
	if r.done == nil {
		return
	}
	if err := execRename(r.done.ops); err != nil {
		r.err = "无法撤销：" + err.Error()
		if r.win != nil {
			r.win.Invalidate()
		}
		return
	}
	updates := make(map[string]string, len(r.done.ops))
	for i, id := range r.done.ids {
		updates[id] = r.done.ops[i].dst // op.dst holds the original path
	}
	if _, err := r.host.store.UpdatePaths(updates); err != nil {
		// The files are back but the tray could not record it: put the
		// names back so tray and disk still agree, and keep 撤销 available.
		redo := make([]renameOp, len(r.done.ops))
		for i, op := range r.done.ops {
			redo[i] = renameOp{src: op.dst, dst: op.src}
		}
		r.err = "无法保存托盘记录：" + err.Error()
		if e := execRename(redo); e != nil {
			r.err += "；" + e.Error()
		}
		if r.win != nil {
			r.win.Invalidate()
		}
		return
	}
	n := len(r.done.ops)
	r.done = nil
	r.err = ""
	r.host.toastStatus(fmt.Sprintf("已撤销 %d 项重命名", n), time.Now().Add(4*time.Second), false)
	if r.win != nil {
		r.win.Invalidate()
	}
}

// openRename shows the 重新命名 window for items, or re-focuses the one
// already open — a single instance like Settings. The shelf releases its
// global Esc/Space while the window is key (the same handover Settings
// uses) and remembers the app to give the keyboard back to on close.
func (a *shelf) openRename(items []store.Item) {
	if len(items) == 0 || a.store == nil {
		return
	}
	if a.rn == nil {
		a.rn = &renameUI{host: a}
		a.modules = append(a.modules, renameModule{host: a})
	}
	r := a.rn
	r.items = items
	r.mode, r.find, r.replace = int(modeReplace), "", ""
	r.addText, r.addSeg = "", 1
	r.fmtName, r.fmtStartText, r.digitsSeg, r.posSeg = "", "1", 0, 1
	r.done, r.err, r.booted = nil, "", false
	r.prevPID = 0
	if pid := mac.FrontmostPID(); pid > 0 && pid != mac.OwnPID() {
		r.prevPID = pid
	}
	a.releaseKeys()
	if r.win != nil {
		mac.ActivateSelf()
		r.win.Show()
		r.win.Focus()
		return
	}
	frame := centeredFrame(renameWidth, renameHeight,
		mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea)
	w := mygo.NewWindow(mygo.WindowOptions{
		Title: renameTitle(len(items)), Width: renameWidth, Height: renameHeight,
		X: frame.X, Y: frame.Y,
		Hidden: true, BackgroundColor: "#1E1E1F",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 9, Y: 12},
		DisableResize:        true, DisableMaximize: true, DisableFullScreen: true,
		Content: ui.View(a.renameView),
	})
	mac.SetWindowAppearance(w.NativeHandle(), true)
	w.SetBounds(frame)
	w.OnClosed(a.renameClosed)
	r.win = w
	w.Show()
	mac.ActivateSelf()
	w.Focus()
}

// renameTitle names the window and the menu item.
func renameTitle(n int) string {
	if n > 1 {
		return fmt.Sprintf("重新命名 %d 项", n)
	}
	return "重新命名"
}

// centeredFrame centers a w×h window inside a display work area, as the
// Settings window centers itself.
func centeredFrame(w, h int, work mygo.Rectangle) mygo.Rectangle {
	x := work.X + (work.Width-w)/2
	y := work.Y + (work.Height-h)/2
	x = min(max(x, work.X), max(work.X, work.X+work.Width-w))
	y = min(max(y, work.Y), max(work.Y, work.Y+work.Height-h))
	return mygo.Rectangle{X: x, Y: y, Width: w, Height: h}
}

// renameModule keeps the shelf's global Esc/Space out of the rename
// window's reach: they are released when the window opens, but a click
// back on the panel would re-grab them, and a held Space never reaches a
// text field. tick puts them down again while the window is up.
type renameModule struct{ host *shelf }

func (renameModule) live() bool { return false }

func (m renameModule) tick(time.Time) {
	if r := m.host.rn; r != nil && r.win != nil {
		m.host.releaseKeys()
	}
}

// renameClosed mirrors settingsClosed: the pointer drops first, then the
// keyboard goes back to the panel (clipboard search), the shelf (panel
// open) or the app the user came from — never hiding this app.
func (a *shelf) renameClosed() {
	if a.rn != nil {
		a.rn.win = nil
	}
	if a.quitting {
		return
	}
	prev := a.rn.prevPID
	a.rn.prevPID = 0
	switch a.settingsKeyboardExit() {
	case settingsToPanel:
		if a.win != nil {
			a.win.Focus()
		}
		return
	case settingsToShelf:
		a.holdKeys()
	}
	a.handBackApp(prev)
}

// renameView draws the window each frame: the drawn title bar (centered
// like the Settings mock), the three-way segment, that mode's inputs, the
// live preview, and 取消/改名. The done state replaces the form with the
// result and 撤销/完成.
func (a *shelf) renameView(c *ui.Context) {
	c.SetTheme(settingsTheme)
	c.Root().Background(ui.Hex("#1E1E1F"))
	r := a.rn
	if r == nil || r.win == nil {
		return
	}
	c.OnShortcut(0, ui.KeyEscape, r.win.Close)
	c.OnShortcut(ui.Cmd, ui.KeyW, r.win.Close)
	applyOrClose := func() {
		if r.done != nil {
			r.win.Close()
		} else {
			r.apply()
		}
	}
	c.OnShortcut(0, ui.KeyEnter, applyOrClose)
	ui.Column(c.Key("win")).Fill().Children(func() {
		bar := c.TitleBar()
		ui.Row(c.Key("titlebar")).Height(float32(max(bar.Height, 38))).FillWidth().
			AlignItems(ui.Center).Justify(ui.Center).Padding(0, bar.Right, 0, bar.Left).
			DragWindow().Children(func() {
			ui.Text(c, renameTitle(len(r.items))).SingleLine().FontSize(13).FontWeight(600).TextColor(setInk2)
		})
		if r.done != nil {
			r.doneView(c)
			return
		}
		ui.Column(c.Key("body")).Padding(0, 16, 0, 16).Gap(10).Fill().Children(func() {
			if sg := ui.Segmented(c.Key("mode"), &r.mode, "替换文字", "添加文字", "名称加序号").
				FillWidth().Label("改名方式"); sg.Changed() {
				r.booted = false
			}
			r.inputSection(c)
			r.previewSection(c)
			if r.err != "" {
				ui.Text(c, r.err).FontSize(11).TextColor(danger).Padding(0, 2, 0, 2)
			}
			r.buttons(c, applyOrClose)
		})
	})
	r.booted = true
}

// inputSection renders the active mode's card: the settings-window row
// look (label left, control right) on a grouped card.
func (r *renameUI) inputSection(c *ui.Context) {
	title := "替换文字"
	switch renameMode(r.mode) {
	case modeAdd:
		title = "添加文字"
	case modeFormat:
		title = "名称加序号"
	}
	settingsSection(c, title, "", false, func() {
		switch renameMode(r.mode) {
		case modeReplace:
			settingsRow(c, "rn-find", "查找", func() {
				r.field(c, "rn-find-in", &r.find, "要替换的文字")
			})
			settingsSep(c, "rn-find")
			settingsRow(c, "rn-with", "替换为", func() {
				r.field(c, "rn-with-in", &r.replace, "替换后的文字")
			})
		case modeAdd:
			settingsRow(c, "rn-add", "文字", func() {
				r.field(c, "rn-add-in", &r.addText, "要添加的文字")
			})
			settingsSep(c, "rn-add")
			settingsRow(c, "rn-addpos", "位置", func() {
				ui.Segmented(c, &r.addSeg, "名称前", "名称后").Label("加在名称前或后")
			})
		default: // modeFormat
			settingsRow(c, "rn-name", "名称", func() {
				r.field(c, "rn-name-in", &r.fmtName, "自定名称（可留空）")
			})
			settingsSep(c, "rn-name")
			settingsRow(c, "rn-start", "起始号", func() {
				r.field(c, "rn-start-in", &r.fmtStartText, "1")
			})
			settingsSep(c, "rn-start")
			settingsRow(c, "rn-digits", "位数", func() {
				ui.Segmented(c, &r.digitsSeg, "1", "2", "3", "4").Label("序号位数")
			})
			settingsSep(c, "rn-digits")
			settingsRow(c, "rn-pos", "序号位置", func() {
				ui.Segmented(c, &r.posSeg, "名称前", "名称后").Label("序号在名称前或后")
			})
		}
	})
}

// field is one right-aligned single-line input inside a settingsRow:
// Enter applies the rename and Escape closes the window, the same keys
// the buttons answer — an input keeps them to itself otherwise.
func (r *renameUI) field(c *ui.Context, key string, value *string, placeholder string) {
	in := ui.TextInputBase(c.Key(key), value).Width(180).FontSize(13).TextColor(setInk).
		Placeholder(placeholder).Label(placeholder)
	if !r.booted && (renameMode(r.mode) == modeReplace && key == "rn-find-in" ||
		renameMode(r.mode) == modeAdd && key == "rn-add-in" ||
		renameMode(r.mode) == modeFormat && key == "rn-name-in") {
		in.Focus()
	}
	in.OnTextKey(func(k ui.TextKey) bool {
		switch k.Key {
		case ui.KeyEnter:
			r.apply()
			return true
		case ui.KeyEscape:
			if r.win != nil {
				r.win.Close()
			}
			return true
		}
		return false
	})
}

// previewSection is the live old → new list: conflicts render in danger
// red with their reason, unchanged rows dim. The list scrolls inside its
// card when the batch is long.
func (r *renameUI) previewSection(c *ui.Context) {
	pl := r.plan()
	// The preview takes whatever height the mode's card leaves, so the
	// format mode's four rows never push 改名 below the window: the list
	// scrolls inside its card instead.
	ui.Column(c.Key("sec-preview")).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Text(c, "预览").SingleLine().FontSize(11).FontWeight(600).TextColor(setInk3).Padding(0, 0, 6, 4)
		ui.Column(c.Key("card-preview")).Radius(10).Background(setCard).Border(0.5, setCardEdge).
			FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Scroll(c.Key("rn-preview")).Fill().MinHeight(0).TrackScroll(&r.scroll).Children(func() {
				ui.Column(c.Key("rn-rows")).FillWidth().Children(func() {
					for i, row := range pl.rows {
						if i > 0 {
							settingsSep(c, "rn-"+row.item.ID)
						}
						rowEl := ui.Row(c.Key("rn-row-"+row.item.ID)).AlignItems(ui.Center).Gap(8).
							MinHeight(30).Padding(0, 12).FillWidth()
						rowEl.Children(func() {
							ui.Text(c, row.oldName).SingleLine().FontSize(12).TextColor(setInk2)
							ui.Text(c, "→").SingleLine().FontSize(12).TextColor(setInk3)
							newColor := setInk
							if row.warn != "" {
								newColor = danger
							} else if row.newName == row.oldName {
								newColor = setInk3
							}
							ui.Text(c, row.newName).SingleLine().FontSize(12).FontWeight(600).TextColor(newColor)
							ui.Box(c).Grow(1)
							if row.warn != "" {
								ui.Text(c, row.warn).SingleLine().FontSize(11).TextColor(danger)
							}
						})
					}
				})
			})
		})
		// The status line under the card: how many rows change, or why
		// 改名 is locked.
		foot := ""
		switch {
		case !r.startOK() && renameMode(r.mode) == modeFormat:
			foot = "起始号需要是数字"
		case pl.conflicts > 0:
			foot = fmt.Sprintf("有 %d 项冲突，解决后才能改名", pl.conflicts)
		case pl.changed == 0:
			foot = "名字没有变化"
		default:
			foot = fmt.Sprintf("将重新命名 %d 项", pl.changed)
		}
		ui.Text(c, foot).FontSize(11).TextColor(setInk3).Padding(4, 12, 0, 12)
	})
}

// buttons is the footer row: 取消 on the left's right side pair as macOS
// dialogs do — 取消 secondary, 改名 the accented default (Enter), greyed
// while anything blocks the batch.
func (r *renameUI) buttons(c *ui.Context, apply func()) {
	pl := r.plan()
	blocked := pl.conflicts > 0 || pl.changed == 0 ||
		(renameMode(r.mode) == modeFormat && !r.startOK())
	ui.Row(c.Key("rn-buttons")).FillWidth().Justify(ui.End).Gap(8).Padding(4, 0, 0, 0).Children(func() {
		if ui.Button(c, "取消").Label("取消改名").Clicked() {
			r.win.Close()
		}
		if ui.Button(c, "改名").Label("应用改名").Disabled(blocked).Clicked() {
			apply()
		}
	})
}

// doneView replaces the form after a batch applied: the completion line
// with 撤销 (which restores the original names and stays available until
// used or a new batch applies) and 完成 to close. Enter/Esc close here.
func (r *renameUI) doneView(c *ui.Context) {
	ui.Column(c.Key("done")).Fill().Center().Gap(14).Children(func() {
		ui.Text(c, fmt.Sprintf("已重新命名 %d 项", len(r.done.ops))).FontSize(15).FontWeight(600).TextColor(setInk)
		ui.Text(c, "托盘中的路径已同步更新").FontSize(11).TextColor(setInk3)
		if r.err != "" {
			ui.Text(c, r.err).FontSize(11).TextColor(danger).Padding(0, 24, 0, 24)
		}
		ui.Row(c.Key("done-buttons")).Gap(8).Children(func() {
			if ui.Button(c, "撤销").Label("撤销这次改名").Clicked() {
				r.undo()
			}
			if ui.Button(c, "完成").Label("关闭改名窗口").Clicked() {
				r.win.Close()
			}
		})
	})
}
