// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
)

// extractProviderSecurityAlertsWithQuarantine decodes every
// security_alert.repository_alert envelope in envelopes into a
// providerSecurityAlert through the typed contracts seam
// (decodeSecurityAlertRepositoryAlert), returning the decoded alerts alongside
// the []quarantinedFact for any alert whose payload was missing its required
// repository_id identity anchor.
//
// It is the single decode site for the security_alert.repository_alert kind on
// the reducer side. BOTH consumers route through it:
// BuildSecurityAlertReconciliations (the reconciliation read surface) and
// appendSecurityAlertImpactFindings (the supply-chain-impact seeder). A fact
// missing repository_id is skipped and returned as a quarantinedFact so the
// caller records a per-fact input_invalid dead-letter, while every valid
// sibling alert still decodes — the same per-fact isolation the AWS/GCP/sbom/
// vulnerability families use. A non-input_invalid error (unsupported schema
// major, undecodable shape) is returned fatally so the whole intent fails for
// durable triage rather than being silently skipped.
//
// The conversion preserves byte-identical output for valid facts: the typed
// struct mirrors the pre-typing payload shape exactly, and this function
// applies the same trim / drop-empty / scalar-plus-slice-merge normalization
// the pre-typing raw-map reads applied, so both the reconciliation decision and
// the supply-chain-impact finding seeded from a security alert are unchanged
// for every fact that decodes.
func extractProviderSecurityAlertsWithQuarantine(
	envelopes []facts.Envelope,
) ([]providerSecurityAlert, []quarantinedFact, error) {
	alerts := make([]providerSecurityAlert, 0)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.SecurityAlertRepositoryAlertFactKind {
			continue
		}
		decoded, err := decodeSecurityAlertRepositoryAlert(envelope)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		alerts = append(alerts, providerSecurityAlertFromDecoded(envelope, decoded))
	}
	return alerts, quarantined, nil
}

// extractProviderSecurityAlerts is the pure, error-free counterpart the
// pre-scan / evidence-scoping call sites use (securityAlertManifestDependencyFilter,
// supplyChainImpactUsesSecurityAlertScope, scopeSupplyChainImpactEvidenceToSecurityAlerts).
// It decodes through the same typed seam as
// extractProviderSecurityAlertsWithQuarantine but drops the quarantine slice and
// any fatal error: a malformed fact is simply excluded from the returned alerts,
// matching the pre-typing behavior of these read-only pre-filters, which never
// formed durable truth. The durable decode site (the reducer Handle path) uses
// the WithQuarantine variant so a malformed fact surfaces as a visible
// input_invalid dead-letter there — these pre-filters intentionally do not
// re-report it, avoiding a double dead-letter for the same fact.
func extractProviderSecurityAlerts(envelopes []facts.Envelope) []providerSecurityAlert {
	alerts, _, _ := extractProviderSecurityAlertsWithQuarantine(envelopes)
	return alerts
}

// providerSecurityAlertFromDecoded builds a providerSecurityAlert from one
// decoded typed payload plus its envelope identity fields, reproducing exactly
// the field mapping and normalization the pre-typing raw-map extraction applied
// so the reconciliation and supply-chain-impact outputs stay byte-identical.
func providerSecurityAlertFromDecoded(
	envelope facts.Envelope,
	decoded securityalertv1.RepositoryAlert,
) providerSecurityAlert {
	providerRepositoryID := securityAlertTrimString(decoded.RepositoryID)
	updatedAt := securityAlertDerefTrim(decoded.UpdatedAt)
	return providerSecurityAlert{
		SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
			ProviderAlertFactID:       envelope.FactID,
			ProviderAlertScopeID:      envelope.ScopeID,
			ProviderAlertGenerationID: envelope.GenerationID,
			Provider:                  securityAlertDerefTrim(decoded.Provider),
			ProviderAlertID:           securityAlertDerefTrim(decoded.ProviderAlertID),
			ProviderAlertNumber:       securityAlertDerefInt64(decoded.ProviderAlertNumber),
			ProviderState:             strings.ToLower(securityAlertDerefTrim(decoded.ProviderState)),
			RepositoryID:              providerRepositoryID,
			ProviderRepositoryID:      providerRepositoryID,
			RepositoryName: firstNonBlank(
				securityAlertDerefTrim(decoded.RepositoryName),
				securityAlertRepositoryNameFromID(providerRepositoryID),
			),
			PackageID:       securityAlertDerefTrim(decoded.PackageID),
			Ecosystem:       securityAlertDerefTrim(decoded.Ecosystem),
			PackageName:     securityAlertDerefTrim(decoded.PackageName),
			ManifestPath:    securityAlertDerefTrim(decoded.ManifestPath),
			DependencyScope: securityAlertDerefTrim(decoded.DependencyScope),
			Relationship:    securityAlertDerefTrim(decoded.Relationship),
			GHSAIDs:         securityAlertMergeIDs(decoded.GHSAID, decoded.GHSAIDs),
			CVEIDs:          securityAlertMergeIDs(decoded.CVEID, decoded.CVEIDs),
			VulnerableRange: securityAlertDerefTrim(decoded.VulnerableRange),
			PatchedVersion:  securityAlertDerefTrim(decoded.PatchedVersion),
			Severity:        securityAlertDerefTrim(decoded.Severity),
			CVSS:            normalizeSecurityAlertAnyMap(decoded.CVSS),
			EPSS:            normalizeSecurityAlertStringMap(decoded.EPSS),
			CWEs:            normalizeSecurityAlertStringMapSlice(decoded.CWEs),
			Summary:         securityAlertDerefTrim(decoded.Summary),
			SourceURL:       securityAlertDerefTrim(decoded.SourceURL),
			CreatedAt:       securityAlertDerefTrim(decoded.CreatedAt),
			UpdatedAt:       updatedAt,
			FixedAt:         securityAlertDerefTrim(decoded.FixedAt),
			DismissedAt:     securityAlertDerefTrim(decoded.DismissedAt),
			SourceFreshness: securityAlertSourceFreshnessFromDecoded(decoded),
			CollectionCoverageState: securityAlertDerefTrim(
				decoded.CollectionCoverageState,
			),
			CollectionTruncated:         securityAlertDerefBool(decoded.CollectionTruncated),
			CollectionPagesFetched:      securityAlertDerefInt64(decoded.CollectionPagesFetched),
			CollectionStateFilter:       securityAlertDerefTrim(decoded.CollectionStateFilter),
			CollectionIncompleteReasons: securityAlertCleanStrings(decoded.CollectionIncompleteReasons),
			CanonicalWrites:             0,
			EvidenceFactIDs:             compactStringSlice(envelope.FactID),
		},
		updatedAtTime: parseSecurityAlertTime(updatedAt),
	}
}

// securityAlertSourceFreshnessFromDecoded reproduces securityAlertSourceFreshness
// against the decoded struct: an explicit source_freshness wins; otherwise an
// "incomplete" coverage state maps to "partial"; otherwise "active".
func securityAlertSourceFreshnessFromDecoded(decoded securityalertv1.RepositoryAlert) string {
	if freshness := securityAlertDerefTrim(decoded.SourceFreshness); freshness != "" {
		return freshness
	}
	if securityAlertDerefTrim(decoded.CollectionCoverageState) == "incomplete" {
		return "partial"
	}
	return "active"
}

// securityAlertTrimString trims a value the same way payloadStr did (via
// strings.TrimSpace on the raw scalar), so a present-but-padded required
// repository_id normalizes to the same string the pre-typing read produced.
func securityAlertTrimString(value string) string {
	return strings.TrimSpace(value)
}

// securityAlertDerefTrim dereferences an optional string field to its trimmed
// value, or "" when the field is absent (nil pointer) — matching payloadStr's
// absent-key-returns-empty and trim behavior.
func securityAlertDerefTrim(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// securityAlertDerefInt64 dereferences an optional int64 field to its value, or
// 0 when absent — matching securityAlertInt64's absent-returns-zero behavior.
func securityAlertDerefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// securityAlertDerefBool dereferences an optional bool field to its value, or
// false when absent — matching payloadBool's absent-returns-false behavior.
func securityAlertDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// securityAlertMergeIDs merges a scalar advisory id with its slice form into one
// trimmed, de-duplicated, sorted set, reproducing exactly the pre-typing
// payloadStrings(scalarKey, sliceKey) read: the scalar is appended first (when
// non-empty), then every non-empty slice entry, then uniqueSortedStrings.
func securityAlertMergeIDs(scalar *string, slice []string) []string {
	var values []string
	if trimmed := securityAlertDerefTrim(scalar); trimmed != "" {
		values = append(values, trimmed)
	}
	for _, value := range slice {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return uniqueSortedStrings(values)
}

// securityAlertCleanStrings trims and drops empty entries from a decoded string
// slice, matching payloadStrings("", sliceKey)'s handling of
// collection_incomplete_reasons (a slice-only read whose scalar key is empty):
// each non-empty trimmed entry, then uniqueSortedStrings.
func securityAlertCleanStrings(slice []string) []string {
	var values []string
	for _, value := range slice {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return uniqueSortedStrings(values)
}

// normalizeSecurityAlertAnyMap reproduces securityAlertMap against a decoded
// map[string]any: nil/empty in yields nil, and empty-key or nil-value entries
// are dropped; nil out when everything is dropped.
func normalizeSecurityAlertAnyMap(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizeSecurityAlertStringMap reproduces securityAlertStringMap against a
// decoded map[string]string: trim key and value, drop empty, nil out when
// everything is dropped.
//
// The decoded map is freshly allocated and solely owned by this decode
// (decode_map.go's anyToStringMap fast path returns a new map on the JSONB path,
// and the payload no longer references an already-typed one after decode), so
// the normalization mutates it in place rather than allocating a second map —
// one allocation instead of two on the per-scope-generation reconciliation path,
// for the same trimmed / empty-dropped result cloneSecurityAlertStringMap
// produced.
func normalizeSecurityAlertStringMap(raw map[string]string) map[string]string {
	return normalizeSecurityAlertStringMapInPlace(raw)
}

// normalizeSecurityAlertStringMapInPlace trims every key and value of a freshly
// decoded, solely-owned map and drops empty entries in place, returning nil when
// nothing survives. A key that changes under trimming is re-inserted AFTER the
// range completes (Go permits deletion during a range but not insertion), so the
// mutation is iteration-safe for any producer, including the rare padded-key
// payload the in-tree collectors never emit. It must never be called on a shared
// or aliased map.
func normalizeSecurityAlertStringMapInPlace(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	var rekeyed map[string]string
	for key, value := range m {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			delete(m, key)
			continue
		}
		if trimmedKey != key {
			delete(m, key)
			if rekeyed == nil {
				rekeyed = make(map[string]string)
			}
			rekeyed[trimmedKey] = trimmedValue
			continue
		}
		if trimmedValue != value {
			m[key] = trimmedValue
		}
	}
	for key, value := range rekeyed {
		m[key] = value
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// normalizeSecurityAlertStringMapSlice reproduces securityAlertStringMapSlice
// against a decoded []map[string]string: trim each entry in place, drop empty
// maps, nil out when everything is dropped. Like normalizeSecurityAlertStringMap
// it mutates the solely-owned decode result rather than cloning each element.
func normalizeSecurityAlertStringMapSlice(raw []map[string]string) []map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(raw))
	for _, item := range raw {
		if normalized := normalizeSecurityAlertStringMapInPlace(item); len(normalized) > 0 {
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
