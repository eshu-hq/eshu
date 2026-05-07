package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartLocalChildProcessRoutesOutputToWorkspaceLogFile(t *testing.T) {
	logDir := t.TempDir()
	binaryPath := filepath.Join(t.TempDir(), "eshu-child")
	script := "#!/bin/sh\nprintf 'stdout-line\\n'\nprintf 'stderr-line\\n' >&2\n"
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake child binary: %v", err)
	}

	originalLookPath := localHostLookPath
	t.Cleanup(func() {
		localHostLookPath = originalLookPath
	})
	localHostLookPath = func(name string) (string, error) {
		if name != "eshu-ingester" {
			t.Fatalf("localHostLookPath(%q), want eshu-ingester", name)
		}
		return binaryPath, nil
	}

	t.Setenv("ESHU_LOCAL_LOG_MODE", "file")
	t.Setenv("ESHU_LOCAL_LOG_DIR", logDir)

	cmd, err := startLocalChildProcess("eshu-ingester", []string{"eshu-ingester"}, os.Environ())
	if err != nil {
		t.Fatalf("startLocalChildProcess() error = %v, want nil", err)
	}
	if err := waitLocalChildProcess(context.Background(), cmd); err != nil {
		t.Fatalf("waitLocalChildProcess() error = %v, want nil", err)
	}

	payload, err := os.ReadFile(filepath.Join(logDir, "eshu-ingester.log"))
	if err != nil {
		t.Fatalf("read child log: %v", err)
	}
	got := string(payload)
	for _, want := range []string{"stdout-line", "stderr-line"} {
		if !strings.Contains(got, want) {
			t.Fatalf("child log missing %q in %q", want, got)
		}
	}
}

func TestStartLocalChildProcessKeepsStdioForTerminalMode(t *testing.T) {
	cmd := &exec.Cmd{}
	if err := configureLocalChildProcessIO(cmd, "eshu-ingester", []string{"ESHU_LOCAL_LOG_MODE=terminal"}); err != nil {
		t.Fatalf("configureLocalChildProcessIO() error = %v, want nil", err)
	}
	if cmd.Stdout != os.Stdout {
		t.Fatalf("Stdout = %#v, want os.Stdout", cmd.Stdout)
	}
	if cmd.Stderr != os.Stderr {
		t.Fatalf("Stderr = %#v, want os.Stderr", cmd.Stderr)
	}
	if cmd.Stdin != os.Stdin {
		t.Fatalf("Stdin = %#v, want os.Stdin", cmd.Stdin)
	}
}
