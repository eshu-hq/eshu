// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
		rules []hostedRepoRule
	}{
		{
			name:  "single explicit repository",
			rules: []hostedRepoRule{{Kind: repoRuleExact, Value: "acme/payments-api"}},
		},
		{
			name: "multiple explicit repositories",
			rules: []hostedRepoRule{
				{Kind: repoRuleExact, Value: "acme/payments-api"},
				{Kind: repoRuleExact, Value: "acme/payments-worker"},
			},
		},
		{
			name:  "anchored scoped prefix pattern",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: "^acme/payments-"}},
		},
		{
			name:  "scoped prefix with team slug",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: "acme/checkout-.*"}},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict := classifyRepoRules(tc.rules)
			if verdict.Broad {
				t.Fatalf("classifyRepoRules() Broad = true, want narrow; reason = %q", verdict.Reason)
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
		rules []hostedRepoRule
	}{
		{
			name:  "whole org star glob",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: "acme/*"}},
		},
		{
			name:  "bare star",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: "*"}},
		},
		{
			name:  "match-everything regex",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: ".*"}},
		},
		{
			name:  "org slash dot star",
			rules: []hostedRepoRule{{Kind: repoRulePattern, Value: "acme/.*"}},
		},
		{
			name:  "empty org-wide selection",
			rules: nil,
		},
		{
			name:  "explicit repo plus broad glob",
			rules: []hostedRepoRule{{Kind: repoRuleExact, Value: "acme/api"}, {Kind: repoRulePattern, Value: "acme/*"}},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verdict := classifyRepoRules(tc.rules)
			if !verdict.Broad {
				t.Fatalf("classifyRepoRules() Broad = false, want broad for %q", tc.name)
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
	if _, err := parseHostedRepoRules([]string{"glob:acme/*"}); err == nil {
		t.Fatal("parseHostedRepoRules() err = nil, want error for unknown kind")
	}
}

// TestParseRepoRuleFlagsParsesKinds proves the flag parser accepts both the
// repo: and pattern: prefixes and a bare value (treated as an exact repo).
func TestParseRepoRuleFlagsParsesKinds(t *testing.T) {
	t.Parallel()
	rules, err := parseHostedRepoRules([]string{"repo:acme/api", "pattern:^acme/pay-", "acme/worker"})
	if err != nil {
		t.Fatalf("parseHostedRepoRules() err = %v, want nil", err)
	}
	if len(rules) != 3 {
		t.Fatalf("parsed %d rules, want 3", len(rules))
	}
	if rules[0].Kind != repoRuleExact || rules[0].Value != "acme/api" {
		t.Fatalf("rule[0] = %+v, want exact acme/api", rules[0])
	}
	if rules[1].Kind != repoRulePattern || rules[1].Value != "^acme/pay-" {
		t.Fatalf("rule[1] = %+v, want pattern ^acme/pay-", rules[1])
	}
	if rules[2].Kind != repoRuleExact || rules[2].Value != "acme/worker" {
		t.Fatalf("rule[2] = %+v, want exact acme/worker", rules[2])
	}
}

// TestParseRepoRuleFlagsRejectsInvalidPattern proves an uncompilable regex is
// rejected at parse time so a broken rule never reaches classification.
func TestParseRepoRuleFlagsRejectsInvalidPattern(t *testing.T) {
	t.Parallel()
	if _, err := parseHostedRepoRules([]string{"pattern:([unclosed"}); err == nil {
		t.Fatal("parseHostedRepoRules() err = nil, want error for invalid regex")
	}
}
