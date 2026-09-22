// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// ProviderProfileConfigured means a profile has the metadata and
	// credential reference needed for future provider-backed extraction.
	ProviderProfileConfigured = "configured"
	// ProviderProfileUnconfigured means a profile is present but missing
	// required non-secret configuration.
	ProviderProfileUnconfigured = "unconfigured"
	// ProviderProfileHealthy means an external health source marked the
	// profile healthy. Eshu does not probe providers in the no-traffic profile
	// registry.
	ProviderProfileHealthy = "healthy"
	// ProviderProfileUnhealthy means an external health source marked the
	// profile unhealthy.
	ProviderProfileUnhealthy = "unhealthy"
)

var providerProfileStates = []string{
	ProviderProfileConfigured,
	ProviderProfileUnconfigured,
	ProviderProfileHealthy,
	ProviderProfileUnhealthy,
}

// ProviderProfileStatus is the redacted operator view of one semantic
// extraction provider profile.
type ProviderProfileStatus struct {
	ProfileID              string
	DisplayName            string
	ProviderKind           string
	CredentialSourceKind   string
	CredentialConfigured   bool
	ModelID                string
	EmbeddingDimensions    int
	EndpointProfileID      string
	SourceClasses          []string
	SourcePolicyConfigured bool
	State                  string
	Reason                 string
	Detail                 string
	UpdatedAt              time.Time
}

// ProviderProfileSupportedStates returns stable provider profile states.
func ProviderProfileSupportedStates() []string {
	return slices.Clone(providerProfileStates)
}

func CloneProviderProfiles(rows []ProviderProfileStatus) []ProviderProfileStatus {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]ProviderProfileStatus, 0, len(rows))
	for _, row := range rows {
		normalized := normalizeProviderProfile(row)
		if normalized.ProfileID == "" {
			continue
		}
		cloned = append(cloned, normalized)
	}
	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].ProfileID < cloned[j].ProfileID
	})
	return cloned
}

func normalizeProviderProfile(row ProviderProfileStatus) ProviderProfileStatus {
	state := strings.TrimSpace(row.State)
	if !isProviderProfileState(state) {
		if row.CredentialConfigured {
			state = ProviderProfileConfigured
		} else {
			state = ProviderProfileUnconfigured
		}
	}

	sourceClasses := normalizeSourceClasses(row.SourceClasses)
	out := ProviderProfileStatus{
		ProfileID:              strings.TrimSpace(row.ProfileID),
		DisplayName:            strings.TrimSpace(row.DisplayName),
		ProviderKind:           strings.TrimSpace(row.ProviderKind),
		CredentialSourceKind:   strings.TrimSpace(row.CredentialSourceKind),
		CredentialConfigured:   row.CredentialConfigured,
		ModelID:                strings.TrimSpace(row.ModelID),
		EmbeddingDimensions:    row.EmbeddingDimensions,
		EndpointProfileID:      strings.TrimSpace(row.EndpointProfileID),
		SourceClasses:          sourceClasses,
		SourcePolicyConfigured: row.SourcePolicyConfigured,
		State:                  state,
		Reason:                 strings.TrimSpace(row.Reason),
		Detail:                 strings.TrimSpace(row.Detail),
		UpdatedAt:              row.UpdatedAt,
	}
	if out.Reason == "" {
		out.Reason = defaultProviderProfileReason(out)
	}
	return out
}

func normalizeSourceClasses(sourceClasses []string) []string {
	if len(sourceClasses) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(sourceClasses))
	normalized := make([]string, 0, len(sourceClasses))
	for _, sourceClass := range sourceClasses {
		sourceClass = strings.TrimSpace(sourceClass)
		if sourceClass == "" {
			continue
		}
		if _, ok := seen[sourceClass]; ok {
			continue
		}
		seen[sourceClass] = struct{}{}
		normalized = append(normalized, sourceClass)
	}
	sort.Strings(normalized)
	return normalized
}

func isProviderProfileState(state string) bool {
	return slices.Contains(providerProfileStates, state)
}

func defaultProviderProfileReason(row ProviderProfileStatus) string {
	switch row.State {
	case ProviderProfileHealthy:
		return "provider_profile_healthy"
	case ProviderProfileUnhealthy:
		return "provider_profile_unhealthy"
	case ProviderProfileUnconfigured:
		return "credential_not_configured"
	default:
		if !row.SourcePolicyConfigured {
			return "source_policy_not_configured"
		}
		return "provider_profile_configured"
	}
}

func profileConfigured(row ProviderProfileStatus) bool {
	switch row.State {
	case ProviderProfileConfigured, ProviderProfileHealthy, ProviderProfileUnhealthy:
		return row.CredentialConfigured
	default:
		return false
	}
}

func profileUnhealthy(row ProviderProfileStatus) bool {
	return row.State == ProviderProfileUnhealthy
}

func profileAllowsSource(row ProviderProfileStatus, sourceClass string) bool {
	if row.State == ProviderProfileUnhealthy ||
		!profileConfigured(row) ||
		!row.SourcePolicyConfigured {
		return false
	}
	return slices.Contains(row.SourceClasses, sourceClass)
}
