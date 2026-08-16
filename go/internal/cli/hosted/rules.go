// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hosted

import (
	"fmt"
	"regexp"
	"strings"
)

// RepoRuleKind names how a hosted onboarding repository sync rule selects
// repositories. It mirrors the deployed ESHU_REPOSITORY_RULES_JSON contract
// (exact identifiers and regex patterns) so the onboarding flow validates the
// same shapes an operator would otherwise hand-edit into Helm values.
type RepoRuleKind string

const (
	// RuleExact selects exactly one repository by its canonical
	// "owner/name" identifier. An exact rule is always narrow.
	RuleExact RepoRuleKind = "exact"
	// RulePattern selects repositories by a regular expression against the
	// canonical identifier. A pattern is narrow only when it is anchored to a
	// scoped prefix; an org-wide match-everything pattern is broad.
	RulePattern RepoRuleKind = "pattern"
)

// RepoRule is one repository sync rule supplied to onboarding. It is the
// validated, in-memory form of a deployed repo-sync selector.
type RepoRule struct {
	// Kind is the selector kind: an exact identifier or a regex pattern.
	Kind RepoRuleKind `json:"kind"`
	// Value is the exact identifier or the regex pattern source.
	Value string `json:"value"`
}

// String renders the rule as a stable "kind:value" token for artifacts and
// errors so an operator can read back exactly what was validated.
func (r RepoRule) String() string {
	return string(r.Kind) + ":" + r.Value
}

// RuleVerdict is the narrow-versus-broad classification of a rule set. Broad
// rule sets risk accidental whole-org ingestion and are rejected unless the
// caller explicitly confirms them.
type RuleVerdict struct {
	// Broad reports whether the rule set would ingest an entire org or otherwise
	// matches an unbounded repository set.
	Broad bool `json:"broad"`
	// Reason explains, in operator language, why the set was classified broad.
	// It is empty for a narrow verdict.
	Reason string `json:"reason,omitempty"`
}

// broadPatternSelectors are regex/glob sources that match an entire org (or
// every repository) once anchors and the org prefix are stripped. They are the
// canonical accidental-org-ingestion shapes the validator must reject.
var broadPatternSelectors = map[string]struct{}{
	"*":   {},
	".*":  {},
	".+":  {},
	"/*":  {},
	"/.*": {},
}

// ClassifyRepoRules classifies a repository rule set as narrow or broad. An empty
// rule set is broad because, with the deployed githubOrg source mode, no rules
// means "ingest the whole org". A set is broad if any pattern resolves to a
// whole-org or match-everything selector; otherwise it is narrow. Mixing one
// explicit repository with a broad glob is still broad, because the glob widens
// the effective scope to the entire org.
func ClassifyRepoRules(rules []RepoRule) RuleVerdict {
	if len(rules) == 0 {
		return RuleVerdict{
			Broad:  true,
			Reason: "no repository rules supplied; the deployed githubOrg source mode would ingest the entire org",
		}
	}
	for _, rule := range rules {
		if rule.Kind != RulePattern {
			continue
		}
		if reason, broad := broadPatternReason(rule.Value); broad {
			return RuleVerdict{Broad: true, Reason: reason}
		}
	}
	return RuleVerdict{}
}

// broadPatternReason reports whether a pattern selector is a whole-org or
// match-everything selector and, if so, why. A pattern is broad when, after
// trimming regex anchors and a single "<org>/" prefix, only a whole-org
// wildcard remains. An anchored scoped prefix such as "^acme/payments-" keeps a
// concrete literal segment and is therefore narrow.
func broadPatternReason(raw string) (string, bool) {
	pattern := strings.TrimSpace(raw)
	if pattern == "" {
		return "an empty pattern matches every repository", true
	}
	stripped := strings.TrimSuffix(strings.TrimPrefix(pattern, "^"), "$")
	if _, ok := broadPatternSelectors[stripped]; ok {
		return fmt.Sprintf("pattern %q matches an entire org or every repository", raw), true
	}
	if org, rest, ok := splitOrgPrefix(stripped); ok {
		if _, broad := broadPatternSelectors[rest]; broad || rest == "" {
			return fmt.Sprintf("pattern %q matches every repository under org %q", raw, org), true
		}
	}
	return "", false
}

// splitOrgPrefix splits a stripped pattern into an "<org>" prefix and the
// remaining selector when the org segment is a concrete literal (no regex
// metacharacters). It returns ok=false when there is no single org-qualified
// segment, so callers only treat "<org>/<rest>" patterns as org-scoped.
func splitOrgPrefix(stripped string) (string, string, bool) {
	idx := strings.Index(stripped, "/")
	if idx <= 0 || idx == len(stripped)-1 {
		// No slash, leading slash, or trailing slash handled by the caller's
		// whole-org table; not an "<org>/<rest>" shape here.
		if idx == len(stripped)-1 && idx > 0 {
			return stripped[:idx], "", true
		}
		return "", "", false
	}
	org := stripped[:idx]
	if strings.ContainsAny(org, "*.+?()[]{}|\\^$") {
		return "", "", false
	}
	return org, stripped[idx+1:], true
}

// ParseRepoRules parses repository rule flag values into validated rules.
// Each value is "repo:<id>", "pattern:<regex>", or a bare identifier (treated as
// an exact repo). Regex patterns are compiled to reject a malformed rule before
// it can reach classification or the deployed config.
func ParseRepoRules(values []string) ([]RepoRule, error) {
	rules := make([]RepoRule, 0, len(values))
	for _, raw := range values {
		rule, err := parseRepoRule(raw)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// parseRepoRule parses a single repository rule flag value.
func parseRepoRule(raw string) (RepoRule, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return RepoRule{}, fmt.Errorf("empty repository rule")
	}
	kind, value, hasPrefix := strings.Cut(trimmed, ":")
	if !hasPrefix {
		return RepoRule{Kind: RuleExact, Value: trimmed}, nil
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	value = strings.TrimSpace(value)
	switch kind {
	case "repo", "exact":
		if value == "" {
			return RepoRule{}, fmt.Errorf("repository rule %q has an empty identifier", raw)
		}
		return RepoRule{Kind: RuleExact, Value: value}, nil
	case "pattern", "regex":
		if value == "" {
			return RepoRule{}, fmt.Errorf("repository rule %q has an empty pattern", raw)
		}
		if _, err := regexp.Compile(value); err != nil {
			return RepoRule{}, fmt.Errorf("repository rule %q has an invalid pattern: %w", raw, err)
		}
		return RepoRule{Kind: RulePattern, Value: value}, nil
	default:
		return RepoRule{}, fmt.Errorf("repository rule %q has an unknown kind %q; use the repo or pattern prefix", raw, kind)
	}
}
