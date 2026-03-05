package utils

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

// ExecOptions configures command execution.
type ExecOptions struct {
	WorkDir  string
	OnStdout func(line string)
	OnStderr func(line string)
}

// ExecOption is a functional option for ExecOptions.
type ExecOption func(*ExecOptions)

// WithWorkDir sets the working directory for command execution.
func WithWorkDir(dir string) ExecOption {
	return func(o *ExecOptions) {
		o.WorkDir = dir
	}
}

// WithStdout sets a callback for stdout lines.
func WithStdout(handler func(line string)) ExecOption {
	return func(o *ExecOptions) {
		o.OnStdout = handler
	}
}

// WithStderr sets a callback for stderr lines.
func WithStderr(handler func(line string)) ExecOption {
	return func(o *ExecOptions) {
		o.OnStderr = handler
	}
}

// Exec runs a command and returns the exit code.
func Exec(ctx context.Context, name string, args []string, options ...ExecOption) (int, error) {
	opts := &ExecOptions{}
	for _, opt := range options {
		opt(opts)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return -1, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return -1, fmt.Errorf("start command: %w", err)
	}

	// Stream output
	done := make(chan struct{}, 2)
	go streamOutput(stdout, opts.OnStdout, done)
	go streamOutput(stderr, opts.OnStderr, done)

	// Wait for output streams to finish
	<-done
	<-done

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), err
		}
		return -1, err
	}

	return 0, nil
}

func streamOutput(r io.Reader, callback func(string), done chan<- struct{}) {
	defer func() { done <- struct{}{} }()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if callback != nil {
			callback(line)
		}
	}
}
