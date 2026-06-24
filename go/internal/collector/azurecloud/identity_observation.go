// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
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

	payload := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    observation.Boundary.CollectorInstanceID,
		"tenant_id":                observation.Boundary.TenantID,
		"scope_kind":               observation.Boundary.ScopeKind,
		"provider_scope_id":        observation.Boundary.ProviderScopeID,
		"source_lane":              observation.Boundary.SourceLane,
		"arm_resource_id":          armID,
		"normalized_resource_id":   identity.Normalized,
		"resource_type":            identity.ResourceType,
		"identity_type":            identityType,
		"role_class":               strings.TrimSpace(observation.RoleClass),
		"assignment_scope":         strings.TrimSpace(observation.AssignmentScope),
		"provider_time":            timeOrNil(observation.ProviderTime),
		"redaction_policy_version": RedactionPolicyVersion,
	}
	addPrincipalFingerprint(payload, "principal_fingerprint", "azure_identity_principal", principal, key)
	addPrincipalFingerprint(payload, "client_fingerprint", "azure_identity_client", client, key)
	addPrincipalFingerprint(payload, "object_fingerprint", "azure_identity_object", object, key)
	addPrincipalFingerprint(payload, "tenant_fingerprint", "azure_identity_tenant", tenant, key)

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
func addPrincipalFingerprint(payload map[string]any, field, reason, raw string, key redact.Key) {
	if raw == "" {
		return
	}
	payload[field] = redact.String(raw, reason, reason+":"+field, key).Marker
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
