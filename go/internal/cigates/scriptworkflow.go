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

// scriptWorkflowSoundSubsetCount is the number of gates in the "sound
// subset" checkVerifyScriptWorkflowMatch can validate: those whose
// scripts/verify-*.sh is invoked by exactly one committed workflow file,
// measured against specs/ci-gates.v1.yaml and .github/workflows/*.yml.
//
// TestScriptWorkflowSoundSubsetCount re-derives this count from the
// committed registry and workflows on every test run and fails when it
// disagrees with this constant. That is the guard this number lacked before:
// a doc comment here once hard-coded a count of 29 gates, the registry grew
// past that silently, and nothing caught the prose going stale. The current
// count lives only in the constant below, not restated anywhere in prose, so
// there is exactly one place it can go stale. Update this constant (and
// re-run that test) whenever a registry or workflow change moves the count;
// do not hand-edit it without doing so.
const scriptWorkflowSoundSubsetCount = 41

// runStep is the minimal shape needed to read a step's executable command.
type runStep struct {
	Run              string `yaml:"run"`
	WorkingDirectory string `yaml:"working-directory"`
}

type runDefaultsRun struct {
	WorkingDirectory string `yaml:"working-directory"`
}

type runDefaults struct {
	Run runDefaultsRun `yaml:"run"`
}

type runJob struct {
	Defaults runDefaults `yaml:"defaults"`
	Steps    []runStep   `yaml:"steps"`
}

type runWorkflowFile struct {
	Defaults runDefaults       `yaml:"defaults"`
	Jobs     map[string]runJob `yaml:"jobs"`
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
// wrapper for an action-driven CI scan. The broader rule -- requiring the
// local script to appear in the declared job -- was considered and rejected
// for this reason: a one-time #5748 measurement found it flagged a double
// digit number of gates, nearly all of them legitimately wired rather than
// broken. That broader rule was never implemented, so its counts cannot be
// re-derived from shipped code; they are not repeated here as a live figure,
// only as the qualitative reason -- entrypoint divergence, not drift -- this
// narrower check exists instead.
//
// "Appears in exactly one workflow" is the sound subset: if a verify script is
// invoked by precisely one workflow, that workflow is unambiguously where the
// gate runs, and any other declaration is wrong. A script no workflow runs, or
// one several run, carries no such signal and is skipped rather than guessed --
// the same convention the rest of this package follows.
// scriptWorkflowSoundSubsetCount names the current size of that subset,
// guarded by TestScriptWorkflowSoundSubsetCount against the committed
// registry -- unlike a hard-coded figure in prose, which the registry can
// silently outgrow. Before the accompanying #5748 fix, this check flagged
// exactly one gate in that subset: the real defect.
func checkVerifyScriptWorkflowMatch(repoRoot string, reg *Registry) []error {
	subset, err := scriptWorkflowSoundSubset(repoRoot, reg)
	if err != nil {
		// An unreadable .github/workflows (missing, permission-denied, not a
		// directory) means this check cannot determine the sound subset at
		// all -- not that there is no drift. Silently returning nil here
		// would read as a clean pass in exactly the situation where a hard
		// failure is wanted (#5939 review).
		return []error{fmt.Errorf("drift: reading workflow-script sound subset: %w", err)}
	}

	var errs []error
	for _, g := range reg.Gates {
		entry, ok := subset[g.ID]
		if !ok {
			// No workflow runs the script (CI uses a different entrypoint), or
			// several do (no single owner). Neither carries a correspondence
			// signal.
			continue
		}
		if filepath.Base(g.CI.Workflow) != entry.host {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q declares ci.workflow %q, but its verify script %s is invoked only by %q -- "+
					"the declared workflow does not run this gate's check; point ci.workflow/ci.job at the workflow that does",
				g.ID, g.CI.Workflow, entry.script, entry.host,
			))
		}
	}
	return errs
}

// scriptWorkflowSoundSubsetEntry names the single workflow file that hosts a
// gate's verify script, and the script path itself for error messages.
type scriptWorkflowSoundSubsetEntry struct {
	host   string
	script string
}

// scriptWorkflowSoundSubset returns, keyed by gate id, every gate in reg.Gates
// whose scripts/verify-*.sh is invoked by exactly one committed workflow
// file -- the "sound subset" checkVerifyScriptWorkflowMatch can validate. A
// gate outside it (no host, or several) carries no correspondence signal and
// is omitted rather than guessed.
//
// This is the single derivation both checkVerifyScriptWorkflowMatch and
// TestScriptWorkflowSoundSubsetCount call, so the production check and its
// anti-drift guard can never disagree about what "the sound subset" means.
func scriptWorkflowSoundSubset(repoRoot string, reg *Registry) (map[string]scriptWorkflowSoundSubsetEntry, error) {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return nil, fmt.Errorf("read workflow dir %s: %w", wfDir, err)
	}

	raws := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		p := filepath.Join(wfDir, e.Name())
		b, err := os.ReadFile(p) // #nosec G304 -- wfDir-confined listing
		if err != nil {
			// Silently dropping one unreadable file from raws is wrong
			// signal, not absent signal: if a script is genuinely hosted by
			// this file and one other, the loop below now observes exactly
			// one host and the gate enters the sound subset as unambiguous
			// when it is not -- either a factually wrong drift error, or a
			// real mismatch masked. That taints every gate's host count, not
			// only ones this file would have hosted, so it fails the whole
			// derivation the same way an unreadable directory does, rather
			// than reporting only this one file (#5939 review).
			return nil, fmt.Errorf("read workflow file %s: %w", p, err)
		}
		raws[e.Name()] = string(b)
	}

	subset := make(map[string]scriptWorkflowSoundSubsetEntry)
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
			continue
		}
		subset[g.ID] = scriptWorkflowSoundSubsetEntry{host: hosts[0], script: script}
	}
	return subset, nil
}
