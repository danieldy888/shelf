//go:build darwin

package mac

import "github.com/ebitengine/purego/objc"

// ObserveMouseDown observes all mouse buttons without consuming events.
// Global mouse-only monitors require no Accessibility permission. Local
// events are returned unchanged. Install and stop on the AppKit main thread.
func ObserveMouseDown(window uintptr, clicked func(inside bool)) (stop func()) {
	const mask = 1<<1 | 1<<3 | 1<<25
	globalBlock := objc.NewBlock(func(_ objc.Block, _ id) { clicked(false) })
	defer globalBlock.Release()
	localBlock := objc.NewBlock(func(_ objc.Block, event id) id {
		clicked(uintptr(send(event, "window")) == window)
		return event
	})
	defer localBlock.Release()
	global := send(send(class("NSEvent"), "addGlobalMonitorForEventsMatchingMask:handler:", mask, uintptr(globalBlock)), "retain")
	local := send(send(class("NSEvent"), "addLocalMonitorForEventsMatchingMask:handler:", mask, uintptr(localBlock)), "retain")
	return func() {
		if global != 0 {
			send(class("NSEvent"), "removeMonitor:", uintptr(global))
			send(global, "release")
			global = 0
		}
		if local != 0 {
			send(class("NSEvent"), "removeMonitor:", uintptr(local))
			send(local, "release")
			local = 0
		}
	}
}
