// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticpolicy

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/status"
)

const (
	// EnvPolicyJSON names the optional semantic extraction policy registry.
	EnvPolicyJSON = "ESHU_SEMANTIC_EXTRACTION_POLICY_JSON"
)

const (
	// ScopeOrganization applies policy to an organization boundary.
	ScopeOrganization = "organization"
	// ScopeTenant applies policy to a tenant boundary.
	ScopeTenant = "tenant"
	// ScopeProject applies policy to a project boundary.
	ScopeProject = "project"
	// ScopeRepository applies policy to a repository boundary.
	ScopeRepository = "repository"
)

const (
	// SourceSelectorPathPrefix allowlists sources by normalized path prefix.
	SourceSelectorPathPrefix = "path_prefix"
	// SourceSelectorSourceID allowlists sources by stable source id.
	SourceSelectorSourceID = "source_id"
	// SourceSelectorDocumentID allowlists sources by document id.
	SourceSelectorDocumentID = "document_id"
	// SourceSelectorSourceURIHash allowlists sources by redacted source URI hash.
	SourceSelectorSourceURIHash = "source_uri_hash"
	// SourceSelectorAll allowlists every source in a matching scope.
	SourceSelectorAll = "all"
)

const (
	// ACLAllowed means source ACLs permit semantic content egress.
	ACLAllowed = "allowed"
	// ACLDenied means source ACLs deny semantic content egress.
	ACLDenied = "denied"
	// ACLPartial means the ACL check was incomplete and must fail closed.
	ACLPartial = "partial"
	// ACLMissing means no ACL decision was available.
	ACLMissing = "missing"
	// ACLStale means the ACL decision is not current for the source revision.
	ACLStale = "stale"
)

const (
	// RedactionStrict requires deterministic redaction before provider use.
	RedactionStrict = "strict"
	// RedactionStandard allows the standard semantic redaction policy.
	RedactionStandard = "standard"
)

const (
	// RetentionMetadataOnly keeps prompt and response metadata without raw bodies.
	RetentionMetadataOnly = "metadata_only"
	// RetentionNone records that raw content must not be retained.
	RetentionNone = "none"
	// RetentionHashOnly records only hashes or fingerprints.
	RetentionHashOnly = "hash_only"
	// RetentionBoundedExcerpt allows bounded redacted excerpts.
	RetentionBoundedExcerpt = "bounded_excerpt"
)

const (
	// ReasonAllowed marks an allowed semantic extraction decision.
	ReasonAllowed = "allowed"
	// ReasonPolicyDisabled marks a missing or disabled policy.
	ReasonPolicyDisabled = "policy_disabled"
	// ReasonInvalidPolicy marks policy JSON or in-memory policy validation errors.
	ReasonInvalidPolicy = "invalid_policy"
	// ReasonUnsupportedSourceClass marks an unknown source class.
	ReasonUnsupportedSourceClass = "unsupported_source_class"
	// ReasonSourceClassDenied marks a class blocked by policy.
	ReasonSourceClassDenied = "source_class_denied"
	// ReasonProviderProfileNotAllowed marks a missing or mismatched profile.
	ReasonProviderProfileNotAllowed = "provider_profile_not_allowed"
	// ReasonACLNotAllowed marks denied, missing, partial, or stale ACL state.
	ReasonACLNotAllowed = "acl_not_allowed"
	// ReasonScopeDisabled marks a source class or scope outside the allowlist.
	ReasonScopeDisabled = status.SemanticExtractionReasonScopeDisabled
	// ReasonSourceNotAllowlisted marks a source outside an otherwise matching rule.
	ReasonSourceNotAllowlisted = "source_not_allowlisted"
)

// Policy captures the semantic extraction allowlist for hosted provider use.
type Policy struct {
	PolicyID            string       `json:"policy_id"`
	Enabled             bool         `json:"enabled"`
	Defaults            Settings     `json:"defaults,omitempty"`
	Egress              EgressPolicy `json:"egress,omitempty"`
	Rules               []Rule       `json:"rules,omitempty"`
	DeniedSourceClasses []string     `json:"denied_source_classes,omitempty"`
}

// Rule allows one provider profile to handle selected source classes and scopes.
type Rule struct {
	RuleID            string           `json:"rule_id"`
	ProviderProfileID string           `json:"provider_profile_id"`
	SourceClasses     []string         `json:"source_classes"`
	Scopes            []Scope          `json:"scopes"`
	SourceAllowlist   []SourceSelector `json:"source_allowlist"`
	Settings          Settings         `json:"settings"`
}

// Scope names an organization, tenant, project, or repository boundary.
type Scope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// SourceSelector names a bounded source allowlist entry inside a matching scope.
type SourceSelector struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Settings carries limits, redaction, and retention policy for an allowed rule.
type Settings struct {
	Limits    Limits    `json:"limits"`
	Redaction Redaction `json:"redaction"`
	Retention Retention `json:"retention"`
}

// Limits bounds prompt chunking and daily provider budget for a policy rule.
type Limits struct {
	MaxChunkBytes      int64 `json:"max_chunk_bytes"`
	MaxTokensPerChunk  int64 `json:"max_tokens_per_chunk"`
	MaxDailyTokens     int64 `json:"max_daily_tokens,omitempty"`
	MaxDailyCostMicros int64 `json:"max_daily_cost_micros,omitempty"`
}

// Redaction names the deterministic redaction posture required before egress.
type Redaction struct {
	Mode      string `json:"mode"`
	PolicyRef string `json:"policy_ref,omitempty"`
}

// Retention names what prompt and provider response material may be retained.
type Retention struct {
	Posture  string `json:"posture"`
	Prompt   string `json:"prompt"`
	Response string `json:"response"`
}

// Request is the source-specific decision input for semantic extraction.
type Request struct {
	ProviderProfileID string
	SourceClass       string
	Scope             Scope
	SourceID          string
	DocumentID        string
	SourceURIHash     string
	SourcePath        string
	ACLState          string
}

// Decision records whether semantic extraction may run for one source.
type Decision struct {
	Allowed           bool
	State             string
	Reason            string
	Detail            string
	PolicyID          string
	RuleID            string
	ProviderProfileID string
	SourceClass       string
	Settings          Settings
}

// LoadFromEnv reads policy JSON from getenv and validates it without I/O.
func LoadFromEnv(getenv func(string) string) (Policy, error) {
	if getenv == nil {
		return Policy{}, nil
	}
	raw := strings.TrimSpace(getenv(EnvPolicyJSON))
	if raw == "" {
		return Policy{}, nil
	}
	return ParsePolicyJSON(raw)
}

// ParsePolicyJSON parses semantic extraction policy JSON.
func ParsePolicyJSON(raw string) (Policy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Policy{}, nil
	}
	var policy Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", EnvPolicyJSON, err)
	}
	normalized, err := Normalize(policy)
	if err != nil {
		return Policy{}, fmt.Errorf("validate %s: %w", EnvPolicyJSON, err)
	}
	return normalized, nil
}

// Normalize validates and canonicalizes an in-memory policy.
func Normalize(policy Policy) (Policy, error) {
	out := Policy{
		PolicyID: strings.TrimSpace(policy.PolicyID),
		Enabled:  policy.Enabled,
		Defaults: normalizeSettings(policy.Defaults),
	}
	if out.Enabled && out.PolicyID == "" {
		return Policy{}, fmt.Errorf("policy_id is required when policy is enabled")
	}
	if out.Enabled && len(policy.Rules) == 0 {
		return Policy{}, fmt.Errorf("rules must include at least one rule when policy is enabled")
	}
	denied, err := normalizeSourceClasses(policy.DeniedSourceClasses)
	if err != nil {
		return Policy{}, fmt.Errorf("denied_source_classes: %w", err)
	}
	out.DeniedSourceClasses = denied
	egress, err := normalizeEgress(policy.Egress)
	if err != nil {
		return Policy{}, err
	}
	sortEgressProviderRules(egress.SemanticProviders)
	out.Egress = egress

	seenRules := make(map[string]struct{}, len(policy.Rules))
	for i, rule := range policy.Rules {
		normalized, err := normalizeRule(rule, out.Defaults, denied)
		if err != nil {
			return Policy{}, fmt.Errorf("rules[%d]: %w", i, err)
		}
		if _, ok := seenRules[normalized.RuleID]; ok {
			return Policy{}, fmt.Errorf("rules[%d].rule_id %q is duplicated", i, normalized.RuleID)
		}
		seenRules[normalized.RuleID] = struct{}{}
		out.Rules = append(out.Rules, normalized)
	}
	return out, nil
}

// Evaluate applies policy, source allowlists, ACL state, and profile status.
func Evaluate(
	policy Policy,
	request Request,
	profiles []status.SemanticProviderProfileStatus,
) Decision {
	policy, err := Normalize(policy)
	if err != nil {
		return deny(status.SemanticExtractionDisabledByPolicy, ReasonInvalidPolicy, err.Error(), request)
	}
	request = normalizeRequest(request)
	if !policy.Enabled {
		return deny(status.SemanticExtractionDisabledByPolicy, ReasonPolicyDisabled, "semantic extraction policy is disabled", request)
	}
	if !isSupportedSourceClass(request.SourceClass) {
		return deny(status.SemanticExtractionDisabledByPolicy, ReasonUnsupportedSourceClass, "source class is unsupported", request)
	}
	if slices.Contains(policy.DeniedSourceClasses, request.SourceClass) {
		return deny(status.SemanticExtractionAvailableButDisabledForScope, ReasonSourceClassDenied, "source class is denied by policy", request)
	}
	if request.ACLState != ACLAllowed {
		return deny(status.SemanticExtractionDisabledByPolicy, ReasonACLNotAllowed, "source ACL does not allow egress", request)
	}
	if !profileAllowsRequest(profiles, request) {
		return deny(status.SemanticExtractionDisabledByPolicy, ReasonProviderProfileNotAllowed, "provider profile is not configured for this source class", request)
	}

	var scopeMatched bool
	for _, rule := range policy.Rules {
		if !ruleMatchesProfileAndClass(rule, request) {
			continue
		}
		if !scopeMatches(rule.Scopes, request.Scope) {
			continue
		}
		scopeMatched = true
		if !sourceMatches(rule.SourceAllowlist, request) {
			continue
		}
		egressAllowed, reason, detail := egressAllowsRequest(policy.Egress, request)
		if !egressAllowed {
			return deny(status.SemanticExtractionDisabledByPolicy, reason, detail, request)
		}
		return Decision{
			Allowed:           true,
			State:             status.SemanticExtractionAvailable,
			Reason:            ReasonAllowed,
			PolicyID:          policy.PolicyID,
			RuleID:            rule.RuleID,
			ProviderProfileID: request.ProviderProfileID,
			SourceClass:       request.SourceClass,
			Settings:          rule.Settings,
		}
	}
	if scopeMatched {
		return deny(status.SemanticExtractionAvailableButDisabledForScope, ReasonSourceNotAllowlisted, "source is outside the allowlist", request)
	}
	return deny(status.SemanticExtractionAvailableButDisabledForScope, ReasonScopeDisabled, "scope is outside the allowlist", request)
}

// ApplyToProviderStatuses projects policy allowlists into redacted status rows.
func ApplyToProviderStatuses(
	profiles []status.SemanticProviderProfileStatus,
	policy Policy,
) []status.SemanticProviderProfileStatus {
	if len(profiles) == 0 {
		return nil
	}
	out := make([]status.SemanticProviderProfileStatus, 0, len(profiles))
	normalizedPolicy, err := Normalize(policy)
	policyUsable := err == nil && normalizedPolicy.Enabled
	denied := map[string]struct{}{}
	for _, sourceClass := range normalizedPolicy.DeniedSourceClasses {
		denied[sourceClass] = struct{}{}
	}
	for _, profile := range profiles {
		row := profile
		row.SourcePolicyConfigured = false
		row.SourceClasses = nil
		if policyUsable {
			sourceClasses := allowedProfileSourceClasses(profile, normalizedPolicy, denied)
			if len(sourceClasses) > 0 {
				row.SourcePolicyConfigured = true
				row.SourceClasses = sourceClasses
			}
		}
		out = append(out, row)
	}
	return out
}
