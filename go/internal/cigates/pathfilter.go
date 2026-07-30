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

// appendGateKeyDisplayRE captures the dorny filter key (2nd positional arg)
// and the display name (3rd positional arg) of an append_gate call in a
// matrix-dispatch workflow such as static-contract-gates.yml:
// append_gate "<selected>" "<key>" "<display>" ...
var appendGateKeyDisplayRE = regexp.MustCompile(`append_gate\s+"[^"]*"\s+"([^"]*)"\s+"([^"]*)"`)

// pathFilterStep is the minimal shape of a workflow step needed to find a
// dorny/paths-filter step and read its `with.filters` block-scalar value.
type pathFilterStep struct {
	Uses string                 `yaml:"uses"`
	With map[string]interface{} `yaml:"with"`
}

type pathFilterJob struct {
	Steps []pathFilterStep `yaml:"steps"`
}

type pathFilterWorkflowFile struct {
	Jobs map[string]pathFilterJob `yaml:"jobs"`
}

// dornyFilters returns the parsed "key -> path glob list" map from the first
// dorny/paths-filter step found in raw, or nil if no such step is present or
// its filters block does not parse. A workflow with no such step (true for
// most workflows) returns nil so callers skip it rather than false-erroring.
func dornyFilters(raw []byte) map[string][]string {
	var wf pathFilterWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil
	}
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if !strings.Contains(step.Uses, "dorny/paths-filter") {
				continue
			}
			rawFilters, ok := step.With["filters"]
			if !ok {
				continue
			}
			s, ok := rawFilters.(string)
			if !ok {
				continue
			}
			var filters map[string][]string
			if err := yaml.Unmarshal([]byte(s), &filters); err != nil {
				continue
			}
			if len(filters) > 0 {
				return filters
			}
		}
	}
	return nil
}

// appendGateKeysByDisplay returns a map of append_gate display name -> dorny
// filter key, parsed from every append_gate call found in raw.
func appendGateKeysByDisplay(raw []byte) map[string]string {
	out := make(map[string]string)
	for _, m := range appendGateKeyDisplayRE.FindAllSubmatch(raw, -1) {
		key := string(m[1])
		display := string(m[2])
		if key != "" && display != "" {
			out[display] = key
		}
	}
	return out
}

// isLiteralTrigger reports whether a registry trigger names a single
// concrete file path rather than a glob (no `*` anywhere). Only a literal
// trigger has one real path to test against a CI path-filter glob with
// MatchGlob; a glob-form trigger (e.g. "go/internal/**") has no single
// concrete path, so checkPathFilterCoverage skips it rather than guessing at
// a glob-vs-glob equivalence.
func isLiteralTrigger(trigger string) bool {
	return !strings.Contains(trigger, "*")
}

// checkPathFilterCoverage validates that every LITERAL (non-glob) registry
// trigger of a gate whose CI workflow uses a dorny/paths-filter matrix
// dispatch (e.g. static-contract-gates.yml) is actually matched by at least
// one glob in that workflow's corresponding filter key, using the same glob
// semantics dorny/paths-filter itself uses (MatchGlob: a single `*` does not
// cross `/`).
//
// This is the registry-vs-CI-filter cross-check for the drift class #5855's
// third review round found: specs/ci-gates.v1.yaml's triggers and
// .github/workflows/static-contract-gates.yml's hand-duplicated dorny
// filters are two independently-edited lists with no prior machine link
// between them, so a trigger can be added to one and never the other,
// leaving CI silently unable to select the gate for a PR that touches only
// that file. A gate whose CI workflow does not use this pattern (no
// dorny/paths-filter step, or no append_gate call naming this gate's
// ci.job) is skipped rather than false-erroring: this check only fires
// where the mapping from gate to filter key is unambiguous.
func checkPathFilterCoverage(repoRoot string, reg *Registry) []error {
	wfDir := filepath.Join(repoRoot, ".github", "workflows")
	rawCache := make(map[string][]byte)
	filtersCache := make(map[string]map[string][]string)
	keysCache := make(map[string]map[string]string)

	var errs []error
	for _, g := range reg.Gates {
		if g.CI.Workflow == "" || g.CI.Job == "" {
			continue
		}
		raw, cached := rawCache[g.CI.Workflow]
		if !cached {
			// filepath.Base strips any path component so the read is confined to
			// wfDir; the filename comes from the committed ci-gates.v1.yaml
			// registry, not runtime input.
			p := filepath.Join(wfDir, filepath.Base(g.CI.Workflow))
			r, err := os.ReadFile(p) // #nosec G304 -- wfDir-confined path; filename is registry-controlled, not runtime input
			if err != nil {
				r = nil
			}
			raw = r
			rawCache[g.CI.Workflow] = raw
		}
		if raw == nil {
			// Missing/unreadable workflow is reported by checkWorkflowCompleteness;
			// do not double-report here.
			continue
		}

		filters, cached := filtersCache[g.CI.Workflow]
		if !cached {
			filters = dornyFilters(raw)
			filtersCache[g.CI.Workflow] = filters
		}
		if filters == nil {
			// Workflow does not use the dorny/paths-filter matrix pattern.
			continue
		}

		keys, cached := keysCache[g.CI.Workflow]
		if !cached {
			keys = appendGateKeysByDisplay(raw)
			keysCache[g.CI.Workflow] = keys
		}
		key, ok := keys[g.CI.Job]
		if !ok {
			// No append_gate call names this gate's ci.job; the mapping from
			// gate to filter key is ambiguous, so skip rather than guess.
			continue
		}

		patterns, ok := filters[key]
		if !ok {
			errs = append(errs, fmt.Errorf(
				"drift: gate %q resolves to dorny filter key %q via append_gate, but %q's filters block has no %q entry",
				g.ID, key, g.CI.Workflow, key,
			))
			continue
		}

		for _, trig := range g.Triggers {
			if !isLiteralTrigger(trig) {
				continue
			}
			matched := false
			for _, p := range patterns {
				if MatchGlob(p, trig) {
					matched = true
					break
				}
			}
			if !matched {
				errs = append(errs, fmt.Errorf(
					"drift: gate %q trigger %q is not matched by any pattern in %q's %q dorny path filter "+
						"(gitignore-style glob: a single '*' does not cross '/') -- a PR touching only that file "+
						"would not select this gate's CI check; add a matching pattern to the filter or fix the trigger",
					g.ID, trig, g.CI.Workflow, key,
				))
			}
		}
	}
	return errs
}
