// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// bashAtLeast44 reports whether the bash binary at path is version 4.4 or
// newer. It execs the candidate with a version-guard one-liner — the same
// `BASH_VERSINFO` arithmetic guard scripts/verify-ifa-*.sh use at the top of
// their own files — rather than parsing `--version`/`BASH_VERSINFO` text
// output, whose format varies across vendors and locales. The guard
// expression evaluates to 0 (exit success) when the candidate is bash >= 4.4
// and to 1 when it is older, so the candidate's own exit code is the signal.
// A candidate that fails to exec at all (missing, not executable, not bash)
// is treated as not qualifying rather than as an error: callers use this to
// skip a broken candidate and try the next one, never to fail the gate over
// an environment they cannot fully control (#5050).
func bashAtLeast44(path string) bool {
	const guard = "exit $(( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 4) ))"
	cmd := exec.Command(path, "-c", guard) // #nosec G204 -- path comes from a fixed candidate list (LookPath("bash") or a hardcoded Homebrew/local path), not attacker input
	return cmd.Run() == nil
}

// resolveBash44Dir returns the directory containing the first bash >= 4.4
// binary found, checked in order: `bash` resolved via the process's PATH,
// then the common Homebrew (Apple Silicon) and Homebrew/Linuxbrew (Intel/
// Linux-adjacent) install locations. Returns "" when no candidate qualifies,
// which on this repo's supported hosts should only happen if bash itself is
// missing — CI's Linux `bash` is already >= 4.x so it qualifies via PATH on
// the first check. Callers MUST treat "" as "leave the environment
// unchanged", never as an error: this resolver exists to make gate commands
// that shell out to `bash scripts/*.sh` pick a working interpreter on macOS
// (whose default /bin/bash is 3.2.57 and lacks bash 4.0+ features like
// `declare -A`), not to enforce a minimum bash version on every host.
func resolveBash44Dir() string {
	var candidates []string
	if p, err := exec.LookPath("bash"); err == nil {
		candidates = append(candidates, p)
	}
	return firstQualifyingBashDir(append(candidates, bashFallbackCandidates...))
}

// gateSubprocessEnv returns the environment a gate subprocess should run with:
// os.Environ() with any GOROOT entry removed, and with a bash >= 4.4 directory
// prepended to PATH only when the PATH-resolved bash does not qualify and
// resolveBash44Dir finds a fallback, so a gate command that shells out to
// "bash scripts/*.sh" resolves the inner bash to a working interpreter
// instead of macOS's 3.2. On hosts where PATH already resolves a qualifying
// bash, PATH is left untouched so caller-installed tools keep their order
// (#8709).
//
// The GOROOT strip is load-bearing, not defensive noise. This binary is
// launched via `go -C go run ./cmd/ci-gates run` (scripts/dev/
// run-selected-gates.sh); when go/go.mod requests a newer Go than the host
// toolchain, GOTOOLCHAIN=auto switches, and the switched go process exports
// GOROOT — the downloaded toolchain's root — to this process, which would
// otherwise pass it on to every gate. A gate whose command runs `go` in a
// module the HOST toolchain already satisfies (sdk/go/collector, the scorecard
// example, each pinned one patch release behind go/) then pairs the host go
// driver with the switched GOROOT's tools and every package fails with
// `compile: version "go1.X" does not match go tool version "go1.Y"`. With
// GOROOT cleared, each gate's go command derives its root from its own binary
// and re-switches per its own go.mod, so both module families stay
// self-consistent. Same mechanism precommit-go.sh isolates per `go install`
// (#6113); the runner clears it once so no gate can inherit it.
//
// It returns nil only when there is neither a qualifying bash to steer to nor
// a GOROOT to strip, which os/exec treats as "inherit the current process
// environment unchanged" — the deliberate no-op path (CI Linux already has a
// >= 4.4 bash on PATH; a host with only an old bash then relies on the
// per-script BASH_VERSINFO guard). This is split out from runShellCommand so
// the wiring is unit-testable rather than only reachable through a live gate
// run (#5050).
// bashFallbackCandidates are the non-PATH bash locations checked, in order,
// when the PATH-resolved bash does not qualify.
var bashFallbackCandidates = []string{"/opt/homebrew/bin/bash", "/usr/local/bin/bash"}

// firstQualifyingBashDir returns the directory of the first candidate that
// exists and satisfies the version guard, or "" when none does.
func firstQualifyingBashDir(candidates []string) string {
	for _, cand := range candidates {
		if cand == "" {
			continue
		}
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		if bashAtLeast44(cand) {
			return filepath.Dir(cand)
		}
	}
	return ""
}

func gateSubprocessEnv() []string {
	pathBash, _ := exec.LookPath("bash")
	return gateSubprocessEnvWith(pathBash, bashFallbackCandidates)
}

// gateSubprocessEnvWith is gateSubprocessEnv with injectable inputs: the
// PATH-resolved bash path ("" when PATH has none) and the fallback candidate
// list (#8711). Production passes the live LookPath result and
// bashFallbackCandidates; tests pass fixtures, so the fallback-prepend branch
// is pinned hermetically on every host instead of only where a macOS-class
// fallback exists.
func gateSubprocessEnvWith(pathBash string, fallbacks []string) []string {
	_, gorootSet := os.LookupEnv("GOROOT")
	// When the PATH-resolved bash already qualifies, there is nothing to
	// steer: rewriting PATH would only push the caller's own ordering back
	// (on Linux the resolved dir is /usr/bin, which then shadows
	// caller-installed tools such as /usr/local/bin/rg — #8709). Only the
	// GOROOT strip may still apply.
	if pathBash != "" && bashAtLeast44(pathBash) {
		if !gorootSet {
			return nil
		}
		return stripGorootEntry(os.Environ())
	}
	// Only reached when steering to a fallback (or steering nowhere): PATH
	// is filtered solely to replace it with the single steered entry. With
	// no fallback dir the inherited PATH must survive intact, or the
	// subprocess loses every command lookup (#8709 F1).
	dir := firstQualifyingBashDir(fallbacks)
	if dir == "" && !gorootSet {
		return nil
	}
	base := os.Environ()
	env := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "GOROOT=") {
			continue
		}
		if dir != "" && strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	if dir != "" {
		// Single PATH entry: appending a second PATH= alongside the
		// inherited one lets os/exec last-wins discard the caller's order
		// (#8709). Replace instead so the steered dir genuinely comes first.
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}

// stripGorootEntry returns env without any GOROOT entry, preserving every
// other entry (including PATH) byte for byte.
func stripGorootEntry(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOROOT=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
