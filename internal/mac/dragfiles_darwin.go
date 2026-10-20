//go:build darwin

package mac

import "github.com/ebitengine/purego/objc"

// DragFiles returns the file paths of the drag session in flight, read from
// the drag pasteboard (NSDragPboard) while the drag is still hovering — mygo
// hands a destination the session's formats only, so the drop-zone picker
// that must know the file kind before the drop reads the board itself.
//
// Two representations carry paths before the mouse releases. Per item a
// public.file-url/public.url NSURL is what writeObjects:-style sources
// (Finder, other apps' NSDraggingItem sessions) vend eagerly; the aggregate
// NSFilenamesPboardType string list is the classic Finder fallback. Promised
// or remote items vend no URL until the drop, so their absence from the
// result is normal, not an error — callers fall back to reading the dropped
// transfer's paths.
//
// The board keeps a completed session's contents, so a call outside a drag
// (DragState().active == false) answers yesterday's files; the caller gates
// on DragState and the pointer being over the panel. Read-only and off the
// drop path. Main thread only.
func DragFiles() []string {
	if err := Init(); err != nil {
		return nil
	}
	return dragFilesFrom(dragPasteboard())
}

// dragFilesFrom reads board's file paths; split out so tests can point a
// scratch pasteboard at it. Call inside the caller's withPool.
func dragFilesFrom(board id) []string {
	var paths []string
	withPool(func() {
		// readObjectsForClasses:NSURL with FileURLsOnly walks the items'
		// file-url and URL types and decodes to file paths in board order.
		urls := send(board, "readObjectsForClasses:options:",
			uintptr(send(class("NSArray"), "arrayWithObject:", uintptr(objc.GetClass("NSURL")))),
			uintptr(send(class("NSDictionary"), "dictionaryWithObject:forKey:",
				uintptr(send(class("NSNumber"), "numberWithBool:", 1)),
				uintptr(nsString("NSPasteboardURLReadingFileURLsOnlyKey")))))
		for i := range sendInt(urls, "count") {
			if u := send(urls, "objectAtIndex:", uintptr(i)); u != 0 && sendInt(u, "isFileURL") != 0 {
				if p := dragURLPath(u); p != "" {
					paths = append(paths, p)
				}
			}
		}
		// NSFilenamesPboardType is the classic array-of-paths some sources
		// (older Finder sessions among them) vend instead of per-item URLs.
		// Entries already seen through a file URL are skipped; anything
		// else is appended in order.
		if list := send(board, "propertyListForType:", uintptr(nsString("NSFilenamesPboardType"))); list != 0 {
			if send(list, "isKindOfClass:", uintptr(objc.GetClass("NSArray"))) != 0 {
				for i := range sendInt(list, "count") {
					if p := goString(send(list, "objectAtIndex:", uintptr(i))); p != "" {
						paths = append(paths, p)
					}
				}
			}
		}
	})
	return dedupePaths(paths)
}

// dragURLPath resolves an NSURL known to be a file URL to its path. The
// file:///.file/id=… references Finder drags expose resolve lazily through
// filePathURL; the plain fileURLWithPath: kind answers path directly.
func dragURLPath(u id) string {
	if resolved := send(u, "filePathURL"); resolved != 0 && resolved != u {
		u = resolved
	}
	return goString(send(u, "path"))
}

// dedupePaths keeps the first spelling of each path: the two
// representations above can both carry the same file, and a duplicated
// path would double an action's inputs.
func dedupePaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}
	seen := make(map[string]bool, len(paths))
	out := paths[:0]
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
