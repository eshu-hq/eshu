// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
	candidates = append(candidates, "/opt/homebrew/bin/bash", "/usr/local/bin/bash")

	for _, cand := range candidates {
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		if bashAtLeast44(cand) {
			return filepath.Dir(cand)
		}
	}
	return ""
}
