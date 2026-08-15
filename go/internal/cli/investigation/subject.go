// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// ParseSubjectFlags turns repeated `key=value` --subject entries into a scope
// map. Both halves are trimmed, and only the first `=` splits, so a value may
// contain further `=` characters. A repeated key keeps the last value.
//
// An entry with no separator, an empty key, or an empty value is an error: a
// silently dropped scope key would produce an artifact answering a different
// question than the operator asked.
func ParseSubjectFlags(raw []string) (map[string]string, error) {
	subject := map[string]string{}
	for _, entry := range raw {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("invalid --subject %q: expected key=value", entry)
		}
		subject[key] = value
	}
	return subject, nil
}

// SubjectOrPlaceholder guarantees a non-empty scope so a refusal packet still
// names what the operator asked for and passes the contract's scope gate.
//
// Whatever it returns is copied verbatim into the packet's identity.subject and
// rendered in every format. That is deliberate -- a refusal has to say what it
// refused about -- but it means a --subject value is not a safe place for a
// credential.
func SubjectOrPlaceholder(subject map[string]string) map[string]string {
	if len(subject) > 0 {
		return subject
	}
	return map[string]string{"requested": "unspecified"}
}

// ParseFamily normalizes the raw --family flag. An unrecognized value is
// returned as-is so BuildPacket can echo it back in an unknown_family refusal.
func ParseFamily(raw string) query.InvestigationFamily {
	return query.InvestigationFamily(strings.TrimSpace(raw))
}

// BoundsFromMaxSourceFacts turns a --max-source-facts override into a bounds
// value, returning nil for zero or negative so the contract defaults apply.
func BoundsFromMaxSourceFacts(maxSourceFacts int) *query.PacketBounds {
	if maxSourceFacts <= 0 {
		return nil
	}
	return &query.PacketBounds{MaxSourceFacts: maxSourceFacts}
}

// SupplyChainFilterFromSubject maps the scope keys this family understands onto
// the explain filter. Keys it does not understand stay packet context.
func SupplyChainFilterFromSubject(subject map[string]string) query.SupplyChainImpactExplanationFilter {
	return query.SupplyChainImpactExplanationFilter{
		FindingID:     subject["finding_id"],
		AdvisoryID:    subject["advisory_id"],
		CVEID:         subject["cve_id"],
		PackageID:     subject["package_id"],
		RepositoryID:  subject["repository_id"],
		SubjectDigest: subject["subject_digest"],
	}
}

// SupplyChainFilterHasScope reports whether the filter names something the
// explain route can answer: a finding id on its own, or an advisory (or CVE)
// paired with a target (package, repository, or subject digest). An advisory
// with no target would match every affected repository, which is not an
// investigation scope.
func SupplyChainFilterHasScope(filter query.SupplyChainImpactExplanationFilter) bool {
	if strings.TrimSpace(filter.FindingID) != "" {
		return true
	}
	hasAdvisory := strings.TrimSpace(filter.AdvisoryID) != "" || strings.TrimSpace(filter.CVEID) != ""
	hasTarget := strings.TrimSpace(filter.PackageID) != "" ||
		strings.TrimSpace(filter.RepositoryID) != "" ||
		strings.TrimSpace(filter.SubjectDigest) != ""
	return hasAdvisory && hasTarget
}

// DeployableUnitParams builds the admission-decisions query from the subject. It
// requires scope_id and generation_id because the admission-decision store keys
// deployable-unit correlation rows by both values. Reducer decisions for this
// domain are persisted under deployable_unit_correlation and use repository
// anchors, so workload/service subjects stay packet context instead of becoming
// unsupported exact anchor filters.
func DeployableUnitParams(subject map[string]string) (url.Values, bool) {
	scopeID := strings.TrimSpace(subject["scope_id"])
	generationID := strings.TrimSpace(subject["generation_id"])
	if scopeID == "" || generationID == "" {
		return nil, false
	}
	params := url.Values{}
	params.Set("domain", "deployable_unit_correlation")
	params.Set("scope_id", scopeID)
	params.Set("generation_id", generationID)
	if repositoryID := firstSubjectValue(subject, "repository_id", "repo_id"); repositoryID != "" {
		params.Set("anchor_kind", "repository")
		params.Set("anchor_id", repositoryID)
	}
	return params, true
}

// DriftRequestBody builds the runtime-drift request from the subject. It requires
// a scope_id (or a provider account/project/subscription alias).
func DriftRequestBody(subject map[string]string) (map[string]any, bool) {
	scopeID := firstSubjectValue(subject, "scope_id", "account_id", "project_id", "subscription_id")
	if scopeID == "" {
		return nil, false
	}
	body := map[string]any{"scope_id": scopeID}
	if provider := strings.TrimSpace(subject["provider"]); provider != "" {
		body["provider"] = provider
	}
	if uid := strings.TrimSpace(subject["cloud_resource_uid"]); uid != "" {
		body["cloud_resource_uid"] = uid
	}
	return body, true
}

// firstSubjectValue returns the first non-empty value among keys, in order, so
// an explicit key wins over its aliases.
func firstSubjectValue(subject map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(subject[key]); v != "" {
			return v
		}
	}
	return ""
}
