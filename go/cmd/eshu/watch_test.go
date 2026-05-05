package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunWatchExecsIngesterWithResolvedWorkspaceRoot(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	startPath := filepath.Join(repoRoot, "pkg")
	if err := os.MkdirAll(startPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}

	restore, calls := stubWatchRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newWatchTestCommand()
	err := runWatch(cmd, []string{startPath})
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runWatch() error = %v, want %v", err, wantExecErr)
	}

	if calls.binary != "/tmp/eshu" {
		t.Fatalf("exec binary = %q, want %q", calls.binary, "/tmp/eshu")
	}
	wantWorkspaceRoot := mustEvalSymlinks(t, repoRoot)
	wantArgs := []string{"eshu", "local-host", "watch", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

func TestRunWatchUsesExplicitWorkspaceRootFlag(t *testing.T) {
	base := t.TempDir()
	workspaceRoot := filepath.Join(base, "monofolder")
	startPath := filepath.Join(workspaceRoot, "repo", "pkg")
	if err := os.MkdirAll(startPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}

	restore, calls := stubWatchRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		return wantExecErr
	}

	cmd := newWatchTestCommand()
	if err := cmd.Flags().Set("workspace-root", workspaceRoot); err != nil {
		t.Fatalf("Set(workspace-root) error = %v, want nil", err)
	}

	err := runWatch(cmd, []string{startPath})
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runWatch() error = %v, want %v", err, wantExecErr)
	}

	wantWorkspaceRoot := mustEvalSymlinks(t, workspaceRoot)
	wantArgs := []string{"eshu", "local-host", "watch", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

func TestRunWorkspaceWatchUsesWorkspaceArgumentAsExplicitRoot(t *testing.T) {
	workspaceRoot := t.TempDir()

	restore, calls := stubWatchRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		return wantExecErr
	}

	cmd := newWatchTestCommand()
	err := runWorkspaceWatch(cmd, []string{workspaceRoot})
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runWorkspaceWatch() error = %v, want %v", err, wantExecErr)
	}

	wantWorkspaceRoot := mustEvalSymlinks(t, workspaceRoot)
	wantArgs := []string{"eshu", "local-host", "watch", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

func TestRunWatchReturnsFriendlyErrorWhenExecutableMissing(t *testing.T) {
	restore, calls := stubWatchRuntime()
	defer restore()

	calls.executable = func() (string, error) {
		return "", errors.New("missing")
	}

	err := runWatch(newWatchTestCommand(), nil)
	if err == nil {
		t.Fatal("runWatch() error = nil, want non-nil")
	}
	if err.Error() != "eshu executable not found" {
		t.Fatalf("runWatch() error = %q, want %q", err.Error(), "eshu executable not found")
	}
}

func TestRunWatchReturnsErrorWhenWorkspaceRootFlagIsUnavailable(t *testing.T) {
	restore, calls := stubWatchRuntime()
	defer restore()

	calls.executable = func() (string, error) {
		t.Fatal("Executable() should not be called when flag lookup fails")
		return "", nil
	}

	err := runWatch(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runWatch() error = nil, want non-nil")
	}
	if err.Error() != "flag accessed but not defined: workspace-root" {
		t.Fatalf("runWatch() error = %q, want missing workspace-root flag error", err.Error())
	}
}

type watchRuntimeCalls struct {
	executable func() (string, error)
	exec       func(string, []string, []string) error
	binary     string
	args       []string
	env        []string
}

func stubWatchRuntime() (func(), *watchRuntimeCalls) {
	calls := &watchRuntimeCalls{}

	originalExecutable := eshuExecutable
	originalExec := eshuExec
	originalEnviron := eshuEnviron

	eshuExecutable = func() (string, error) {
		if calls.executable == nil {
			return "", errors.New("eshuExecutable not stubbed")
		}
		return calls.executable()
	}
	eshuExec = func(binary string, args []string, env []string) error {
		if calls.exec == nil {
			return errors.New("eshuExec not stubbed")
		}
		return calls.exec(binary, args, env)
	}
	eshuEnviron = func() []string {
		return []string{"PATH=/tmp"}
	}

	return func() {
		eshuExecutable = originalExecutable
		eshuExec = originalExec
		eshuEnviron = originalEnviron
	}, calls
}

func newWatchTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-root", "", "")
	return cmd
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v, want nil", path, err)
	}
	return resolved
}
