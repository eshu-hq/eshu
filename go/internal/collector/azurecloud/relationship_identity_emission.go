// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"sort"
	"strings"
)

// relationshipFromManagedBy derives a provenance-only `managed_by` relationship
// from a resource row's ARM `managedBy` field — the ARM id of the owning
// resource. It returns false when the resource declares no owner. The
// relationship resolves no endpoints; the reducer materializes the edge only
// when both endpoints resolve in scope.
func relationshipFromManagedBy(boundary Boundary, row ResourceRow) (RelationshipObservation, bool) {
	managedBy := strings.TrimSpace(row.ManagedBy)
	if managedBy == "" {
		return RelationshipObservation{}, false
	}
	return RelationshipObservation{
		Boundary:            boundary,
		SourceARMResourceID: row.ID,
		RelationshipType:    "managed_by",
		TargetARMResourceID: managedBy,
		SupportState:        RelationshipSupportSupported,
		ProviderTime:        row.ProviderTime(),
	}, true
}

// identityObservationsFromRow derives the managed-identity observations from a
// resource row's ARM `identity` block: the system-assigned identity (when the
// block is system-assigned and carries a principal id) plus one per
// user-assigned identity under `userAssignedIdentities`. A resource with no
// identity, or whose declared identities carry no principal/client id yet,
// produces no observation. Observations are deterministically ordered so
// re-emission of a generation is idempotent.
func identityObservationsFromRow(boundary Boundary, row ResourceRow) []IdentityObservation {
	if len(row.Identity) == 0 {
		return nil
	}
	tenant := strings.TrimSpace(identityString(row.Identity, "tenantId"))
	var out []IdentityObservation
	if strings.Contains(strings.ToLower(identityString(row.Identity, "type")), "systemassigned") {
		if principal := strings.TrimSpace(identityString(row.Identity, "principalId")); principal != "" {
			out = append(out, IdentityObservation{
				Boundary:      boundary,
				ARMResourceID: row.ID,
				IdentityType:  IdentityTypeSystemAssigned,
				PrincipalID:   principal,
				TenantID:      tenant,
			})
		}
	}
	return append(out, userAssignedIdentityObservations(boundary, row, tenant)...)
}

// userAssignedIdentityObservations derives one identity observation per
// user-assigned managed identity under the ARM `userAssignedIdentities` map,
// keyed by the user-assigned identity's own ARM id. Entries with no principal or
// client id are skipped; the result is sorted by the identity's ARM id so
// emission order is deterministic.
func userAssignedIdentityObservations(boundary Boundary, row ResourceRow, tenant string) []IdentityObservation {
	raw, ok := row.Identity["userAssignedIdentities"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	armIDs := make([]string, 0, len(raw))
	for armID := range raw {
		armIDs = append(armIDs, armID)
	}
	sort.Strings(armIDs)
	var out []IdentityObservation
	for _, armID := range armIDs {
		entry, ok := raw[armID].(map[string]any)
		if !ok {
			continue
		}
		principal := strings.TrimSpace(identityString(entry, "principalId"))
		client := strings.TrimSpace(identityString(entry, "clientId"))
		if principal == "" && client == "" {
			continue
		}
		out = append(out, IdentityObservation{
			Boundary:      boundary,
			ARMResourceID: row.ID,
			IdentityType:  IdentityTypeUserAssigned,
			PrincipalID:   principal,
			ClientID:      client,
			TenantID:      tenant,
		})
	}
	return out
}

// identityString reads a string field from a raw ARM identity block, returning
// the empty string for an absent or non-string value.
func identityString(identity map[string]any, key string) string {
	if value, ok := identity[key].(string); ok {
		return value
	}
	return ""
}
