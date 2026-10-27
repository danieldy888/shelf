//go:build darwin

package main

// An in-app update of the old Shelf replaced whatever bundle it found at
// Shelf.app with the new app. The renamed app is ShelfU.app, so a bundle
// that still lives at .../Shelf.app is moved to .../ShelfU.app on its first
// launch and reopened from there. Renaming a running bundle is safe: the
// process keeps running from the moved files, and the reopened instance
// owns the visible UI.

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	// oldBundleName is the bundle the 1.9.1 app installed and updated into.
	oldBundleName = "Shelf.app"
	// newBundleName is the bundle the renamed app ships as.
	newBundleName = "ShelfU.app"
)

// selfRenameTarget reports the rename an executable needs: the bundle it
// runs from and the bundle it should run from. ok is false unless exe is
// inside a bundle called Shelf.app, which is the only shape an old
// installed copy has.
func selfRenameTarget(exe string) (oldBundle, newBundle string, ok bool) {
	exe = filepath.Clean(exe)
	macOSDir := filepath.Dir(exe)
	contentsDir := filepath.Dir(macOSDir)
	bundle := filepath.Dir(contentsDir)
	if filepath.Base(macOSDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || filepath.Base(bundle) != oldBundleName {
		return "", "", false
	}
	return bundle, filepath.Join(filepath.Dir(bundle), newBundleName), true
}

// selfRenameBundle moves a .../Shelf.app bundle to .../ShelfU.app and
// reopens the app from there. It does nothing when the app does not run
// from such a bundle (go run, go test), when the renamed bundle already
// exists, or when the rename fails (a read-only directory, App
// Translocation); the app then keeps running from where it is.
func selfRenameBundle() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	oldBundle, nextBundle, ok := selfRenameTarget(exe)
	if !ok {
		return
	}
	if _, serr := os.Stat(nextBundle); serr == nil {
		return
	}
	if rerr := os.Rename(oldBundle, nextBundle); rerr != nil {
		log.Printf("%s: 无法把 %s 重命名为 %s，继续从原位置运行：%v", appName, oldBundle, nextBundle, rerr)
		return
	}
	args := append([]string{"-n", nextBundle, "--args"}, os.Args[1:]...)
	if oerr := exec.Command("/usr/bin/open", args...).Run(); oerr != nil {
		log.Printf("%s: 应用包已改名为 %s，但重新打开失败，继续从新位置运行：%v", appName, nextBundle, oerr)
		return
	}
	log.Printf("%s: 应用包已改名为 %s，正在重新打开", appName, nextBundle)
	os.Exit(0)
}
