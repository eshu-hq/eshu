// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

// fullCodeApplicable reads the trusted caller's code filter. This exact
// pattern set uses only globs supported by MatchGlob. An unknown policy cannot
// prove a legitimate Go-lane skip, so it leaves full success unmarked.
func fullCodeApplicable(root string, paths []string, truncated bool) (bool, error) {
	filters, every, err := cigates.DornyFiltersFile(filepath.Join(root, ".github/workflows/test.yml"))
	if err != nil {
		return false, err
	}
	patterns := filters["code"]
	want := []string{"**", "!docs/**", "!*.md", "!**/*.md", "!*.json", "!docs/**/*.json", "!scripts/**/*.json", "!mkdocs.yml", "!.github/**/*.md", "!.agents/**"}
	if !every || !slices.Equal(patterns, want) {
		return false, fmt.Errorf("build test code filter has an unrecognized selection policy")
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
		selected := true
		for _, pattern := range patterns {
			if negated, ok := strings.CutPrefix(pattern, "!"); ok {
				selected = selected && !cigates.MatchGlob(negated, path)
			} else {
				selected = selected && cigates.MatchGlob(pattern, path)
			}
		}
		if selected {
			return true, nil
		}
	}
	return false, nil
}
