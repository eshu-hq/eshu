// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import "time"

type semanticProviderProfileJSON struct {
	ProfileID              string   `json:"profile_id"`
	DisplayName            string   `json:"display_name,omitempty"`
	ProviderKind           string   `json:"provider_kind"`
	CredentialSourceKind   string   `json:"credential_source_kind"`
	CredentialConfigured   bool     `json:"credential_configured"`
	ModelID                string   `json:"model_id,omitempty"`
	EmbeddingDimensions    int      `json:"embedding_dimensions,omitempty"`
	EndpointProfileID      string   `json:"endpoint_profile_id,omitempty"`
	SourceClasses          []string `json:"source_classes"`
	SourcePolicyConfigured bool     `json:"source_policy_configured"`
	State                  string   `json:"state"`
	Reason                 string   `json:"reason,omitempty"`
	Detail                 string   `json:"detail,omitempty"`
	UpdatedAt              string   `json:"updated_at,omitempty"`
}

func semanticProviderProfilesJSON(
	profiles []SemanticProviderProfileStatus,
) []semanticProviderProfileJSON {
	if len(profiles) == 0 {
		return nil
	}
	rows := make([]semanticProviderProfileJSON, 0, len(profiles))
	for _, profile := range cloneSemanticProviderProfiles(profiles) {
		row := semanticProviderProfileJSON{
			ProfileID:              profile.ProfileID,
			DisplayName:            profile.DisplayName,
			ProviderKind:           profile.ProviderKind,
			CredentialSourceKind:   profile.CredentialSourceKind,
			CredentialConfigured:   profile.CredentialConfigured,
			ModelID:                profile.ModelID,
			EmbeddingDimensions:    profile.EmbeddingDimensions,
			EndpointProfileID:      profile.EndpointProfileID,
			SourceClasses:          profile.SourceClasses,
			SourcePolicyConfigured: profile.SourcePolicyConfigured,
			State:                  profile.State,
			Reason:                 profile.Reason,
			Detail:                 profile.Detail,
		}
		if !profile.UpdatedAt.IsZero() {
			row.UpdatedAt = profile.UpdatedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}
	return rows
}
