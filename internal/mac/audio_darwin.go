//go:build darwin

package mac

// Default output device volume and mute through the CoreAudio hardware
// abstraction layer — the same scalar the volume keys and Control Center
// move. Strictly read-only: nothing here calls AudioObjectSetPropertyData,
// so the user's volume, mute and default device can never be changed.
// Listeners use the plain C callback, which only raises an atomic flag and
// touches no Objective-C, so delivery on a HAL thread is safe.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
)

// audioObjectPropertyAddress mirrors AudioObjectPropertyAddress: three
// UInt32s selecting a CoreAudio property.
type audioObjectPropertyAddress struct {
	selector uint32 // mSelector
	scope    uint32 // mScope
	element  uint32 // mElement
}

const (
	audioObjectSystemObject  = 1                                                                // kAudioObjectSystemObject
	audioPropertyElementMain = 0                                                                // kAudioObjectPropertyElementMain
	audioScopeOutput         = uint32('o')<<24 | uint32('p')<<16 | uint32('t')<<8 | uint32('p') // kAudioObjectPropertyScopeOutput
	audioScopeGlobal         = uint32('g')<<24 | uint32('l')<<16 | uint32('o')<<8 | uint32('b') // kAudioObjectPropertyScopeGlobal

	selectorDefaultOutputDevice = uint32('d')<<24 | uint32('O')<<16 | uint32('u')<<8 | uint32('t') // kAudioHardwarePropertyDefaultOutputDevice
	selectorVolumeScalar        = uint32('v')<<24 | uint32('o')<<16 | uint32('l')<<8 | uint32('m') // kAudioDevicePropertyVolumeScalar
	selectorMute                = uint32('m')<<24 | uint32('u')<<16 | uint32('t')<<8 | uint32('e') // kAudioDevicePropertyMute
)

var (
	libAudio uintptr

	audioGetPropertyData func(obj uint32, addr unsafe.Pointer, qualDataSize uint32, qualData unsafe.Pointer, dataSize *uint32, data unsafe.Pointer) int32
	audioAddListener     func(obj uint32, addr unsafe.Pointer, proc uintptr, clientData unsafe.Pointer) int32
	audioRemoveListener  func(obj uint32, addr unsafe.Pointer, proc uintptr, clientData unsafe.Pointer) int32

	audioOnce     sync.Once
	audioErr      error
	audioWatch    sync.Once
	audioDirty    atomic.Bool
	audioWatchOK  bool
	audioCB       uintptr
	watchedDevice uint32
)

// OutputState is one observation of the default output device.
type OutputState struct {
	OK    bool
	Level float32 // 0..1
	Muted bool
}

// ensureAudio opens CoreAudio once; a failure leaves the volume feature
// inert.
func ensureAudio() error {
	audioOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				audioErr = fmt.Errorf("mac: audio bindings: %v", r)
			}
		}()
		var err error
		if libAudio, err = purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
			audioErr = err
			return
		}
		reg := func(f any, name string) {
			if err == nil {
				purego.RegisterLibFunc(f, libAudio, name)
			}
		}
		reg(&audioGetPropertyData, "AudioObjectGetPropertyData")
		reg(&audioAddListener, "AudioObjectAddPropertyListener")
		reg(&audioRemoveListener, "AudioObjectRemovePropertyListener")
		if err != nil {
			audioErr = err
		}
	})
	return audioErr
}

// defaultOutputDevice returns the AudioObjectID of the current default
// output device, or 0 when there is none (headless Mac).
func defaultOutputDevice() uint32 {
	addr := audioObjectPropertyAddress{selector: selectorDefaultOutputDevice, scope: audioScopeGlobal, element: audioPropertyElementMain}
	var dev uint32
	size := uint32(unsafe.Sizeof(dev))
	if audioGetPropertyData(audioObjectSystemObject, unsafe.Pointer(&addr), 0, nil, &size, unsafe.Pointer(&dev)) != 0 {
		return 0
	}
	return dev
}

// outputFloat reads one Float32 property on the default output device.
func outputFloat(selector uint32) (float32, bool) {
	dev := defaultOutputDevice()
	if dev == 0 {
		return 0, false
	}
	addr := audioObjectPropertyAddress{selector: selector, scope: audioScopeOutput, element: audioPropertyElementMain}
	var v float32
	size := uint32(unsafe.Sizeof(v))
	if audioGetPropertyData(dev, unsafe.Pointer(&addr), 0, nil, &size, unsafe.Pointer(&v)) != 0 || size < 4 {
		return 0, false
	}
	return v, true
}

// outputFlag reads one UInt32 property on the default output device.
func outputFlag(selector uint32) (bool, bool) {
	dev := defaultOutputDevice()
	if dev == 0 {
		return false, false
	}
	addr := audioObjectPropertyAddress{selector: selector, scope: audioScopeOutput, element: audioPropertyElementMain}
	var v uint32
	size := uint32(unsafe.Sizeof(v))
	if audioGetPropertyData(dev, unsafe.Pointer(&addr), 0, nil, &size, unsafe.Pointer(&v)) != 0 || size < 4 {
		return false, false
	}
	return v != 0, true
}

// ReadOutput snapshots the default output device's volume scalar and mute
// flag. ok is false when no output device exists or it has no software
// volume (some HDMI devices report none). Main thread only.
func ReadOutput() OutputState {
	var s OutputState
	if err := ensureAudio(); err != nil {
		return s
	}
	v, ok := outputFloat(selectorVolumeScalar)
	if !ok {
		return s
	}
	s.OK = true
	s.Level = v
	if s.Level < 0 {
		s.Level = 0
	} else if s.Level > 1 {
		s.Level = 1
	}
	s.Muted, _ = outputFlag(selectorMute)
	return s
}

// watchOutputDevice installs the shared listener proc on the system
// object's default-device property and on dev's volume and mute
// properties, after removing it from whichever device was watched before.
// Main thread only.
func watchOutputDevice(dev uint32) {
	if watchedDevice == dev {
		return
	}
	sys := audioObjectPropertyAddress{selector: selectorDefaultOutputDevice, scope: audioScopeGlobal, element: audioPropertyElementMain}
	vol := audioObjectPropertyAddress{selector: selectorVolumeScalar, scope: audioScopeOutput, element: audioPropertyElementMain}
	mute := audioObjectPropertyAddress{selector: selectorMute, scope: audioScopeOutput, element: audioPropertyElementMain}
	if watchedDevice != 0 {
		audioRemoveListener(watchedDevice, unsafe.Pointer(&vol), audioCB, nil)
		audioRemoveListener(watchedDevice, unsafe.Pointer(&mute), audioCB, nil)
		audioRemoveListener(audioObjectSystemObject, unsafe.Pointer(&sys), audioCB, nil)
		watchedDevice = 0
	}
	if dev == 0 {
		return
	}
	audioAddListener(audioObjectSystemObject, unsafe.Pointer(&sys), audioCB, nil)
	// A device without a software volume reports errors for both of these;
	// the reads stay inert and the default-device listener still follows
	// the user back to a controllable device.
	audioAddListener(dev, unsafe.Pointer(&vol), audioCB, nil)
	audioAddListener(dev, unsafe.Pointer(&mute), audioCB, nil)
	watchedDevice = dev
}

// WatchOutput installs CoreAudio property listeners so every change to the
// default output device, its volume scalar or its mute flag sets the flag
// ConsumeOutputChanged hands out, and (re)arms them onto the current
// default device. Reports whether listeners were installed — false means
// the caller should fall back to slow polling. Main thread only.
func WatchOutput() bool {
	if err := ensureAudio(); err != nil {
		return false
	}
	audioWatch.Do(func() {
		audioCB = purego.NewCallback(func(_, _, _, _ uintptr) { audioDirty.Store(true) })
		watchOutputDevice(defaultOutputDevice())
		audioWatchOK = true
	})
	return audioWatchOK
}

// ConsumeOutputChanged reports whether an audio change arrived since the
// last call, clearing the flag either way. When the default device moved
// it also re-arms the listeners onto the new device. Main thread only.
func ConsumeOutputChanged() bool {
	if !audioDirty.Swap(false) {
		return false
	}
	if audioWatchOK {
		watchOutputDevice(defaultOutputDevice())
	}
	return true
}
