//go:build darwin

package mac

// 「框选文字」的屏幕录制权限：CGPreflightScreenCaptureAccess 只读查询，
// 不弹系统对话框；CGRequestScreenCaptureAccess 是用户主动发起的申请，
// 会把本 app 放进「系统设置 → 隐私与安全性 → 屏幕录制」的列表。
// 与辅助功能同一处理：ad-hoc 签名把授权钉在具体 build 上，升级后设置里
// 那条仍然勾选、却不再对新的 build 生效，并且会让系统不再弹框；所以
// 申请前先清掉本 app 的旧记录，让系统重新问一次。
import (
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

var preflightOnce sync.Once

var (
	cgPreflightScreenCaptureAccess func() bool
	cgRequestScreenCaptureAccess   func() bool
)

// tccutilResetDone limits the stale-record reset to the first failed
// preflight per process. It runs there — while the permission sheet is
// drawn, before the user can grant — because CGPreflightScreenCaptureAccess
// caches its answer for the process's lifetime: a user who grants in
// System Settings and then presses 「打开系统设置」 still reads false, so a
// reset on that click would wipe the grant it was meant to repair.
// tccutilReset and openURL are seams for the tests, which must never
// spawn either.
var (
	tccutilResetDone atomic.Bool
	tccutilReset     = func(id string) { _ = exec.Command("/usr/bin/tccutil", "reset", "ScreenCapture", id).Run() }
	openURL          = func(url string) { _ = exec.Command("/usr/bin/open", url).Start() }
	requestBundleID  = bundleIdentifier
)

// ScreenCapturePreflight reports whether this process may capture the
// screen right now. It never prompts and never clears anything, so UI that
// only displays the state can call it freely. Note macOS caches the answer
// for the lifetime of the process: a grant made while the app is running
// can keep answering false until the next launch.
func ScreenCapturePreflight() bool {
	if err := Init(); err != nil {
		return false
	}
	preflightOnce.Do(func() {
		purego.RegisterLibFunc(&cgPreflightScreenCaptureAccess, libCG, "CGPreflightScreenCaptureAccess")
		purego.RegisterLibFunc(&cgRequestScreenCaptureAccess, libCG, "CGRequestScreenCaptureAccess")
	})
	return cgPreflightScreenCaptureAccess()
}

// ScreenCaptureAllowed is the permission check used by the user-initiated
// request path. The first false answer clears any stale record of this app
// — one that grants nothing to the current build and suppresses the system
// prompt — once per process, before any grant could exist to lose.
func ScreenCaptureAllowed() bool {
	if ScreenCapturePreflight() {
		return true
	}
	if tccutilResetDone.CompareAndSwap(false, true) {
		if id := requestBundleID(); id != "" {
			// Fails harmlessly when there is no record yet (first grant).
			tccutilReset(id)
		}
	}
	return false
}

// RequestScreenCapture is the user-initiated path to grant the
// permission: macOS is asked to prompt or register the app, and System
// Settings is opened on the 屏幕录制 pane. The stale-record reset lives
// with the failed preflight (ScreenCaptureAllowed), so this call never
// wipes a grant the user just made. It reports whether the permission
// was already active; an already-trusted call leaves the settings page
// alone.
func RequestScreenCapture() bool {
	if ScreenCaptureAllowed() {
		return true
	}
	if cgRequestScreenCaptureAccess == nil {
		return false // Init failed; preflight could not have passed either
	}
	ok := cgRequestScreenCaptureAccess()
	// The request adds the app to the list and (never-asked builds) asks;
	// a build that was denied before is not re-prompted, so the pane is
	// opened either way and the user flips the switch there.
	openURL("x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")
	return ok
}
