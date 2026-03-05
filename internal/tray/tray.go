package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "bridge.h"
*/
import "C"
import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
)

const (
	stateOffline    = 0
	stateIdle       = 1
	stateRecording  = 2
	stateProcessing = 3
)

func Run() {
	runtime.LockOSThread()
	C.TrayBridgeRun()
}

//export goTrayOnReady
func goTrayOnReady() {
	go pollDaemon()
}

//export goTrayOnStart
func goTrayOnStart() {
	go func() {
		client, err := daemon.NewClient()
		if err != nil {
			log.Printf("tray: connect failed: %v", err)
			return
		}
		defer client.Close()
		if err := client.StartRecording("Manual recording"); err != nil {
			log.Printf("tray: start failed: %v", err)
		}
		updateStatus()
	}()
}

//export goTrayOnStop
func goTrayOnStop() {
	go func() {
		client, err := daemon.NewClient()
		if err != nil {
			log.Printf("tray: connect failed: %v", err)
			return
		}
		defer client.Close()
		if err := client.StopRecording(); err != nil {
			log.Printf("tray: stop failed: %v", err)
		}
		updateStatus()
	}()
}

//export goTrayOnOpenTranscripts
func goTrayOnOpenTranscripts() {
	go func() {
		home, _ := os.UserHomeDir()
		exec.Command("open", filepath.Join(home, "Documents", "Ghostwriter")).Start()
	}()
}

//export goTrayOnQuit
func goTrayOnQuit() {
	go func() {
		C.TrayBridgeQuit()
	}()
}

func pollDaemon() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	updateStatus()
	for range ticker.C {
		updateStatus()
	}
}

func updateStatus() {
	client, err := daemon.NewClient()
	if err != nil {
		setStatus("Daemon offline", stateOffline)
		return
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		setStatus("Daemon offline", stateOffline)
		return
	}

	switch status.State {
	case daemon.StateIdle:
		setStatus("Idle", stateIdle)
	case daemon.StateRecording:
		text := "Recording"
		if status.CurrentMeeting != "" {
			text = status.CurrentMeeting
		}
		if status.Duration != "" {
			text = text + "  " + status.Duration
		}
		setStatus(text, stateRecording)
	case daemon.StateProcessing:
		setStatus("Transcribing...", stateProcessing)
	}
}

func setStatus(text string, state int) {
	cs := C.CString(text)
	C.TrayBridgeUpdateStatus(cs, C.int(state))
	C.free(unsafe.Pointer(cs))
}
