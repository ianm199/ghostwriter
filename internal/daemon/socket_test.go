package daemon_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ianmclaughlin/ghostwriter/internal/daemon"
)

// Test: Socket IPC Round-Trip
func TestSocketRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	sock, err := daemon.NewSocketAt(socketPath)
	if err != nil {
		t.Fatalf("NewSocketAt failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands := sock.Listen(ctx)

	// Handle commands in background
	go func() {
		for cmd := range commands {
			switch cmd.Type {
			case daemon.CmdStatus:
				cmd.Reply <- daemon.Response{
					OK:     true,
					Status: &daemon.StatusInfo{State: daemon.StateIdle},
				}
			case daemon.CmdStartRecording:
				cmd.Reply <- daemon.Response{OK: true}
			default:
				cmd.Reply <- daemon.Response{OK: false, Error: "unknown"}
			}
		}
	}()

	// Give listener time to start
	time.Sleep(50 * time.Millisecond)

	// Test status command
	client, err := daemon.NewClientAt(socketPath)
	if err != nil {
		t.Fatalf("NewClientAt failed: %v", err)
	}
	status, err := client.Status()
	client.Close()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.State != daemon.StateIdle {
		t.Errorf("expected idle, got %s", status.State)
	}

	// Test start recording command
	client2, err := daemon.NewClientAt(socketPath)
	if err != nil {
		t.Fatalf("NewClientAt failed: %v", err)
	}
	err = client2.StartRecording("Test Meeting")
	client2.Close()
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}
}

// Test: Concurrent Socket Clients
func TestConcurrentSocketClients(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	sock, err := daemon.NewSocketAt(socketPath)
	if err != nil {
		t.Fatalf("NewSocketAt failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	commands := sock.Listen(ctx)

	// Handle commands
	go func() {
		for cmd := range commands {
			cmd.Reply <- daemon.Response{
				OK:     true,
				Status: &daemon.StatusInfo{State: daemon.StateIdle},
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Launch 10 concurrent clients
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := daemon.NewClientAt(socketPath)
			if err != nil {
				errors <- err
				return
			}
			defer c.Close()
			_, err = c.Status()
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent client error: %v", err)
	}
}
