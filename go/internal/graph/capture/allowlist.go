// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// upstreamIssueRE pins the allowlist's upstream contract: the link must
// point at a real issue on a GitHub repository, so every excused divergence
// stays tracked against a fixable item rather than a wiki page or a bare
// repo root.
var upstreamIssueRE = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/issues/[0-9]+$`)

// AllowlistEntry is one accepted backend divergence: the normalized
// statement text it excuses (plus optional bound parameters), the divergence
// kind it excuses (see the tier vocabulary below), the reason it is
// accepted, the upstream issue tracking it, and its owner.
type AllowlistEntry struct {
	Statement  string `yaml:"statement"`
	Parameters string `yaml:"parameters"`
	Tier       string `yaml:"tier"`
	Reason     string `yaml:"reason"`
	Upstream   string `yaml:"upstream"`
	Owner      string `yaml:"owner"`
}

// The tier vocabulary scopes an entry to one divergence kind, so an excuse
// for scheduling noise can never cover a result disagreement on the same
// statement. "statement" excuses any kind on the named statement (a genuine
// whole-statement dialect divergence); any other tier excuses only its named
// kind (see backendconformance.Divergence*). An executions-tier entry is
// result-agreement-conditional by construction — it only matches when both
// backends returned the same result sets — so it is exempt from the stale
// match requirement: poll iteration counts agree exactly on some runs, and a
// flap between stale-failure and excuse would make the gate nondeterministic.
var allowlistTiers = map[string]string{
	"statement":  "",
	"missing":    backendconformance.DivergenceMissing,
	"results":    backendconformance.DivergenceResults,
	"executions": backendconformance.DivergenceExecutions,
	"failures":   backendconformance.DivergenceFailures,
	"rowcount":   backendconformance.DivergenceRowCount,
}

// Allowlist is the parsed divergence allowlist. It is empty by default:
// every divergence fails the gate until an entry names it with a reason
// and an upstream issue link.
type Allowlist struct {
	entries []AllowlistEntry
}

// ParseAllowlist parses and validates raw allowlist YAML. A missing reason
// or a missing/non-issue upstream link fails the parse, so a divergence
// can never be silenced without an explanation and a tracked issue.
func ParseAllowlist(raw []byte) (*Allowlist, error) {
	var document struct {
		Entries []AllowlistEntry `yaml:"entries"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse divergence allowlist: %w", err)
	}
	for i, entry := range document.Entries {
		if strings.TrimSpace(entry.Statement) == "" {
			return nil, fmt.Errorf("divergence allowlist entry %d: statement is required", i)
		}
		if _, ok := allowlistTiers[strings.TrimSpace(entry.Tier)]; !ok {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): tier must be one of statement, missing, results, executions, failures, rowcount", i, entry.Statement)
		}
		if strings.TrimSpace(entry.Reason) == "" {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): reason is required", i, entry.Statement)
		}
		if !upstreamIssueRE.MatchString(strings.TrimSpace(entry.Upstream)) {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): upstream must be a GitHub issue URL", i, entry.Statement)
		}
	}
	return &Allowlist{entries: document.Entries}, nil
}

// Excuse removes the divergences the allowlist names and returns the rest.
// An allowlist entry must match at least one divergence in the run:
// Validate reports entries that matched nothing, so a fixed bug cannot
// linger as a permanent exemption.
func (a *Allowlist) Excuse(diffs []backendconformance.DifferentialDifference) ([]backendconformance.DifferentialDifference, error) {
	if a == nil {
		return diffs, nil
	}
	matched := make([]bool, len(a.entries))
	var remaining []backendconformance.DifferentialDifference
	for _, diff := range diffs {
		excused := false
		for i, entry := range a.entries {
			if entryMatches(entry, diff) {
				matched[i] = true
				excused = true
				break
			}
		}
		if !excused {
			remaining = append(remaining, diff)
		}
	}
	for i, entry := range a.entries {
		kind := allowlistTiers[strings.TrimSpace(entry.Tier)]
		if !matched[i] && kind != backendconformance.DivergenceExecutions {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): matched no divergence in this run (stale)", i, entry.Statement)
		}
	}
	return remaining, nil
}

// Validate reports whether every allowlist entry matched at least one of
// the run's divergences, without excusing anything. It is the check the
// gate runs even when the diff itself is clean, so a stale entry fails on
// a green run too.
func (a *Allowlist) Validate(diffs []backendconformance.DifferentialDifference) error {
	_, err := a.Excuse(diffs)
	return err
}

// entryMatches reports whether entry excuses diff. Parameters narrow the
// match when set; an empty parameters field excuses the statement under
// any bindings. The tier scopes the match to one divergence kind, except
// the statement tier, which excuses any kind on the named statement.
func entryMatches(entry AllowlistEntry, diff backendconformance.DifferentialDifference) bool {
	if entry.Statement != diff.Fingerprint.Statement {
		return false
	}
	kind, ok := allowlistTiers[strings.TrimSpace(entry.Tier)]
	if !ok {
		return false
	}
	if kind != "" && kind != diff.Kind {
		return false
	}
	return entry.Parameters == "" || entry.Parameters == diff.Fingerprint.Parameters
}
