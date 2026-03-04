package sysaware

type ProcessChecker interface {
	IsRunning(name string) bool
	RunningProcesses() []string
}
