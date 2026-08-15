// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package procexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// The five variables below are the package's process seams. They are variables
// rather than functions for one reason: every one of them reaches host state a
// test cannot arrange, and Exec in particular replaces the running process
// image and never returns. A command that called syscall.Exec directly could
// not be tested at all -- the test binary would be gone. Callers therefore call
// through these variables, and a test substitutes the ones it needs and
// restores them afterwards.
//
// They are shared process state, so a test that assigns one MUST restore the
// original (t.Cleanup or defer) and MUST NOT run in parallel with another test
// touching the same seam. They are a test seam, not configuration: production
// code assigns to them nowhere.
var (
	// Executable reports the path of the running binary. Defaults to
	// os.Executable.
	Executable = os.Executable

	// Getwd reports the process working directory. Defaults to os.Getwd.
	Getwd = os.Getwd

	// LookPath resolves a binary name against PATH. Defaults to exec.LookPath.
	LookPath = exec.LookPath

	// Exec replaces the current process image with binary, running it with
	// args as its argv (args[0] is the program name, not the first argument)
	// and env as its complete environment. On success it DOES NOT RETURN --
	// the calling process ceases to exist and nothing after the call runs, so
	// deferred functions do not fire, buffered output is not flushed, and the
	// child inherits the caller's PID, file descriptors, and exit status
	// obligations. It returns only when the exec itself fails.
	//
	// Defaults to syscall.Exec. That compiles on every GOOS Go supports,
	// including windows -- but the windows implementation
	// (syscall/exec_windows.go) is a stub returning EWINDOWS, so on windows
	// this never replaces anything and always returns an error. Do not read a
	// successful GOOS=windows build as working process replacement.
	Exec = func(binary string, args []string, env []string) error {
		return syscall.Exec(binary, args, env) // #nosec G204 -- binary is resolved via LookPath from a fixed name; args are program-constructed
	}

	// Environ reports the process environment as name=value entries. Defaults
	// to os.Environ.
	Environ = os.Environ
)

// CleanExecutableArg0 reduces a resolved binary path to the argv[0] a re-exec
// should advertise: the base name, with surrounding whitespace removed, so a
// child sees "eshu" rather than "/usr/local/bin/eshu".
//
// The "eshu" fallback covers a narrower case than it looks like it does,
// because filepath.Base is applied first: it fires only for a whitespace-only
// input. An empty input yields Base's ".", and a trailing separator yields the
// parent directory name ("/usr/local/bin/" gives "bin"). Neither is reachable
// from a successful Executable() or LookPath(), which is why this has never
// mattered; see the pinned cases in procexec_test.go before changing it.
func CleanExecutableArg0(binary string) string {
	name := strings.TrimSpace(filepath.Base(binary))
	if name == "" {
		return "eshu"
	}
	return name
}
