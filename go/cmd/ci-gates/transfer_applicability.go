// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// fullCodeApplicable reads the trusted caller's code filter. This exact
// pattern set uses only globs supported by MatchGlob. An unknown policy
// cannot prove a legitimate Go-lane skip, so it leaves full success unmarked.
// Lockstep test inputs count as code: the dorny `lockstep` filter (manifest
// positives, `some` quantifier) runs the same lanes for them, so a change
// the code filter swallows but the lockstep filter selects still proves
// full success only through actually successful lanes.
func fullCodeApplicable(root string, paths []string, truncated bool) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/test.yml")) // #nosec G304 -- caller supplies a repository checkout root.
	if err != nil {
		return false, err
	}
	var code, lockstep *cigates.DornyFilterStep
	for _, step := range cigates.DornyFilterSteps(raw) {
		if _, ok := step.Filters["code"]; ok && code == nil {
			s := step
			code = &s
		}
		if _, ok := step.Filters["lockstep"]; ok && lockstep == nil {
			s := step
			lockstep = &s
		}
	}
	if code == nil || !code.Every {
		return false, fmt.Errorf("build test code filter has an unrecognized selection policy")
	}
	patterns := code.Filters["code"]
	want := []string{"**", "!docs/**", "!*.md", "!**/*.md", "!*.json", "!docs/**/*.json", "!scripts/**/*.json", "!mkdocs.yml", "!.github/**/*.md", "!.agents/**"}
	if !slices.Equal(patterns, want) {
		return false, fmt.Errorf("build test code filter has an unrecognized selection policy")
	}
	if lockstep == nil || lockstep.Every {
		return false, fmt.Errorf("build test lockstep filter has an unrecognized selection policy")
	}
	manifest, err := cigates.LockstepManifestPaths(root)
	if err != nil {
		return false, err
	}
	if !slices.Equal(lockstep.Filters["lockstep"], manifest) {
		return false, fmt.Errorf("build test lockstep filter drifts from the lockstep manifest")
	}
	if truncated {
		return true, nil
	}
	if len(paths) == 0 {
		return false, fmt.Errorf("cannot prove code applicability without changed paths")
	}
	for _, path := range paths {
		if path == "" {
			return false, fmt.Errorf("changed path is empty")
		}
	}
	return cigates.DornyEveryMatch(patterns, paths) || cigates.DornySomeMatch(lockstep.Filters["lockstep"], paths), nil
}
