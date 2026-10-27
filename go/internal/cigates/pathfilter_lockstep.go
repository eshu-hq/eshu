// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DornyFilterStep is one dorny/paths-filter step's parsed filters, in the
// order the steps appear in the workflow file (jobs visited in sorted key
// order, steps in document order).
type DornyFilterStep struct {
	Filters map[string][]string
	Every   bool
	Job     string
}

// DornyFilterSteps returns every dorny/paths-filter step in raw workflow
// file bytes. Most workflows host exactly one; the code-gated workflows
// host two: the `code` step (predicate-quantifier `every`, negations
// subtract) and the `lockstep` step (default `some`, manifest-derived
// positives). Callers must select the step by filter key, never by
// position: step order is a document detail, not a contract.
func DornyFilterSteps(raw []byte) []DornyFilterStep {
	var wf pathFilterWorkflowFile
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return nil
	}
	jobKeys := make([]string, 0, len(wf.Jobs))
	for jobKey := range wf.Jobs {
		jobKeys = append(jobKeys, jobKey)
	}
	sort.Strings(jobKeys)
	var steps []DornyFilterStep
	for _, jobKey := range jobKeys {
		for _, step := range wf.Jobs[jobKey].Steps {
			if !strings.Contains(step.Uses, "dorny/paths-filter") {
				continue
			}
			s, ok := step.With["filters"].(string)
			if !ok {
				continue
			}
			var filters map[string][]string
			if err := yaml.Unmarshal([]byte(s), &filters); err != nil {
				continue
			}
			if len(filters) == 0 {
				continue
			}
			q, _ := step.With["predicate-quantifier"].(string)
			steps = append(steps, DornyFilterStep{Filters: filters, Every: q == "every", Job: jobKey})
		}
	}
	return steps
}

// DornyFilterStepWithKey returns the first dorny step whose filters contain
// key, or false when no step carries it. Missing is distinct from empty:
// callers proving CI selection must fail loudly on false rather than treat
// a renamed filter as "nothing selected".
func DornyFilterStepWithKey(steps []DornyFilterStep, key string) (DornyFilterStep, bool) {
	for _, step := range steps {
		if _, ok := step.Filters[key]; ok {
			return step, true
		}
	}
	return DornyFilterStep{}, false
}

// DornyEveryMatch reports whether any path survives every pattern, the
// predicate-quantifier `every` semantics: a positive pattern the path must
// match, a `!`-prefixed negation it must not. It mirrors the transfer
// applicability check, which pins the exact pattern list separately.
func DornyEveryMatch(patterns, paths []string) bool {
	for _, path := range paths {
		if path == "" {
			continue
		}
		selected := true
		for _, pattern := range patterns {
			if negated, ok := strings.CutPrefix(pattern, "!"); ok {
				selected = selected && !MatchGlob(negated, path)
			} else {
				selected = selected && MatchGlob(pattern, path)
			}
		}
		if selected {
			return true
		}
	}
	return false
}

// DornySomeMatch reports whether any path matches any pattern, the dorny
// default (`some`) semantics the all-positive lockstep filter relies on: a
// PR touching any listed test input must run the code-gated lanes.
func DornySomeMatch(patterns, paths []string) bool {
	for _, path := range paths {
		if path == "" {
			continue
		}
		for _, pattern := range patterns {
			if negated, ok := strings.CutPrefix(pattern, "!"); ok {
				if !MatchGlob(negated, path) {
					return true
				}
				continue
			}
			if MatchGlob(pattern, path) {
				return true
			}
		}
	}
	return false
}

// LockstepManifestPaths reads the canonical docs lockstep manifest: the
// markdown and JSON files whose content is an asserted test input. One
// repo-root-relative path per line; `#` comments and blank lines skipped.
// The manifest is the single source of truth the dorny `lockstep` filters
// and the Go docs set both follow.
func LockstepManifestPaths(root string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "specs", "ci-docs-lockstep-paths.txt")) // #nosec G304 -- caller supplies a repository checkout root.
	if err != nil {
		return nil, fmt.Errorf("read lockstep manifest: %w", err)
	}
	var paths []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("lockstep manifest holds no paths")
	}
	return paths, nil
}
