//go:build darwin

package mac

import (
	"testing"
)

// The stale-record reset runs on the first failed preflight, before the
// user can grant — never on a 「打开系统设置」 click:
// CGPreflightScreenCaptureAccess caches its false answer until relaunch,
// so a click after the user granted in Settings still reads false, and a
// reset there would wipe the grant it was meant to repair. The seams
// stub every spawned binary and the CG calls.
func TestScreenCaptureResetRunsOnFailedPreflight(t *testing.T) {
	var resets, opens, requests int
	oldReset, oldOpen, oldID := tccutilReset, openURL, requestBundleID
	oldPre, oldReq := cgPreflightScreenCaptureAccess, cgRequestScreenCaptureAccess
	oldDone := tccutilResetDone.Swap(true)
	preflightOnce.Do(func() {}) // consume the registration once; the stubs below stand in
	tccutilReset = func(string) { resets++ }
	openURL = func(string) { opens++ }
	requestBundleID = func() string { return "com.test.shelf" }
	cgPreflightScreenCaptureAccess = func() bool { return false }
	cgRequestScreenCaptureAccess = func() bool { requests++; return false }
	tccutilResetDone.Store(false)
	t.Cleanup(func() {
		tccutilReset, openURL, requestBundleID = oldReset, oldOpen, oldID
		cgPreflightScreenCaptureAccess, cgRequestScreenCaptureAccess = oldPre, oldReq
		tccutilResetDone.Store(oldDone)
	})

	if ScreenCaptureAllowed() {
		t.Fatal("untrusted stub reported trusted")
	}
	if resets != 1 {
		t.Fatalf("failed preflight did not reset the stale record: resets=%d", resets)
	}
	// Same process, user grants in Settings then clicks 打开系统设置:
	// preflight still answers false (process cache) — the request asks
	// and opens the pane but never resets again.
	if RequestScreenCapture() {
		t.Fatal("untrusted stub reported trusted")
	}
	if resets != 1 {
		t.Fatalf("the grant button reset the fresh grant: resets=%d", resets)
	}
	if requests != 1 || opens != 1 {
		t.Fatalf("request skipped request/open: requests=%d opens=%d", requests, opens)
	}
}

// An already-trusted process never resets, asks or opens anything.
func TestRequestScreenCaptureTrustedShortCircuits(t *testing.T) {
	var resets, opens, requests int
	oldReset, oldOpen := tccutilReset, openURL
	oldPre, oldReq := cgPreflightScreenCaptureAccess, cgRequestScreenCaptureAccess
	preflightOnce.Do(func() {})
	tccutilReset = func(string) { resets++ }
	openURL = func(string) { opens++ }
	cgPreflightScreenCaptureAccess = func() bool { return true }
	cgRequestScreenCaptureAccess = func() bool { requests++; return true }
	t.Cleanup(func() {
		tccutilReset, openURL = oldReset, oldOpen
		cgPreflightScreenCaptureAccess, cgRequestScreenCaptureAccess = oldPre, oldReq
	})

	if !RequestScreenCapture() {
		t.Fatal("trusted stub reported untrusted")
	}
	if resets != 0 || requests != 0 || opens != 0 {
		t.Fatalf("trusted path ran side effects: resets=%d requests=%d opens=%d", resets, requests, opens)
	}
}
