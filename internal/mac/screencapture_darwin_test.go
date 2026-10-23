//go:build darwin

package mac

import (
	"testing"
)

// RequestScreenCapture may run tccutil reset once per process, on the
// first untrusted request only. CGPreflightScreenCaptureAccess caches its
// false answer until relaunch, so a second 「打开系统设置」 click in the
// same run reaches here again — resetting there would wipe the grant the
// user just made. The seams stub every spawned binary and the CG calls.
func TestRequestScreenCaptureResetsOncePerProcess(t *testing.T) {
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

	if RequestScreenCapture() {
		t.Fatal("untrusted stub reported trusted")
	}
	if resets != 1 || requests != 1 || opens != 1 {
		t.Fatalf("first request: resets=%d requests=%d opens=%d", resets, requests, opens)
	}
	// Same process, user clicks again after granting in Settings:
	// preflight still answers false (process cache) — no second reset.
	if RequestScreenCapture() {
		t.Fatal("untrusted stub reported trusted")
	}
	if resets != 1 {
		t.Fatalf("second request reset again: resets=%d", resets)
	}
	if requests != 2 || opens != 2 {
		t.Fatalf("second request skipped request/open: requests=%d opens=%d", requests, opens)
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
