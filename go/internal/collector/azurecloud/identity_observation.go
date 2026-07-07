// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// Azure identity observation types classify what kind of identity or assignment
// the collector observed on a resource. They are a bounded enum so a fabricated
// type never reaches durable facts.
const (
	// IdentityTypeSystemAssigned is a system-assigned managed identity.
	IdentityTypeSystemAssigned = "system_assigned"
	// IdentityTypeUserAssigned is a user-assigned managed identity.
	IdentityTypeUserAssigned = "user_assigned"
	// IdentityTypeServicePrincipal is a service principal identity.
	IdentityTypeServicePrincipal = "service_principal"
	// IdentityTypeRoleAssignment is an RBAC role assignment observation.
	IdentityTypeRoleAssignment = "role_assignment"
)

// IdentityObservation is one provider-observed managed identity or RBAC
// assignment on an ARM resource. The collector preserves the bounded identity
// type, role class, and assignment scope as evidence and fingerprints every
// principal GUID; it is policy evidence only and creates no identity graph node.
type IdentityObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// ARMResourceID is the raw ARM identity carrying the identity/assignment.
	ARMResourceID string
	// IdentityType is the bounded identity/assignment type.
	IdentityType string
	// PrincipalID, ClientID, ObjectID, TenantID are raw GUIDs. Each is
	// fingerprinted; at least one must be present.
	PrincipalID string
	ClientID    string
	ObjectID    string
	TenantID    string
	// RoleClass is the bounded role/action class (e.g. owner, contributor).
	RoleClass string
	// AssignmentScope is the ARM scope of the assignment, preserved verbatim.
	AssignmentScope string
	// ProviderTime is the read/update time, or nil when absent.
	ProviderTime *time.Time
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewIdentityObservationEnvelope builds the durable azure_identity_observation
// fact for one identity/assignment. Every principal GUID is fingerprinted with
// the redaction key so raw GUIDs never reach durable facts; the identity type,
// role class, and assignment scope stay as bounded evidence. The stable fact key
// is derived from the resource identity, type, role class, and the raw principal
// GUIDs (hashed by facts.StableID, never exposed), so it is independent of the
// redaction key and stable across key rotation, while distinct principals key
// distinct rows.
//
// It fails closed on a missing resource id, an unknown identity type, no
// principal GUIDs at all, or a zero redaction key.
func NewIdentityObservationEnvelope(observation IdentityObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure identity observation requires a redaction key")
	}
	armID := strings.TrimSpace(observation.ARMResourceID)
	if armID == "" {
		return facts.Envelope{}, fmt.Errorf("azure identity observation requires arm_resource_id")
	}
	identityType, err := normalizeIdentityType(observation.IdentityType)
	if err != nil {
		return facts.Envelope{}, err
	}

	principal := strings.TrimSpace(observation.PrincipalID)
	client := strings.TrimSpace(observation.ClientID)
	object := strings.TrimSpace(observation.ObjectID)
	tenant := strings.TrimSpace(observation.TenantID)
	if principal == "" && client == "" && object == "" && tenant == "" {
		return facts.Envelope{}, fmt.Errorf("azure identity observation requires at least one principal id")
	}

	identity, err := ParseARMIdentity(armID)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("normalize arm identity: %w", err)
	}

	stableKey := facts.StableID(facts.AzureIdentityObservationFactKind, map[string]any{
		"normalized_id": identity.Normalized,
		"identity_type": identityType,
		"role_class":    strings.TrimSpace(observation.RoleClass),
		"principal_id":  principal,
		"client_id":     client,
		"object_id":     object,
		"tenant_id":     tenant,
		"source_lane":   observation.Boundary.SourceLane,
	})

	roleClass := strings.TrimSpace(observation.RoleClass)
	assignmentScope := strings.TrimSpace(observation.AssignmentScope)
	payload, err := factschema.EncodeAzureIdentityObservation(azurev1.IdentityObservation{
		ARMResourceID:          armID,
		NormalizedResourceID:   identity.Normalized,
		ResourceType:           identity.ResourceType,
		IdentityType:           identityType,
		RoleClass:              &roleClass,
		AssignmentScope:        &assignmentScope,
		PrincipalFingerprint:   principalFingerprintPtr("azure_identity_principal", "principal_fingerprint", principal, key),
		ClientFingerprint:      principalFingerprintPtr("azure_identity_client", "client_fingerprint", client, key),
		ObjectFingerprint:      principalFingerprintPtr("azure_identity_object", "object_fingerprint", object, key),
		TenantFingerprint:      principalFingerprintPtr("azure_identity_tenant", "tenant_fingerprint", tenant, key),
		ProviderTime:           timeStringPtr(observation.ProviderTime),
		RedactionPolicyVersion: stringPtr(RedactionPolicyVersion),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode azure_identity_observation payload: %w", err)
	}
	addAzureBoundaryPayload(payload, observation.Boundary)
	payload["tenant_id"] = observation.Boundary.TenantID
	payload["scope_kind"] = observation.Boundary.ScopeKind
	payload["provider_scope_id"] = observation.Boundary.ProviderScopeID
	payload["source_lane"] = observation.Boundary.SourceLane

	return newEnvelope(
		observation.Boundary,
		facts.AzureIdentityObservationFactKind,
		facts.AzureIdentityObservationSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized+"|"+identityType),
		observation.SourceURI,
		payload,
	), nil
}

// addPrincipalFingerprint fingerprints one principal GUID into the payload under
// key, skipping blank values so an absent principal produces no field.
func principalFingerprintPtr(reason, field, raw string, key redact.Key) *string {
	if raw == "" {
		return nil
	}
	fingerprint := redact.String(raw, reason, reason+":"+field, key).Marker
	return &fingerprint
}

// normalizeIdentityType validates the identity type against the bounded set so a
// fabricated type never reaches durable facts.
func normalizeIdentityType(identityType string) (string, error) {
	switch strings.TrimSpace(identityType) {
	case IdentityTypeSystemAssigned:
		return IdentityTypeSystemAssigned, nil
	case IdentityTypeUserAssigned:
		return IdentityTypeUserAssigned, nil
	case IdentityTypeServicePrincipal:
		return IdentityTypeServicePrincipal, nil
	case IdentityTypeRoleAssignment:
		return IdentityTypeRoleAssignment, nil
	default:
		return "", fmt.Errorf("azure identity observation has unknown identity_type %q", identityType)
	}
}
