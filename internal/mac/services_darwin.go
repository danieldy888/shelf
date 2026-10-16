//go:build darwin

package mac

// Finder 右键“服务/快速操作”与应用内文字服务的接收端。Info.plist 的
// NSServices 声明由 pbs（pasteboard server）汇总进服务菜单；用户调用时
// pbs 通过 NSPortName 对应的 mach 端口把请求送到已注册的 services
// provider，于是 Shelf 不用暴露快捷键或常驻监听也能收到“添加到 Shelf”。
// 主线程注册与派发；回调把剪贴板内容读成 Go 值后交给调用方。

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
	servicesOnce     sync.Once
	servicesErr      error
	servicesCallback func(paths []string, text string)
	servicesProvider id // kept alive for the process's lifetime
)

// serviceContent reduces the raw service payload to what the store wants:
// files first, text only when no usable file path arrived, mirroring the
// drop path's preference for the source's own representation.
func serviceContent(paths []string, text string) ([]string, string) {
	files := paths[:0]
	for _, p := range paths {
		if p != "" {
			files = append(files, p)
		}
	}
	if len(files) > 0 {
		return files, ""
	}
	return nil, text
}

// addToShelf serves the "添加到 Shelf" request (NSMessage addToShelf,
// selector addToShelf:userData:error:, type encoding v@:@@^@). pbs copies
// the caller's payload into this fresh pasteboard; it is read only here,
// never written back. Files beat text; an empty payload faults the request
// back to the caller through the NSError out-parameter.
func addToShelf(_ id, _ objc.SEL, pboard id, _ id, errOut *id) {
	var paths []string
	var text string
	withPool(func() {
		urls := send(pboard, "readObjectsForClasses:options:",
			uintptr(send(class("NSArray"), "arrayWithObject:", uintptr(objc.GetClass("NSURL")))),
			uintptr(send(class("NSDictionary"), "dictionaryWithObject:forKey:",
				uintptr(send(class("NSNumber"), "numberWithBool:", 1)),
				uintptr(nsString("NSPasteboardURLReadingFileURLsOnlyKey")))))
		for i := range sendInt(urls, "count") {
			u := send(urls, "objectAtIndex:", uintptr(i))
			if u == 0 || sendInt(u, "isFileURL") == 0 {
				continue
			}
			if p := goString(send(u, "path")); p != "" {
				paths = append(paths, p)
			}
		}
		if len(paths) == 0 {
			// sendFileTypes 是 Finder 的筛选条件，其他应用仍可能只声明
			// 经典文字类型，所以兜底逐个尝试常见纯文本类型。
			for _, t := range []string{
				"public.utf8-plain-text",
				"public.plain-text",
				"public.text",
				"NSStringPboardType",
			} {
				if s := goString(send(pboard, "stringForType:", uintptr(nsString(t)))); s != "" {
					text = s
					break
				}
			}
		}
	})
	paths, text = serviceContent(paths, text)
	if len(paths) == 0 && text == "" {
		if errOut != nil {
			desc := send(class("NSDictionary"), "dictionaryWithObject:forKey:",
				uintptr(nsString("Nothing to add to Shelf")),
				uintptr(nsString("NSLocalizedDescriptionKey")))
			*errOut = send(send(class("NSError"), "alloc"), "initWithDomain:code:userInfo:",
				uintptr(nsString("local.y3.shelf")), 1, uintptr(desc))
		}
		return
	}
	if cb := servicesCallback; cb != nil {
		cb(paths, text)
	}
}

// InstallServiceProvider registers the process as the handler for the
// "添加到 Shelf" service and routes each invocation to fn. fn receives the
// payload as Go values and must not keep Objective-C objects.
// setServicesProvider: installs the provider on this NSApplication; the
// explicit NSRegisterServicesProvider additionally vends the classic
// NSPortName-based endpoint the plist declares. When the app is not running
// the system launches it and delivers once the provider registers.
// Main thread only; call after the application has started.
func InstallServiceProvider(fn func(paths []string, text string)) error {
	if err := Init(); err != nil {
		return err
	}
	servicesOnce.Do(func() {
		cls, err := objc.RegisterClass("ShelfServicesProvider", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: sel("addToShelf:userData:error:"), Fn: addToShelf},
		})
		if err != nil {
			servicesErr = fmt.Errorf("mac: services provider class: %w", err)
			return
		}
		servicesProvider = send(id(cls), "new")
		if servicesProvider == 0 {
			servicesErr = errors.New("mac: cannot allocate services provider")
			return
		}
		servicesCallback = fn
		send(send(class("NSApplication"), "sharedApplication"), "setServicesProvider:", uintptr(servicesProvider))
		var registerProvider func(provider id, portName id)
		purego.RegisterLibFunc(&registerProvider, libAppKit, "NSRegisterServicesProvider")
		registerProvider(servicesProvider, nsString("Shelf"))
		var updateServices func()
		purego.RegisterLibFunc(&updateServices, libAppKit, "NSUpdateDynamicServices")
		updateServices()
	})
	return servicesErr
}
