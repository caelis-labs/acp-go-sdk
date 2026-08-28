package stdio

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
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
	tree   processTree

	treeReady chan struct{}
	waitDone  chan struct{}
	forceDone chan struct{}
	waitErr   error
	forceErr  error
	closeErr  error
	stdinErr  error
	stdoutErr error

	forceRequested atomic.Bool
	forceOnce      sync.Once
	closeOnce      sync.Once
	shutdownOnce   sync.Once
	stdinOnce      sync.Once
	stdoutOnce     sync.Once
	shutdownErr    error
}

type processTree interface {
	terminate() error
	release() error
}

type directProcessTree struct {
	process *os.Process
}

func (t directProcessTree) terminate() error {
	if t.process == nil {
		return nil
	}
	if err := t.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func (directProcessTree) release() error { return nil }

// Start launches an ACP subprocess and continuously drains its stderr. On
// Windows it does not create or show a console window for the child.
func Start(ctx context.Context, command Command) (*Process, error) {
	if command.Executable == "" {
		return nil, errors.New("stdio: executable is required")
	}
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	configureProcessCommand(cmd)
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
	process := &Process{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
		treeReady: make(chan struct{}),
		waitDone:  make(chan struct{}),
		forceDone: make(chan struct{}),
	}
	cmd.Cancel = func() error {
		return process.forceStop()
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	tree, err := attachProcessTree(cmd)
	if err != nil {
		process.installTree(directProcessTree{process: cmd.Process})
		_ = process.forceStop()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = cmd.Wait()
		return nil, err
	}
	process.installTree(tree)
	go func() {
		waitErr := cmd.Wait()
		releaseErr := tree.release()
		process.waitErr = errors.Join(waitErr, releaseErr)
		close(process.waitDone)
	}()
	return process, nil
}

func (p *Process) installTree(tree processTree) {
	p.tree = tree
	close(p.treeReady)
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

// Close idempotently closes the pipes and immediately terminates the owned
// process tree. Use Shutdown to give the process an opportunity to exit after
// stdin closes.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = errors.Join(p.closeInput(), p.forceStop(), p.closeOutput())
	})
	return p.closeErr
}

// Shutdown closes the child's stdin and waits for a graceful exit. If ctx is
// canceled first, Shutdown forcefully terminates the owned process tree,
// closes stdout, and still joins the internal waiter before returning. The
// context cause is returned after forced cleanup succeeds. The first call owns
// the graceful deadline and its terminal result is returned by later calls.
func (p *Process) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stdio: shutdown context is required")
	}
	p.shutdownOnce.Do(func() {
		p.shutdownErr = p.shutdown(ctx)
	})
	return p.shutdownErr
}

func (p *Process) shutdown(ctx context.Context) error {
	inputErr := p.closeInput()
	select {
	case <-p.waitDone:
		outputErr := p.closeOutput()
		if p.forceRequested.Load() {
			return errors.Join(inputErr, p.forceStop(), outputErr)
		}
		return errors.Join(inputErr, p.waitErr, outputErr)
	case <-ctx.Done():
		forceErr := p.forceStop()
		outputErr := p.closeOutput()
		<-p.waitDone
		return errors.Join(context.Cause(ctx), inputErr, forceErr, outputErr)
	}
}

func (p *Process) waitComplete() bool {
	select {
	case <-p.waitDone:
		return true
	default:
		return false
	}
}

func (p *Process) forceStop() error {
	p.forceOnce.Do(func() {
		p.forceRequested.Store(true)
		defer close(p.forceDone)
		<-p.treeReady
		p.forceErr = p.tree.terminate()
	})
	<-p.forceDone
	return p.forceErr
}

func (p *Process) closeInput() error {
	p.stdinOnce.Do(func() {
		if err := p.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) && !p.waitComplete() {
			p.stdinErr = err
		}
	})
	return p.stdinErr
}

func (p *Process) closeOutput() error {
	p.stdoutOnce.Do(func() {
		if err := p.stdout.Close(); err != nil && !errors.Is(err, os.ErrClosed) && !p.waitComplete() {
			p.stdoutErr = err
		}
	})
	return p.stdoutErr
}

// ClientProcess couples a ClientSideConnection with the child it owns.
type ClientProcess struct {
	Connection *acp.ClientSideConnection
	Process    *Process

	closeOnce    sync.Once
	shutdownOnce sync.Once
	closeErr     error
	shutdownErr  error
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
	p.closeOnce.Do(func() {
		p.closeErr = errors.Join(p.Connection.Close(), p.Process.Close())
	})
	return p.closeErr
}

// Shutdown closes the ACP connection, gracefully shuts down the child within
// ctx, and joins the connection after the process has reached a terminal state.
// The first call owns the graceful deadline and its terminal result is returned
// by later calls.
func (p *ClientProcess) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("stdio: shutdown context is required")
	}
	p.shutdownOnce.Do(func() {
		p.shutdownErr = p.shutdown(ctx)
	})
	return p.shutdownErr
}

func (p *ClientProcess) shutdown(ctx context.Context) error {
	connectionCloseErr := p.Connection.Close()
	processErr := p.Process.Shutdown(ctx)
	connectionErr := p.Connection.Wait(context.WithoutCancel(ctx))
	if errors.Is(connectionErr, acp.ErrConnectionClosed) || errors.Is(connectionErr, acp.ErrPeerClosed) {
		connectionErr = nil
	}
	return errors.Join(connectionCloseErr, processErr, connectionErr)
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
