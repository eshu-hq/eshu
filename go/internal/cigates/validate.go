// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Validate performs integrity checks on the registry against the actual files
// on disk at repoRoot. It accumulates all errors rather than stopping at the
// first. A nil or empty error slice means the registry is consistent with the
// repository.
//
// Checks performed (#4213 AC):
//   - For gates with Local set: the leading script path in Local.Command (and
//     Local.TestCommand, when present) exists on disk relative to repoRoot.
//   - For gates with CI.Workflow set: the workflow file exists under
//     .github/workflows/ relative to repoRoot.
//
// CI-only gates (Local==nil) skip the script check but still require the
// workflow file to be present.
func (r *Registry) Validate(repoRoot string) []error {
	var errs []error
	for _, g := range r.Gates {
		if g.Local != nil {
			if err := checkScript(repoRoot, g.ID, g.Local.Command); err != nil {
				errs = append(errs, err)
			}
			if g.Local.TestCommand != "" {
				if err := checkScript(repoRoot, g.ID, g.Local.TestCommand); err != nil {
					errs = append(errs, err)
				}
			}
		}
		if g.CI.Workflow != "" {
			wfPath := filepath.Join(repoRoot, ".github", "workflows", g.CI.Workflow)
			if _, err := os.Stat(wfPath); os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("gate %q: workflow file %q not found", g.ID, wfPath))
			}
		}
	}
	return errs
}

// checkScript verifies that the leading script path in a local command exists
// on disk. For inline go-toolchain commands (e.g. "cd go && go test ...") the
// extractor returns "" and the check is skipped — those commands do not
// reference a file path that can be stat-checked.
func checkScript(repoRoot, gateID, command string) error {
	scriptPath := extractScriptPath(command)
	if scriptPath == "" {
		// Inline toolchain command (cd go && go …) or unrecognised pattern —
		// no script file to verify.
		return nil
	}
	full := filepath.Join(repoRoot, scriptPath)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return fmt.Errorf("gate %q: script %q not found (derived from command %q)", gateID, full, command)
	}
	return nil
}

// extractScriptPath returns the repo-relative script path from a shell command
// string, or "" when the command does not reference a script file (e.g. an
// inline go-toolchain invocation like "cd go && go test ...").
//
// Recognised patterns:
//
//   - "bash scripts/foo.sh [args]"  → "scripts/foo.sh"
//   - "scripts/foo.sh [args]"       → "scripts/foo.sh"
//   - "cd go && go test ..."        → "" (inline go command, no script to check)
//   - "cd go && go run ..."         → "" (inline go command, no script to check)
//
// Only words beginning with "scripts/" are treated as script refs. Words that
// start with "go/" are Go package paths passed to the toolchain, not files to
// stat. Commands starting with "cd " are inline shell pipelines; they are
// considered valid (not pointing at a missing file) so the validator skips the
// script-existence check rather than erroring on them.
func extractScriptPath(command string) string {
	trimmed := strings.TrimSpace(command)

	// Inline shell pipeline starting with "cd" — no script file to check.
	if strings.HasPrefix(trimmed, "cd ") {
		return ""
	}

	words := strings.Fields(trimmed)
	for _, w := range words {
		if strings.HasPrefix(w, "scripts/") {
			return w
		}
	}
	return ""
}
