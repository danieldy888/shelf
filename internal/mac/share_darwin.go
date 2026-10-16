//go:build darwin

package mac

import (
	"errors"
)

// NSSharingServiceNameSendViaAirDrop, the services-menu identifier for
// AirDrop (com.apple.share.AirDrop.send).
const shareServiceAirDrop = "com.apple.share.AirDrop.send"

// AirDrop opens the system AirDrop sheet for the given files and texts.
// Empty input is an error. Returns an error when AirDrop is unavailable.
// Paths are wrapped as file URLs; texts are shared as plain strings. The
// sheet is system-owned: delivery happens (or not) after this returns.
// Main thread only.
func AirDrop(paths []string, texts []string) error {
	if err := Init(); err != nil {
		return err
	}
	if len(paths) == 0 && len(texts) == 0 {
		return errors.New("mac: AirDrop needs at least one file or text")
	}
	var shareErr error
	withPool(func() {
		service := send(class("NSSharingService"), "sharingServiceNamed:", uintptr(nsString(shareServiceAirDrop)))
		if service == 0 {
			shareErr = errors.New("mac: AirDrop sharing service unavailable")
			return
		}
		items := send(send(class("NSMutableArray"), "alloc"), "initWithCapacity:", uintptr(len(paths)+len(texts)))
		items = send(items, "autorelease")
		for _, p := range paths {
			url := send(class("NSURL"), "fileURLWithPath:", uintptr(nsString(p)))
			if url != 0 {
				send(items, "addObject:", uintptr(url))
			}
		}
		for _, t := range texts {
			send(items, "addObject:", uintptr(nsString(t)))
		}
		if sendInt(items, "count") == 0 {
			shareErr = errors.New("mac: AirDrop items are all unusable")
			return
		}
		if send(service, "canPerformWithItems:", uintptr(items)) == 0 {
			shareErr = errors.New("mac: AirDrop cannot share these items")
			return
		}
		// performWithItems: hands the sheet to the system asynchronously;
		// retain the service so it survives this call's autorelease pool
		// (the service outlives its sheet — one retained object per call).
		send(service, "retain")
		send(service, "performWithItems:", uintptr(items))
	})
	return shareErr
}
