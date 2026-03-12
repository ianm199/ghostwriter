package pidlock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Lock manages a PID file to ensure only one instance of a process runs.
type Lock struct {
	path string
}

// New creates a Lock that uses the given name to derive a PID file path
// at /tmp/<name>.pid.
func New(name string) *Lock {
	return &Lock{
		path: filepath.Join(os.TempDir(), name+".pid"),
	}
}

// Check returns an error if another process already holds the lock.
func (l *Lock) Check() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return nil
	}
	return fmt.Errorf("already running (PID %d)", pid)
}

// Acquire writes the current process PID to the lock file.
// Call Check first to avoid overwriting an active lock.
func (l *Lock) Acquire() error {
	return os.WriteFile(l.path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

// Release removes the PID file.
func (l *Lock) Release() {
	os.Remove(l.path)
}
