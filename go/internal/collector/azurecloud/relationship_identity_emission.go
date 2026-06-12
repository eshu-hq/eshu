package azurecloud

import "strings"

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

// systemAssignedIdentityFromRow derives a system-assigned managed-identity
// observation from a resource row's ARM `identity` block. It returns false
// unless the block is system-assigned and carries a principal id, so a resource
// with no identity (or a system-assigned identity the provider has not yet
// populated a principal for) emits no identity fact. User-assigned identities,
// which live under `userAssignedIdentities`, are a separate follow-up.
func systemAssignedIdentityFromRow(boundary Boundary, row ResourceRow) (IdentityObservation, bool) {
	if len(row.Identity) == 0 {
		return IdentityObservation{}, false
	}
	if !strings.Contains(strings.ToLower(identityString(row.Identity, "type")), "systemassigned") {
		return IdentityObservation{}, false
	}
	principal := strings.TrimSpace(identityString(row.Identity, "principalId"))
	if principal == "" {
		return IdentityObservation{}, false
	}
	return IdentityObservation{
		Boundary:      boundary,
		ARMResourceID: row.ID,
		IdentityType:  IdentityTypeSystemAssigned,
		PrincipalID:   principal,
		TenantID:      strings.TrimSpace(identityString(row.Identity, "tenantId")),
	}, true
}

// identityString reads a string field from a raw ARM identity block, returning
// the empty string for an absent or non-string value.
func identityString(identity map[string]any, key string) string {
	if value, ok := identity[key].(string); ok {
		return value
	}
	return ""
}
