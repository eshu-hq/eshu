// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewGCPPrincipalEnvelope builds the durable gcp_iam_principal source fact for
// one GCP service-account principal observed as an IAM binding grantee. The
// join identity is the redaction-safe member fingerprint; no raw email or member
// string is ever stored.
func NewGCPPrincipalEnvelope(observation GCPPrincipalObservation) (facts.Envelope, error) {
	if err := validateGCPContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	fingerprint := strings.TrimSpace(observation.PrincipalFingerprint)
	if fingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("gcp secrets iam principal observation requires principal_fingerprint")
	}
	memberClass := strings.TrimSpace(observation.MemberClass)
	if memberClass == "" {
		memberClass = GCPMemberClassServiceAccount
	}
	// Identity is the service-account fingerprint alone: the same service account
	// can be granted at project, folder, and organization level, so folding the
	// observation project into the key would split one identity into several
	// non-idempotent principal facts. project_id stays descriptive payload only.
	stableKey := facts.StableID(facts.GCPIAMPrincipalFactKind, map[string]any{
		"principal_fingerprint": fingerprint,
		"principal_type":        PrincipalTypeGCPServiceAccount,
	})
	payload := gcpCommonPayload(observation.Context)
	payload["principal_fingerprint"] = fingerprint
	payload["principal_type"] = PrincipalTypeGCPServiceAccount
	payload["member_class"] = memberClass
	return newEnvelope(
		gcpToEnvelopeContext(observation.Context),
		facts.GCPIAMPrincipalFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, fingerprint),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// NewGCPPermissionPolicyEnvelope builds the durable gcp_iam_permission_policy
// source fact for one GCP IAM grant: a service-account principal granted a role
// on a resource. It mirrors NewPermissionPolicyEnvelope for GCP. The principal
// join key is the same member fingerprint the principal fact carries, so the
// reducer joins grants to principals by construction.
func NewGCPPermissionPolicyEnvelope(observation GCPPermissionPolicyObservation) (facts.Envelope, error) {
	if err := validateGCPContext(observation.Context); err != nil {
		return facts.Envelope{}, err
	}
	fingerprint := strings.TrimSpace(observation.PrincipalFingerprint)
	if fingerprint == "" {
		return facts.Envelope{}, fmt.Errorf("gcp secrets iam permission policy observation requires principal_fingerprint")
	}
	role := strings.TrimSpace(observation.Role)
	if role == "" {
		return facts.Envelope{}, fmt.Errorf("gcp secrets iam permission policy observation requires role")
	}
	resourceName := strings.TrimSpace(observation.ResourceFullName)
	if resourceName == "" {
		return facts.Envelope{}, fmt.Errorf("gcp secrets iam permission policy observation requires resource_full_resource_name")
	}
	principalType := strings.TrimSpace(observation.PrincipalType)
	if principalType == "" {
		principalType = PrincipalTypeGCPServiceAccount
	}
	conditionFingerprint := strings.TrimSpace(observation.ConditionFingerprint)
	// The CAI resource_full_name already encodes the resource's project, so it is
	// not folded into the key separately; identity is (principal, role, resource,
	// condition).
	stableKey := facts.StableID(facts.GCPIAMPermissionPolicyFactKind, map[string]any{
		"principal_fingerprint": fingerprint,
		"role":                  role,
		"resource_full_name":    resourceName,
		"condition_fingerprint": conditionFingerprint,
	})
	payload := gcpCommonPayload(observation.Context)
	payload["principal_fingerprint"] = fingerprint
	payload["principal_type"] = principalType
	payload["role"] = role
	payload["resource_full_resource_name"] = resourceName
	payload["resource_asset_type"] = strings.TrimSpace(observation.ResourceAssetType)
	payload["resource_is_secret"] = observation.ResourceIsSecret
	payload["broad_role"] = observation.BroadRole
	payload["condition_present"] = observation.ConditionPresent
	if conditionFingerprint != "" {
		payload["condition_fingerprint"] = conditionFingerprint
	}
	return newEnvelope(
		gcpToEnvelopeContext(observation.Context),
		facts.GCPIAMPermissionPolicyFactKind,
		stableKey,
		sourceRecordID(observation.SourceRecordID, fingerprint+"|"+role+"|"+resourceName),
		firstNonBlank(observation.SourceURI, observation.Context.SourceURI),
		payload,
	), nil
}

// gcpCommonPayload stamps the GCP-native common fields onto a secrets/IAM
// source-fact payload, parallel to commonPayload for AWS.
func gcpCommonPayload(ctx GCPEnvelopeContext) map[string]any {
	return map[string]any{
		"provider":                 ProviderGCPIAM,
		"project_id":               strings.TrimSpace(ctx.ProjectID),
		"location_bucket":          strings.TrimSpace(ctx.LocationBucket),
		"collector_instance_id":    strings.TrimSpace(ctx.CollectorInstanceID),
		"redaction_policy_version": RedactionPolicyVersion,
	}
}

// gcpToEnvelopeContext adapts a GCP context to the shared EnvelopeContext that
// newEnvelope consumes for scope/generation/fencing/observed-at/source fields.
// The project id rides in AccountID and the location bucket in Region purely so
// newEnvelope's contract is satisfied; the GCP payload itself uses project_id.
func gcpToEnvelopeContext(ctx GCPEnvelopeContext) EnvelopeContext {
	return EnvelopeContext{
		AccountID:           ctx.ProjectID,
		Region:              ctx.LocationBucket,
		ScopeID:             ctx.ScopeID,
		GenerationID:        ctx.GenerationID,
		CollectorInstanceID: ctx.CollectorInstanceID,
		FencingToken:        ctx.FencingToken,
		ObservedAt:          ctx.ObservedAt,
		SourceURI:           ctx.SourceURI,
	}
}

func validateGCPContext(ctx GCPEnvelopeContext) error {
	// project_id is descriptive provenance, not identity, so it is not required:
	// organization- and folder-level IAM bindings carry no project segment, and
	// the principal/permission identity is the service-account fingerprint.
	switch {
	case strings.TrimSpace(ctx.ScopeID) == "":
		return fmt.Errorf("gcp secrets iam observation requires scope_id")
	case strings.TrimSpace(ctx.GenerationID) == "":
		return fmt.Errorf("gcp secrets iam observation requires generation_id")
	case strings.TrimSpace(ctx.CollectorInstanceID) == "":
		return fmt.Errorf("gcp secrets iam observation requires collector_instance_id")
	case ctx.FencingToken <= 0:
		return fmt.Errorf("gcp secrets iam observation fencing_token must be positive")
	default:
		return nil
	}
}
