package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunMCPStartStdioExecsLocalHostWithResolvedWorkspaceRoot(t *testing.T) {
	base := t.TempDir()
	repoRoot := filepath.Join(base, "repo")
	startPath := filepath.Join(repoRoot, "pkg")
	if err := os.MkdirAll(startPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v, want nil", err)
	}

	restore, calls := stubServiceRuntime()
	defer restore()

	wantExecErr := errors.New("exec sentinel")
	calls.executable = func() (string, error) { return "/tmp/eshu", nil }
	calls.getwd = func() (string, error) { return startPath, nil }
	calls.exec = func(binary string, args []string, env []string) error {
		calls.binary = binary
		calls.args = append([]string(nil), args...)
		calls.env = append([]string(nil), env...)
		return wantExecErr
	}

	cmd := newMCPStartTestCommand()
	err := runMCPStart(cmd, nil)
	if !errors.Is(err, wantExecErr) {
		t.Fatalf("runMCPStart() error = %v, want %v", err, wantExecErr)
	}

	if got, want := calls.binary, "/tmp/eshu"; got != want {
		t.Fatalf("exec binary = %q, want %q", got, want)
	}
	wantWorkspaceRoot := mustEvalSymlinks(t, repoRoot)
	wantArgs := []string{"eshu", "local-host", "mcp-stdio", wantWorkspaceRoot}
	if !reflect.DeepEqual(calls.args, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", calls.args, wantArgs)
	}
}

type serviceRuntimeCalls struct {
	executable func() (string, error)
	getwd      func() (string, error)
	exec       func(string, []string, []string) error
	binary     string
	args       []string
	env        []string
}

func stubServiceRuntime() (func(), *serviceRuntimeCalls) {
	calls := &serviceRuntimeCalls{}

	originalExecutable := eshuExecutable
	originalGetwd := eshuGetwd
	originalExec := eshuExec
	originalEnviron := eshuEnviron

	eshuExecutable = func() (string, error) {
		if calls.executable == nil {
			return "", errors.New("eshuExecutable not stubbed")
		}
		return calls.executable()
	}
	eshuGetwd = func() (string, error) {
		if calls.getwd == nil {
			return "", errors.New("eshuGetwd not stubbed")
		}
		return calls.getwd()
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
		eshuGetwd = originalGetwd
		eshuExec = originalExec
		eshuEnviron = originalEnviron
	}, calls
}

func newMCPStartTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("transport", "stdio", "")
	cmd.Flags().String("host", "127.0.0.1", "")
	cmd.Flags().Int("port", 0, "")
	cmd.Flags().String("workspace-root", "", "")
	return cmd
}
