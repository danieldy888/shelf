//go:build darwin

package main

// Tests for the decision behind the bundle self-rename: only an executable
// inside Shelf.app moves, and the new bundle path is always a sibling of
// the old one. Nothing here touches a real installed app.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelfRenameTarget(t *testing.T) {
	cases := []struct {
		exe     string
		old     string
		next    string
		wantHit bool
	}{
		// An in-app update of the old app leaves the new bundle at Shelf.app.
		{"/Applications/Shelf.app/Contents/MacOS/ShelfU", "/Applications/Shelf.app", "/Applications/ShelfU.app", true},
		// The old app's own binary in the same place.
		{"/Applications/Shelf.app/Contents/MacOS/Shelf", "/Applications/Shelf.app", "/Applications/ShelfU.app", true},
		{"/Users/me/Applications/Shelf.app/Contents/MacOS/ShelfU", "/Users/me/Applications/Shelf.app", "/Users/me/Applications/ShelfU.app", true},
		// Already renamed: nothing to do.
		{"/Applications/ShelfU.app/Contents/MacOS/ShelfU", "", "", false},
		// go run and go test build into a temporary directory.
		{"/var/folders/xy/T/go-build123/b001/exe/shelf", "", "", false},
		// A bare executable, and a directory that only looks like a bundle.
		{"/tmp/Shelf", "", "", false},
		{"/tmp/Shelf.app/Contents/ShelfU", "", "", false},
		{"/tmp/Shelf.app/MacOS/ShelfU", "", "", false},
	}
	for _, tc := range cases {
		old, next, ok := selfRenameTarget(tc.exe)
		if ok != tc.wantHit || old != tc.old || next != tc.next {
			t.Errorf("selfRenameTarget(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.exe, old, next, ok, tc.old, tc.next, tc.wantHit)
		}
	}
}

// TestSelfRenameTargetIsSiblingOfTheOldBundle pins the property the rename
// relies on: the new bundle is created in the same directory, so the rename
// stays on one volume.
func TestSelfRenameTargetIsSiblingOfTheOldBundle(t *testing.T) {
	old, next, ok := selfRenameTarget("/Applications/Shelf.app/Contents/MacOS/ShelfU")
	if !ok {
		t.Fatal("an exe inside Shelf.app was not recognised")
	}
	if filepath.Dir(old) != filepath.Dir(next) {
		t.Errorf("the new bundle %q is not next to %q", next, old)
	}
	if filepath.Base(next) != newBundleName || filepath.Base(old) != oldBundleName {
		t.Errorf("bundle names = %q, %q; want %q, %q", oldBundleName, newBundleName, filepath.Base(old), filepath.Base(next))
	}
}

// TestSelfRenameBundleIgnoresNonBundles checks the wrapper used at startup:
// under go test the executable is not in an .app, so the call is a no-op
// and leaves the process running.
func TestSelfRenameBundleIgnoresNonBundles(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path:", err)
	}
	if _, _, ok := selfRenameTarget(exe); ok {
		t.Skipf("the test binary runs from %s, inside Shelf.app", exe)
	}
	selfRenameBundle() // must return without exiting
}
