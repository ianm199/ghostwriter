// Package sysaware provides system awareness primitives for desktop
// applications: process monitoring and microphone state detection.
package sysaware

type ProcessChecker interface {
	IsRunning(name string) bool
	RunningProcesses() []string
}
