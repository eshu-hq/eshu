// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// verifyScriptRE captures a gate's own verify script from its local command.
// Only `scripts/verify-*.sh` qualifies: a wrapper such as
// scripts/dev/precommit-go.sh runs several gates' checks and says nothing about
// where any one of them is enforced in CI, and a `test-verify-*.sh` harness is
// the self-test rather than the gate.
var verifyScriptRE = regexp.MustCompile(`scripts/verify-[\w.-]+\.sh`)

// runStep is the minimal shape needed to read a step's executable command.
type runStep struct {
	Run string `yaml:"run"`
}

type runJob struct {
	Steps []runStep `yaml:"steps"`
}

type runWorkflowFile struct {
	Jobs map[string]runJob `yaml:"jobs"`
}

// workflowRunCommands returns every step `run:` block in raw, which is the only
// place a workflow actually executes a script.
//
// Scanning the whole file instead counts a mention as an invocation, and the
// two are routinely different: a dorny paths filter WATCHES scripts so the job
// re-runs when they change. scripts/verify-golden-corpus-gate.sh is watched by
// static-contract-gates.yml's maturitydrift filter and executed only by
// golden-corpus-gate.yml, so a whole-file scan sees two hosts, fails the
// "exactly one workflow" precondition, and silently skips golden-corpus-gate --
// letting a cross-wired entry through the check built to catch it (#5748
// review).
func workflowRunCommands(raw []byte) []string {
	var wf runWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil
	}
	var cmds []string
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if step.Run != "" {
				cmds = append(cmds, step.Run)
			}
		}
	}
	return cmds
}

// workflowRunsScript reports whether raw invokes exactly this script.
//
// The leading boundary is why this is not a plain strings.Contains. A workflow
// running "myscripts/verify-no-ai-attribution.sh" contains
// "scripts/verify-no-ai-attribution.sh" as a substring while running a
// different file. Counting it as a host gives the script two apparent owners,
// which fails the "exactly one workflow" precondition below, which silently
// skips the gate -- so a real mismatch becomes a pass, through the very check
// meant to catch it (#5748).
//
// Note the sibling "scripts/test-verify-X.sh" harness is NOT such a case: the
// "scripts/" prefix in the search string already excludes it. Only a path with
// something glued to the left of "scripts/" collides.
func workflowRunsScript(raw, script string) bool {
	for _, cmd := range workflowRunCommands([]byte(raw)) {
		if commandRunsScript(cmd, script) {
			return true
		}
	}
	return false
}

// commandRunsScript reports whether a single `run:` block invokes script.
func commandRunsScript(raw, script string) bool {
	for i := 0; i+len(script) <= len(raw); i++ {
		if raw[i:i+len(script)] != script {
			continue
		}
		if i == 0 {
			return true
		}
		switch c := raw[i-1]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '/', c == '-', c == '_':
		default:
			return true
		}
	}
	return false
}

// checkVerifyScriptWorkflowMatch validates that a gate whose verify script is
// invoked by exactly one workflow declares that workflow as its ci.workflow.
//
// checkJobNamesResolve only proves membership -- that the declared ci.job is a
// real job in the declared workflow -- not correspondence, so a gate can point
// at a real job that does not run its check and pass drift validation. That is
// how no-ai-attribution came to declare test.yml / docs-helm-hygiene while the
// check actually runs in verify-agent-hygiene.yml (#5748).
//
// Correspondence in general is NOT checkable from script names in this repo,
// and this check is deliberately narrow because of it: a gate's local
// entrypoint and its CI entrypoint are legitimately different artifacts. CI
// runs golangci-lint directly where the local gate runs precommit-go.sh; it
// runs generate-contracttest.sh in a regenerate-and-diff shape where the local
// gate runs verify-contracttest.sh; trivy-fs-local.sh is a local convenience
// wrapper for an action-driven CI scan. Requiring the local script to appear in
// the declared job flags 16 gates, and 15 of those are correct wiring.
//
// "Appears in exactly one workflow" is the sound subset: if a verify script is
// invoked by precisely one workflow, that workflow is unambiguously where the
// gate runs, and any other declaration is wrong. A script no workflow runs, or
// one several run, carries no such signal and is skipped rather than guessed --
// the same convention the rest of this package follows. Measured against the
// committed registry this checks 29 gates and, before the accompanying fix,
// flagged exactly one: the real defect.
func checkVerifyScriptWorkflowMatch(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return nil
	}

	raws := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(wfDir, e.Name())) // #nosec G304 -- wfDir-confined listing
		if err != nil {
			continue
		}
		raws[e.Name()] = string(b)
	}

	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.Local == nil || g.Local.Command == "" {
			continue
		}
		script := verifyScriptRE.FindString(g.Local.Command)
		if script == "" {
			continue
		}

		var hosts []string
		for name, raw := range raws {
			if workflowRunsScript(raw, script) {
				hosts = append(hosts, name)
			}
		}
		if len(hosts) != 1 {
			// No workflow runs it (CI uses a different entrypoint), or several
			// do (no single owner). Neither carries a correspondence signal.
			continue
		}
		if filepath.Base(g.CI.Workflow) != hosts[0] {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q declares ci.workflow %q, but its verify script %s is invoked only by %q -- "+
					"the declared workflow does not run this gate's check; point ci.workflow/ci.job at the workflow that does",
				g.ID, g.CI.Workflow, script, hosts[0],
			))
		}
	}
	return errs
}
