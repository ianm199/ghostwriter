//go:build darwin

package audiocapture

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework ScreenCaptureKit -framework CoreMedia -framework AVFoundation -framework Foundation -framework AppKit
#include <stdlib.h>
#include "sckit_bridge.h"
*/
import "C"
import "unsafe"

func scKitEnsureAppInit() {
	C.SCKitBridgeEnsureAppInit()
}

func scKitRunMainLoop() {
	C.SCKitBridgeRunMainLoop()
}

func scKitQuitMainLoop() {
	C.SCKitBridgeQuitMainLoop()
}

func scKitIsAvailable() bool {
	return bool(C.SCKitBridgeIsAvailable())
}

func scKitHasPermission() bool {
	return bool(C.SCKitBridgeHasPermission())
}

func scKitStartCapture(appName string) error {
	var cName *C.char
	if appName != "" {
		cName = C.CString(appName)
		defer C.free(unsafe.Pointer(cName))
	}

	rc := C.SCKitBridgeStartCapture(cName)
	if rc != 0 {
		return errSCKitStartFailed
	}
	return nil
}

func scKitStopCapture() (AudioData, error) {
	buf := C.SCKitBridgeStopCapture()
	defer C.SCKitBridgeFreeBuffer(buf)

	if buf.samples == nil || buf.sampleCount == 0 {
		return AudioData{}, errSCKitNoAudio
	}

	count := int(buf.sampleCount)
	samples := make([]float32, count)
	cSlice := unsafe.Slice((*float32)(unsafe.Pointer(buf.samples)), count)
	copy(samples, cSlice)

	return AudioData{
		Samples:    samples,
		SampleRate: int(buf.sampleRate),
		Channels:   int(buf.channels),
	}, nil
}
