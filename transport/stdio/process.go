package stdio

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// Command configures an explicitly named ACP subprocess. Executable is
// launched directly; no shell is used.
type Command struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
	Stderr     io.Writer
	WaitDelay  time.Duration
}

// Process is a started ACP subprocess with stdio protocol pipes.
type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	waitDone  chan struct{}
	waitErr   error
	closeOnce sync.Once
}

// Start launches an ACP subprocess and continuously drains its stderr.
func Start(ctx context.Context, command Command) (*Process, error) {
	if command.Executable == "" {
		return nil, errors.New("stdio: executable is required")
	}
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	cmd.WaitDelay = command.WaitDelay
	if command.Env != nil {
		cmd.Env = append([]string(nil), command.Env...)
	}
	if command.Stderr == nil {
		cmd.Stderr = io.Discard
	} else {
		cmd.Stderr = command.Stderr
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	process := &Process{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		waitDone: make(chan struct{}),
	}
	go func() {
		process.waitErr = cmd.Wait()
		close(process.waitDone)
	}()
	return process, nil
}

// Input is the child's protocol stdin.
func (p *Process) Input() io.WriteCloser { return p.stdin }

// Output is the child's protocol stdout.
func (p *Process) Output() io.ReadCloser { return p.stdout }

// Wait waits for the child process to exit.
func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-p.waitDone:
		return p.waitErr
	}
}

// Close idempotently closes the pipes and terminates a still-running child.
func (p *Process) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if err := p.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			closeErr = err
		}
		if err := p.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) && closeErr == nil {
			closeErr = err
		}
		if p.cmd.Process != nil {
			if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

// ClientProcess couples a ClientSideConnection with the child it owns.
type ClientProcess struct {
	Connection *acp.ClientSideConnection
	Process    *Process

	closeOnce sync.Once
}

// StartClient launches a child agent and connects client to it over stdio.
func StartClient(ctx context.Context, client acp.Client, command Command, opts acp.ConnectionOptions) (*ClientProcess, error) {
	process, err := Start(ctx, command)
	if err != nil {
		return nil, err
	}
	connection, err := acp.NewClientSideConnectionWithOptions(client, process.Input(), process.Output(), opts)
	if err != nil {
		_ = process.Close()
		return nil, err
	}
	return &ClientProcess{Connection: connection, Process: process}, nil
}

// Close idempotently closes the connection and child process.
func (p *ClientProcess) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if err := p.Connection.Close(); err != nil {
			closeErr = err
		}
		if err := p.Process.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	})
	return closeErr
}

// Wait waits for the child to exit, then closes and joins the connection.
func (p *ClientProcess) Wait(ctx context.Context) error {
	processErr := p.Process.Wait(ctx)
	if processErr != nil && ctx.Err() != nil {
		return processErr
	}
	_ = p.Connection.Close()
	connectionErr := p.Connection.Wait(ctx)
	if processErr != nil {
		return processErr
	}
	if errors.Is(connectionErr, acp.ErrConnectionClosed) || errors.Is(connectionErr, acp.ErrPeerClosed) {
		return nil
	}
	return connectionErr
}
