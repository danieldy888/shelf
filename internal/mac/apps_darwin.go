//go:build darwin

package mac

import (
	"os"
	"strings"
)

// NSApplicationActivationOptions, as NSRunningApplication expects them.
const (
	nsApplicationActivateAllWindows        = 1 << 0
	nsApplicationActivateIgnoringOtherApps = 1 << 1
)

// FrontmostApp returns the frontmost application's bundle identifier and
// localized name through NSWorkspace.frontmostApplication — the app the
// user is copying from. Both are "" when the system reports none (early
// launch, no GUI session). Read-only, no permission needed. Main thread
// only.
func FrontmostApp() (bundleID, name string) {
	withPool(func() {
		app := send(send(class("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
		if app == 0 {
			return
		}
		bundleID = goString(send(app, "bundleIdentifier"))
		name = goString(send(app, "localizedName"))
	})
	return bundleID, name
}

// FrontmostPID returns the process identifier of the frontmost
// application, 0 when the system reports none. Main thread only.
func FrontmostPID() int {
	var pid int
	withPool(func() {
		app := send(send(class("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
		if app == 0 {
			return
		}
		pid = sendInt(app, "processIdentifier")
	})
	return pid
}

// OwnPID returns this process's process identifier, for telling the
// frontmost application apart from Shelf itself. Main thread only.
func OwnPID() int { return os.Getpid() }

// AppActive reports whether this application is the active one
// (NSApplication.isActive). An accessory app that never activated anything
// still reports false while its panel holds the keyboard. Main thread only.
func AppActive() bool {
	var active bool
	withPool(func() {
		app := send(class("NSApplication"), "sharedApplication")
		if app == 0 {
			return
		}
		active = sendInt(app, "isActive") != 0
	})
	return active
}

// ActivateSelf brings this application to the front, so a window can take
// the keyboard and the user can type into it. Focus stealing: call only
// for a flow the user started explicitly (the clipboard hotkey). Main
// thread only.
func ActivateSelf() {
	withPool(func() {
		self := send(class("NSRunningApplication"), "currentApplication")
		if self != 0 {
			send(self, "activateWithOptions:", uintptr(nsApplicationActivateIgnoringOtherApps))
			return
		}
		send(send(class("NSApplication"), "sharedApplication"), "activateIgnoringOtherApps:", 1)
	})
}

// ActivateApp brings the application with the process identifier pid to
// the front and reports whether the system accepted the request. Used to
// hand the keyboard back to the app the user copied from. Main thread
// only.
func ActivateApp(pid int) bool {
	if pid <= 0 {
		return false
	}
	var ok bool
	withPool(func() {
		app := send(class("NSRunningApplication"), "runningApplicationWithProcessIdentifier:", uintptr(pid))
		if app == 0 {
			return
		}
		ok = sendInt(app, "activateWithOptions:", uintptr(nsApplicationActivateIgnoringOtherApps)) != 0
	})
	return ok
}

// DeactivateSelf resigns this application's active status by hiding it
// (NSApplication.hide:), used when the app to hand focus back to is gone.
// The shelf only activates for the keyboard-driven search flow; hiding
// afterwards leaves no key window and no frontmost shadow of a dead app.
// Main thread only.
func DeactivateSelf() {
	withPool(func() {
		send(send(class("NSApplication"), "sharedApplication"), "hide:", 0)
	})
}

// AppNameForBundleID resolves a display name for a bundle identifier: the
// name of the running application with that identifier, else the bundle's
// localized name from disk, else "". Read-only, no permission needed.
// Main thread only.
func AppNameForBundleID(bundleID string) string {
	if strings.TrimSpace(bundleID) == "" {
		return ""
	}
	var name string
	withPool(func() {
		apps := send(class("NSRunningApplication"), "runningApplicationsWithBundleIdentifier:",
			uintptr(nsString(bundleID)))
		if sendInt(apps, "count") > 0 {
			name = goString(send(send(apps, "objectAtIndex:", 0), "localizedName"))
		}
		if name != "" {
			return
		}
		url := send(send(class("NSWorkspace"), "sharedWorkspace"), "URLForApplicationWithBundleIdentifier:",
			uintptr(nsString(bundleID)))
		if url == 0 {
			return
		}
		info := send(send(class("NSBundle"), "bundleWithURL:", uintptr(url)), "localizedInfoDictionary")
		for _, key := range []string{"CFBundleDisplayName", "CFBundleName"} {
			if value := send(info, "objectForKey:", uintptr(nsString(key))); value != 0 {
				if name = goString(value); name != "" {
					return
				}
			}
		}
	})
	return name
}
