// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"strings"
	"testing"
)

// TestClassifyRepoRulesNarrowVariants proves that explicit repositories and a
// scoped prefix selector are classified as narrow so onboarding proceeds without
// requiring the broad-ingestion confirmation.
func TestClassifyRepoRulesNarrowVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		rules []RepoRule
	}{
		{
			name:  "single explicit repository",
			rules: []RepoRule{{Kind: RuleExact, Value: "acme/payments-api"}},
		},
		{
			name: "multiple explicit repositories",
			rules: []RepoRule{
				{Kind: RuleExact, Value: "acme/payments-api"},
				{Kind: RuleExact, Value: "acme/payments-worker"},
			},
		},
		{
			name:  "anchored scoped prefix pattern",
			rules: []RepoRule{{Kind: RulePattern, Value: "^acme/payments-"}},
		},
		{
			name:  "scoped prefix with team slug",
			rules: []RepoRule{{Kind: RulePattern, Value: "acme/checkout-.*"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict := ClassifyRepoRules(tc.rules)
			if verdict.Broad {
				t.Fatalf("ClassifyRepoRules() Broad = true, want narrow; reason = %q", verdict.Reason)
			}
		})
	}
}

// TestClassifyRepoRulesBroadVariants proves that whole-org globs and
// unconstrained org-wide selectors are classified as broad so accidental
// org-wide ingestion is rejected unless explicitly confirmed.
func TestClassifyRepoRulesBroadVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		rules []RepoRule
	}{
		{
			name:  "whole org star glob",
			rules: []RepoRule{{Kind: RulePattern, Value: "acme/*"}},
		},
		{
			name:  "bare star",
			rules: []RepoRule{{Kind: RulePattern, Value: "*"}},
		},
		{
			name:  "match-everything regex",
			rules: []RepoRule{{Kind: RulePattern, Value: ".*"}},
		},
		{
			name:  "org slash dot star",
			rules: []RepoRule{{Kind: RulePattern, Value: "acme/.*"}},
		},
		{
			name:  "empty org-wide selection",
			rules: nil,
		},
		{
			name:  "explicit repo plus broad glob",
			rules: []RepoRule{{Kind: RuleExact, Value: "acme/api"}, {Kind: RulePattern, Value: "acme/*"}},
		},
		{
			name:  "anchored whole-org glob",
			rules: []RepoRule{{Kind: RulePattern, Value: "^acme/.*$"}},
		},
		{
			name:  "org prefix with trailing slash only",
			rules: []RepoRule{{Kind: RulePattern, Value: "acme/"}},
		},
		{
			name:  "match-one-or-more regex",
			rules: []RepoRule{{Kind: RulePattern, Value: ".+"}},
		},
		{
			name:  "whitespace-only pattern",
			rules: []RepoRule{{Kind: RulePattern, Value: "   "}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict := ClassifyRepoRules(tc.rules)
			if !verdict.Broad {
				t.Fatalf("ClassifyRepoRules() Broad = false, want broad for %q", tc.name)
			}
			if strings.TrimSpace(verdict.Reason) == "" {
				t.Fatal("broad verdict must carry a human reason")
			}
		})
	}
}

// TestParseRepoRuleFlagsRejectsUnknownKind proves the flag parser rejects an
// unsupported rule kind instead of silently dropping it.
func TestParseRepoRuleFlagsRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	if _, err := ParseRepoRules([]string{"glob:acme/*"}); err == nil {
		t.Fatal("ParseRepoRules() err = nil, want error for unknown kind")
	}
}

// TestParseRepoRuleFlagsParsesKinds proves the flag parser accepts both the
// repo: and pattern: prefixes and a bare value (treated as an exact repo).
func TestParseRepoRuleFlagsParsesKinds(t *testing.T) {
	t.Parallel()
	rules, err := ParseRepoRules([]string{"repo:acme/api", "pattern:^acme/pay-", "acme/worker"})
	if err != nil {
		t.Fatalf("ParseRepoRules() err = %v, want nil", err)
	}
	if len(rules) != 3 {
		t.Fatalf("parsed %d rules, want 3", len(rules))
	}
	if rules[0].Kind != RuleExact || rules[0].Value != "acme/api" {
		t.Fatalf("rule[0] = %+v, want exact acme/api", rules[0])
	}
	if rules[1].Kind != RulePattern || rules[1].Value != "^acme/pay-" {
		t.Fatalf("rule[1] = %+v, want pattern ^acme/pay-", rules[1])
	}
	if rules[2].Kind != RuleExact || rules[2].Value != "acme/worker" {
		t.Fatalf("rule[2] = %+v, want exact acme/worker", rules[2])
	}
}

// TestParseRepoRuleFlagsRejectsInvalidPattern proves an uncompilable regex is
// rejected at parse time so a broken rule never reaches classification.
func TestParseRepoRuleFlagsRejectsInvalidPattern(t *testing.T) {
	t.Parallel()
	if _, err := ParseRepoRules([]string{"pattern:([unclosed"}); err == nil {
		t.Fatal("ParseRepoRules() err = nil, want error for invalid regex")
	}
}

// TestParseRepoRuleFlagsRejectsEmptyValues proves each empty-value shape is
// rejected with its own message rather than parsed into a rule that would then
// be classified.
func TestParseRepoRuleFlagsRejectsEmptyValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "   ", "repo:", "pattern:", "exact:  ", "regex:"} {
		if _, err := ParseRepoRules([]string{raw}); err == nil {
			t.Fatalf("ParseRepoRules(%q) err = nil, want an empty-value rejection", raw)
		}
	}
}

// TestRepoRuleStringToken proves a rule round-trips into the stable
// "kind:value" token the artifact and the rejection error both quote back.
func TestRepoRuleStringToken(t *testing.T) {
	t.Parallel()
	if got, want := (RepoRule{Kind: RuleExact, Value: "acme/api"}).String(), "exact:acme/api"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	if got, want := (RepoRule{Kind: RulePattern, Value: "^acme/"}).String(), "pattern:^acme/"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
