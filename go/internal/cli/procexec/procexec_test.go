// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package procexec_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/procexec"
)

func TestCleanExecutableArg0(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		binary string
		want   string
	}{
		{name: "absolute path keeps only the base name", binary: "/usr/local/bin/eshu", want: "eshu"},
		{name: "bare name is returned unchanged", binary: "eshu", want: "eshu"},
		{name: "surrounding whitespace is trimmed", binary: "/usr/local/bin/eshu  ", want: "eshu"},
		{name: "whitespace-only input falls back to eshu", binary: "   ", want: "eshu"},
		// The next two pin what filepath.Base actually does, which is not what
		// the "eshu" fallback reads like it covers. Base("") is ".", and a
		// trailing separator is stripped before the last element is taken, so
		// neither reaches the empty-name fallback. Both inputs are
		// unreachable in production -- binary always comes from a successful
		// Executable() or LookPath() -- but pinning them keeps a future
		// "simplification" of the fallback from changing argv[0] silently.
		{name: "empty input yields Base's dot, not the fallback", binary: "", want: "."},
		{name: "trailing separator yields the parent directory name", binary: "/usr/local/bin/", want: "bin"},
		{name: "a differently named binary keeps its own name", binary: "/opt/eshu-mcp-server", want: "eshu-mcp-server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := procexec.CleanExecutableArg0(tt.binary); got != tt.want {
				t.Fatalf("CleanExecutableArg0(%q) = %q, want %q", tt.binary, got, tt.want)
			}
		})
	}
}

// TestSeamsDefaultToTheRealHostCalls checks that each seam ships wired to a
// working implementation. A seam left nil compiles and only panics at the
// moment a command tries to re-exec, which is the least convenient place to
// discover it. Exec is deliberately absent: calling it replaces the test
// binary's process image, so the only safe assertion about it is that it is
// not nil (covered by TestExecSeamIsWiredAndSubstitutable).
func TestSeamsDefaultToTheRealHostCalls(t *testing.T) {
	self, err := procexec.Executable()
	if err != nil {
		t.Fatalf("Executable() returned an error: %v", err)
	}
	if self == "" {
		t.Fatal("Executable() returned an empty path")
	}

	wd, err := procexec.Getwd()
	if err != nil {
		t.Fatalf("Getwd() returned an error: %v", err)
	}
	if !filepath.IsAbs(wd) {
		t.Fatalf("Getwd() = %q, want an absolute path", wd)
	}

	if _, err := procexec.LookPath("this-binary-does-not-exist-6059"); err == nil {
		t.Fatal("LookPath() found a binary that should not exist")
	}

	env := procexec.Environ()
	if len(env) != len(os.Environ()) {
		t.Fatalf("Environ() returned %d entries, want %d", len(env), len(os.Environ()))
	}
}

// TestExecSeamIsWiredAndSubstitutable is the load-bearing test for this
// package. syscall.Exec replaces the process image and never returns, so a
// command that reaches it cannot be tested in-process at all unless the call
// goes through a variable a test can reassign. This asserts both halves: the
// default is wired, and reassignment is observed by the caller.
func TestExecSeamIsWiredAndSubstitutable(t *testing.T) {
	if procexec.Exec == nil {
		t.Fatal("Exec is nil; every re-exec call site would panic")
	}

	original := procexec.Exec
	t.Cleanup(func() { procexec.Exec = original })

	sentinel := errors.New("substituted exec")
	var gotBinary string
	var gotArgs, gotEnv []string
	procexec.Exec = func(binary string, args []string, env []string) error {
		gotBinary, gotArgs, gotEnv = binary, args, env
		return sentinel
	}

	err := procexec.Exec("/usr/local/bin/eshu", []string{"eshu", "local-host", "watch"}, []string{"PATH=/bin"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Exec() error = %v, want %v", err, sentinel)
	}
	if gotBinary != "/usr/local/bin/eshu" {
		t.Fatalf("Exec() binary = %q, want /usr/local/bin/eshu", gotBinary)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "eshu" {
		t.Fatalf("Exec() args = %q, want the caller's argv", gotArgs)
	}
	if len(gotEnv) != 1 || gotEnv[0] != "PATH=/bin" {
		t.Fatalf("Exec() env = %q, want the caller's environment", gotEnv)
	}
}

// TestSeamsAreSubstitutableAndRestorable covers the other four seams with the
// same save/override/restore shape go/cmd/eshu's tests use, so a change that
// turned one of them into a plain func (and silently broke every override site)
// fails here first.
func TestSeamsAreSubstitutableAndRestorable(t *testing.T) {
	originalExecutable := procexec.Executable
	originalGetwd := procexec.Getwd
	originalLookPath := procexec.LookPath
	originalEnviron := procexec.Environ
	t.Cleanup(func() {
		procexec.Executable = originalExecutable
		procexec.Getwd = originalGetwd
		procexec.LookPath = originalLookPath
		procexec.Environ = originalEnviron
	})

	procexec.Executable = func() (string, error) { return "/stub/eshu", nil }
	procexec.Getwd = func() (string, error) { return "/stub/wd", nil }
	procexec.LookPath = func(name string) (string, error) { return "/stub/" + name, nil }
	procexec.Environ = func() []string { return []string{"PATH=/stub"} }

	if got, _ := procexec.Executable(); got != "/stub/eshu" {
		t.Fatalf("Executable() = %q, want /stub/eshu", got)
	}
	if got, _ := procexec.Getwd(); got != "/stub/wd" {
		t.Fatalf("Getwd() = %q, want /stub/wd", got)
	}
	if got, _ := procexec.LookPath("eshu-mcp-server"); got != "/stub/eshu-mcp-server" {
		t.Fatalf("LookPath() = %q, want /stub/eshu-mcp-server", got)
	}
	if got := procexec.Environ(); len(got) != 1 || got[0] != "PATH=/stub" {
		t.Fatalf("Environ() = %q, want [PATH=/stub]", got)
	}
}
