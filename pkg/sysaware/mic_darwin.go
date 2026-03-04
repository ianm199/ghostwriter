//go:build darwin

package sysaware

import mediadevicesstate "github.com/antonfisher/go-media-devices-state"

type DarwinMicDetector struct{}

func NewDarwinMicDetector() *DarwinMicDetector {
	return &DarwinMicDetector{}
}

func (d *DarwinMicDetector) IsActive() bool {
	on, err := mediadevicesstate.IsMicrophoneOn()
	if err != nil {
		return false
	}
	return on
}
