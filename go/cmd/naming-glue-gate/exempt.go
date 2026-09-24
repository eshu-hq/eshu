// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"
)

// defaultExemptFile is the repo-relative ledger of owner-approved
// glued-compound directory names, resolved against -repo-root unless
// -exempt-file overrides it.
const defaultExemptFile = "scripts/lib/naming-glue-exempt.tsv"

// loadExemptPaths parses the exemption ledger at path: one row per exempt
// directory, repo-relative directory path, then the reason, then the approver
// or issue, tab-separated. Lines starting with '#' and blank lines are
// ignored. A missing file means no exemptions and is not an error; a
// malformed row is, failing the same loud way an unresolvable git state
// does, so a corrupted ledger can never silently unexempt a name.
func loadExemptPaths(path string) (map[string]string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a CLI flag defaulting into the repo, not external input.
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading exemption ledger %s: %w", path, err)
	}
	exempt := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 || strings.TrimSpace(cols[0]) == "" || strings.TrimSpace(cols[1]) == "" || strings.TrimSpace(cols[2]) == "" {
			return nil, fmt.Errorf("exemption ledger %s line %d: want <repo-relative dir> TAB <reason> TAB <approver or issue>, got %q", path, i+1, line)
		}
		exempt[strings.TrimSpace(cols[0])] = strings.TrimSpace(cols[1])
	}
	return exempt, nil
}

// filterExempt drops candidates whose repo-relative path carries an
// owner-approved exemption. The match is on the full path, never the
// basename alone, so an exemption cannot leak onto a same-named directory
// anywhere else.
func filterExempt(candidates []Candidate, exempt map[string]string) []Candidate {
	if len(exempt) == 0 {
		return candidates
	}
	var kept []Candidate
	for _, c := range candidates {
		if _, ok := exempt[c.Path]; !ok {
			kept = append(kept, c)
		}
	}
	return kept
}
