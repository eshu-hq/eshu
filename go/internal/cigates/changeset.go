// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"slices"
	"strings"
)

// goLaneGateIDs are the registry gates that judge compiled Go sources without
// executing tests: build compiles, vet analyzes, golangci-lint lints, and
// gofumpt formats. A shell-only or JSON/config-only change cannot alter their
// outcome — shell sources are never compiled (no .sh file in the tree is
// embedded in Go sources, verified 2026-11-19) and data-file content never
// changes whether Go sources compile, vet, lint, or format — so the local
// lane skips them while every proven-consumer lane keeps running.
//
// Deliberately absent: go-test-race executes the replay test packages, so a
// test-input change (JSON or shell fixture) can change its verdict; skipping
// it would first fail in CI. TestGoLaneGateIDsPinnedToRegistry pins this set
// against the committed registry so a renamed or moved lane fails loudly.
var goLaneGateIDs = map[string]struct{}{
	"go-build": {},
	"go-lint":  {},
	"go-vet":   {},
	"go-fmt":   {},
}

// IsShellScriptPath reports whether path names a shell script. The match is
// suffix-only on purpose: like the docs set, the change classes below answer
// "what kind of file is this", never whether it exists.
func IsShellScriptPath(path string) bool {
	return strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".bash")
}

// IsJSONConfigPath reports whether path names a JSON or config data file:
// JSON plus the YAML/TOML shapes operators edit alongside it. CI workflow
// files (.github/workflows/*.yml) count by shape; a lane that names one as a
// literal trigger keeps selecting through the proven-consumer exception.
func IsJSONConfigPath(path string) bool {
	return strings.HasSuffix(path, ".json") ||
		strings.HasSuffix(path, ".yaml") ||
		strings.HasSuffix(path, ".yml") ||
		strings.HasSuffix(path, ".toml")
}

// allShellChanged reports whether every changed path is a shell script. An
// empty path list is not shell-only: like allDocsSetChanged it fails closed so
// truncated giant PRs keep the full set.
func allShellChanged(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !IsShellScriptPath(path) || slices.Contains(lockstepDocsPaths, path) {
			return false
		}
	}
	return true
}

// allJSONConfigChanged reports whether every changed path is a JSON or config
// data file. A lockstep test input stays code even when it is JSON-shaped:
// editing it alone must still run the asserting test's lanes, so it never
// counts toward the config-only class.
func allJSONConfigChanged(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !IsJSONConfigPath(path) || slices.Contains(lockstepDocsPaths, path) {
			return false
		}
	}
	return true
}

// gateLiterallyTriggers reports whether gate names any changed path as a
// literal (glob-free) trigger. A literal trigger is the registry's proof that
// the gate consumes that exact file — go-fmt/go-lint name their own
// implementation scripts this way — as opposed to a blanket tree pattern such
// as go/** that matches the file only by address.
func gateLiterallyTriggers(gate Gate, changed []string) bool {
	for _, trigger := range gate.Triggers {
		if strings.Contains(trigger, "*") {
			continue
		}
		for _, path := range changed {
			if MatchGlob(trigger, path) {
				return true
			}
		}
	}
	return false
}

// changeClassSkipsGoLane reports whether gate, one of the Go compile lanes,
// must not select for changed: the change is shell-only or JSON/config-only,
// so it cannot affect the lane's verdict, and the lane does not name any
// changed file as a literal trigger (its proven consumers stay selected).
//
// Local layer only. RequiredGates is untouched on purpose: CI's `code` dorny
// filter still matches shell and config paths, so those jobs still run in CI.
// Narrowing merge enforcement awaits the CI-side filter change with
// fixture-PR proof (a #9315 followup); shrinking the local preflight while CI
// still enforces everything cannot weaken the merge.
func changeClassSkipsGoLane(gate Gate, changed []string) bool {
	if _, ok := goLaneGateIDs[gate.ID]; !ok {
		return false
	}
	if !allShellChanged(changed) && !allJSONConfigChanged(changed) {
		return false
	}
	return !gateLiterallyTriggers(gate, changed)
}

// changeClassName names the shell/JSON change class for skip reasons.
func changeClassName(changed []string) string {
	if allShellChanged(changed) {
		return "shell-only"
	}
	return "json/config-only"
}
