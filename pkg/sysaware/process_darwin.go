//go:build darwin

package sysaware

import (
	"os/exec"
	"strings"
)

type DarwinProcessChecker struct{}

func NewDarwinProcessChecker() *DarwinProcessChecker {
	return &DarwinProcessChecker{}
}

func (d *DarwinProcessChecker) IsRunning(name string) bool {
	out, err := exec.Command("ps", "-eo", "comm").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

func (d *DarwinProcessChecker) RunningProcesses() []string {
	out, err := exec.Command("ps", "-eo", "comm").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(out), "\n")
	var procs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			procs = append(procs, line)
		}
	}
	return procs
}
