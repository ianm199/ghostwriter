package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type CommandType string

const (
	CmdStartRecording CommandType = "start_recording"
	CmdStopRecording  CommandType = "stop_recording"
	CmdStatus         CommandType = "status"
	CmdStop           CommandType = "stop"
)

type Command struct {
	Type  CommandType `json:"type"`
	Title string     `json:"title,omitempty"`
	Reply chan Response
}

type Response struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error,omitempty"`
	Status *StatusInfo `json:"status,omitempty"`
}

type StatusInfo struct {
	State          State  `json:"state"`
	CurrentMeeting string `json:"current_meeting,omitempty"`
	Duration       string `json:"duration,omitempty"`
}

type Socket struct {
	listener net.Listener
	path     string
}

func socketPath() string {
	return filepath.Join(os.TempDir(), "ghostwriter.sock")
}

func NewSocket() (*Socket, error) {
	path := socketPath()
	os.Remove(path)

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", path, err)
	}

	return &Socket{listener: listener, path: path}, nil
}

// Listen accepts commands from CLI clients over the unix domain socket.
func (s *Socket) Listen(ctx context.Context) <-chan Command {
	commands := make(chan Command, 8)

	go func() {
		defer s.listener.Close()
		defer os.Remove(s.path)

		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go s.handleConnection(conn, commands)
		}
	}()

	return commands
}

func (s *Socket) handleConnection(conn net.Conn, commands chan<- Command) {
	defer conn.Close()

	var req struct {
		Type  CommandType `json:"type"`
		Title string     `json:"title,omitempty"`
	}

	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	reply := make(chan Response, 1)
	commands <- Command{Type: req.Type, Title: req.Title, Reply: reply}

	resp := <-reply
	json.NewEncoder(conn).Encode(resp)
}

// Client connects to the daemon's control socket.
type Client struct {
	conn net.Conn
}

func NewClient() (*Client, error) {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) send(cmd CommandType, title string) (Response, error) {
	req := struct {
		Type  CommandType `json:"type"`
		Title string     `json:"title,omitempty"`
	}{Type: cmd, Title: title}

	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return Response{}, err
	}

	var resp Response
	if err := json.NewDecoder(c.conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func (c *Client) StartRecording(title string) error {
	resp, err := c.send(CmdStartRecording, title)
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (c *Client) StopRecording() error {
	resp, err := c.send(CmdStopRecording, "")
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}

func (c *Client) Status() (*StatusInfo, error) {
	resp, err := c.send(CmdStatus, "")
	if err != nil {
		return nil, err
	}
	return resp.Status, nil
}

func (c *Client) Stop() error {
	resp, err := c.send(CmdStop, "")
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
