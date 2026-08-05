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
	errs := r.ValidateRequiredStatusChecks()
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
	for _, check := range r.RequiredStatusChecks {
		if check.Workflow == "" {
			continue
		}
		wfPath := filepath.Join(repoRoot, ".github", "workflows", check.Workflow)
		if _, err := os.Stat(wfPath); os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf(
				"required status context %q: workflow file %q not found",
				check.Context,
				wfPath,
			))
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

// extractScriptPaths returns every repo-relative scripts/ token in command,
// in left-to-right order, or nil when command references no scripts/ file
// (e.g. an inline go-toolchain invocation like "cd go && go test ..."). A
// gate's local command can chain more than one scripts/ invocation with
// "&&" — frontend-console-checks and ci-gate-registry both do — and every one
// of them is a file a caller may need to see, not only the first.
//
// Recognised patterns:
//
//   - "bash scripts/foo.sh [args]"       → ["scripts/foo.sh"]
//   - "scripts/foo.sh [args]"            → ["scripts/foo.sh"]
//   - "a && bash scripts/b.sh"           → ["scripts/b.sh"] (every scripts/ token)
//   - "cd go && go test ..."             → nil (inline go command, no script)
//   - "cd go && go run ..."              → nil (inline go command, no script)
//
// Only words beginning with "scripts/" are treated as script refs. Words that
// start with "go/" are Go package paths passed to the toolchain, not files to
// stat. A word that references a scripts/ file through a relative prefix
// ("../scripts/foo.txt") is not recognised — heredoc-budget's local.command
// is the one gate command in the registry with that shape, and its file is
// covered by an explicit registry trigger instead of being derived here (see
// checkScriptTriggerCoverage's doc comment in scripttrigger.go). Commands
// starting with "cd " are inline shell pipelines; they are considered valid
// (not pointing at a missing file) so callers skip rather than error on them.
func extractScriptPaths(command string) []string {
	trimmed := strings.TrimSpace(command)

	// Inline shell pipeline starting with "cd" — no script file to check.
	if strings.HasPrefix(trimmed, "cd ") {
		return nil
	}

	var out []string
	for _, w := range strings.Fields(trimmed) {
		if strings.HasPrefix(w, "scripts/") {
			out = append(out, w)
		}
	}
	return out
}

// extractScriptPath returns the leading repo-relative script path from a
// shell command string — the one Validate stat-checks for existence — or ""
// when the command references no scripts/ file. Use extractScriptPaths
// directly when every scripts/ token in a compound command matters, not only
// the first.
func extractScriptPath(command string) string {
	paths := extractScriptPaths(command)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
