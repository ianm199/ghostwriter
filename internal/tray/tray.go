package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "bridge.h"
*/
import "C"
import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
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

var (
	panelOpen   bool
	panelMu     sync.Mutex
	panelTicker *time.Ticker
	panelDone   chan struct{}
)

func Setup() {
	C.TrayBridgeSetup()
}

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

//export goTrayOnTogglePanel
func goTrayOnTogglePanel() {
	panelMu.Lock()
	wasOpen := panelOpen
	panelOpen = !panelOpen
	panelMu.Unlock()

	C.TrayBridgeTogglePanel()

	if !wasOpen {
		go fetchPanelData()
		go startPanelRefresh()
	} else {
		stopPanelRefresh()
	}
}

//export goTrayOnSelectTranscript
func goTrayOnSelectTranscript(transcriptID *C.char) {
	id := C.GoString(transcriptID)
	go fetchTranscriptDetail(id)
}

//export goTrayOnQuit
func goTrayOnQuit() {
	go func() {
		stopPanelRefresh()
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

func fetchPanelData() {
	client, err := daemon.NewClient()
	if err != nil {
		log.Printf("tray: panel fetch connect failed: %v", err)
		return
	}
	transcripts, err := client.ListTranscripts(20)
	client.Close()
	if err != nil {
		log.Printf("tray: list transcripts failed: %v", err)
	} else {
		data, _ := json.Marshal(transcripts)
		cs := C.CString(string(data))
		C.TrayBridgeUpdateTranscripts(cs)
		C.free(unsafe.Pointer(cs))
	}

	evClient, err := daemon.NewClient()
	if err != nil {
		log.Printf("tray: panel fetch connect failed: %v", err)
		return
	}
	events, err := evClient.ListEvents()
	evClient.Close()
	if err != nil {
		log.Printf("tray: list events failed: %v", err)
	} else {
		data, _ := json.Marshal(events)
		cs := C.CString(string(data))
		C.TrayBridgeUpdateEvents(cs)
		C.free(unsafe.Pointer(cs))
	}
}

func fetchTranscriptDetail(id string) {
	client, err := daemon.NewClient()
	if err != nil {
		log.Printf("tray: detail fetch connect failed: %v", err)
		return
	}
	defer client.Close()

	detail, err := client.GetTranscript(id)
	if err != nil {
		log.Printf("tray: get transcript failed: %v", err)
		return
	}

	data, _ := json.Marshal(detail)
	cs := C.CString(string(data))
	C.TrayBridgeShowTranscriptDetail(cs)
	C.free(unsafe.Pointer(cs))
}

func startPanelRefresh() {
	panelMu.Lock()
	if panelTicker != nil {
		panelMu.Unlock()
		return
	}
	panelTicker = time.NewTicker(30 * time.Second)
	panelDone = make(chan struct{})
	ticker := panelTicker
	done := panelDone
	panelMu.Unlock()

	for {
		select {
		case <-ticker.C:
			panelMu.Lock()
			open := panelOpen
			panelMu.Unlock()
			if !open {
				return
			}
			fetchPanelData()
		case <-done:
			return
		}
	}
}

func stopPanelRefresh() {
	panelMu.Lock()
	defer panelMu.Unlock()
	if panelTicker != nil {
		panelTicker.Stop()
		panelTicker = nil
	}
	if panelDone != nil {
		close(panelDone)
		panelDone = nil
	}
}
