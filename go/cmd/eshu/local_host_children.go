package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type localHostChild struct {
	name string
	cmd  *exec.Cmd
}

type localHostChildExit struct {
	name string
	err  error
}

func waitLocalHostChildren(ctx context.Context, children []localHostChild, allowCleanExit string) error {
	if len(children) == 0 {
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	exitc := make(chan localHostChildExit, len(children))
	for _, child := range children {
		child := child
		go func() {
			exitc <- localHostChildExit{
				name: child.name,
				err:  localHostWaitChildProcess(childCtx, child.cmd),
			}
		}()
	}

	first := <-exitc
	cancel()

	for remaining := len(children) - 1; remaining > 0; remaining-- {
		<-exitc
	}

	if ctx.Err() != nil {
		return nil
	}
	if first.err != nil {
		return fmt.Errorf("%s exited: %w", first.name, first.err)
	}
	if allowCleanExit != "" && first.name == allowCleanExit {
		return nil
	}
	return fmt.Errorf("%s exited unexpectedly", first.name)
}

func waitLocalHostChildrenKeepingAllowedCleanExits(ctx context.Context, children []localHostChild, allowedCleanExits map[string]struct{}) error {
	if len(children) == 0 {
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	exitc := make(chan localHostChildExit, len(children))
	for _, child := range children {
		child := child
		go func() {
			exitc <- localHostChildExit{
				name: child.name,
				err:  localHostWaitChildProcess(childCtx, child.cmd),
			}
		}()
	}

	active := len(children)
	for active > 0 {
		select {
		case <-ctx.Done():
			cancel()
			for ; active > 0; active-- {
				<-exitc
			}
			return nil
		case exit := <-exitc:
			active--
			if ctx.Err() != nil {
				cancel()
				for ; active > 0; active-- {
					<-exitc
				}
				return nil
			}
			if exit.err != nil {
				cancel()
				for ; active > 0; active-- {
					<-exitc
				}
				return fmt.Errorf("%s exited: %w", exit.name, exit.err)
			}
			if _, ok := allowedCleanExits[exit.name]; ok {
				slog.Info("local Eshu service child exited cleanly; keeping service alive",
					slog.String("child", exit.name),
					slog.Int("remaining_children", active),
				)
				continue
			}
			cancel()
			for ; active > 0; active-- {
				<-exitc
			}
			return fmt.Errorf("%s exited unexpectedly", exit.name)
		}
	}
	return nil
}

func startLocalChildProcess(name string, args []string, env []string) (*exec.Cmd, error) {
	binary, err := localHostLookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s binary not found in PATH", name)
	}
	cmd := exec.Command(binary, args[1:]...)
	cmd.Args = append([]string(nil), args...)
	cmd.Env = env
	closeIO, err := configureLocalChildProcessIOWithCleanup(cmd, name, env)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if closeIO != nil {
			_ = closeIO()
		}
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	if closeIO != nil {
		_ = closeIO()
	}
	return cmd, nil
}

func configureLocalChildProcessIO(cmd *exec.Cmd, name string, env []string) error {
	_, err := configureLocalChildProcessIOWithCleanup(cmd, name, env)
	return err
}

func configureLocalChildProcessIOWithCleanup(cmd *exec.Cmd, name string, env []string) (func() error, error) {
	mode := strings.TrimSpace(localHostEnvValue(env, localHostLogModeEnv))
	if mode == "" {
		mode = localHostLogModeFile
	}

	switch mode {
	case localHostLogModeTerminal:
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return nil, nil
	case localHostLogModeQuiet:
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		cmd.Stdin = nil
		return nil, nil
	case localHostLogModeFile:
		logDir := strings.TrimSpace(localHostEnvValue(env, localHostLogDirEnv))
		if logDir == "" {
			return nil, fmt.Errorf("%s is required when %s=%s", localHostLogDirEnv, localHostLogModeEnv, localHostLogModeFile)
		}
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return nil, fmt.Errorf("create local child log dir: %w", err)
		}
		logPath := filepath.Join(logDir, filepath.Base(name)+".log")
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open %s log: %w", name, err)
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		cmd.Stdin = nil
		return logFile.Close, nil
	default:
		return nil, fmt.Errorf("unsupported %s %q; expected %s, %s, or %s", localHostLogModeEnv, mode, localHostLogModeFile, localHostLogModeTerminal, localHostLogModeQuiet)
	}
}

func waitLocalChildProcess(ctx context.Context, cmd *exec.Cmd) error {
	errc := make(chan error, 1)
	go func() {
		errc <- cmd.Wait()
	}()

	select {
	case err := <-errc:
		return normalizeLocalChildNaturalExit(err)
	case <-ctx.Done():
		if err := interruptLocalChildProcess(cmd); err != nil {
			return err
		}
		return waitForLocalChildExit(cmd, errc, localHostShutdownTimeout)
	}
}

func stopLocalChildProcess(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}
	if err := interruptLocalChildProcess(cmd); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	return waitForLocalChildExit(cmd, done, timeout)
}

func interruptLocalChildProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = cmd.Process.Kill()
		return fmt.Errorf("interrupt child process: %w", err)
	}
	return nil
}

func waitForLocalChildExit(cmd *exec.Cmd, done <-chan error, timeout time.Duration) error {
	select {
	case err := <-done:
		return normalizeLocalChildStoppedExit(err)
	case <-time.After(timeout):
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("kill child process: %w", err)
		}
		<-done
		return nil
	}
}

func normalizeLocalChildNaturalExit(err error) error {
	if err == nil || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ECHILD) {
		return nil
	}
	if strings.Contains(err.Error(), "Wait was already called") {
		return nil
	}
	return err
}

func normalizeLocalChildStoppedExit(err error) error {
	if err == nil || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ECHILD) {
		return nil
	}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		return nil
	}
	if strings.Contains(err.Error(), "Wait was already called") {
		return nil
	}
	return err
}

func localHostEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
