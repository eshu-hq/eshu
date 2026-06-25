// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redact

import (
	"bytes"
	"fmt"
)

// Surface names a hosted output boundary covered by the redaction registry.
type Surface string

const (
	// SurfaceFacts covers persisted fact payloads and fact metadata.
	SurfaceFacts Surface = "facts"
	// SurfaceLogs covers structured log fields.
	SurfaceLogs Surface = "logs"
	// SurfaceMetricLabels covers Prometheus or OTEL metric labels.
	SurfaceMetricLabels Surface = "metric_labels"
	// SurfaceStatusErrors covers admin, status, and readiness error payloads.
	SurfaceStatusErrors Surface = "status_errors"
	// SurfaceGraphProperties covers canonical graph node and relationship fields.
	SurfaceGraphProperties Surface = "graph_properties"
	// SurfaceAPIMCPBodies covers HTTP API and MCP response bodies.
	SurfaceAPIMCPBodies Surface = "api_mcp_bodies"
	// SurfaceConsoleSurfaces covers browser console and dashboard payloads.
	SurfaceConsoleSurfaces Surface = "console_surfaces"
	// SurfaceAuditEvents covers hosted governance audit event payloads.
	SurfaceAuditEvents Surface = "audit_events"
	// SurfaceDocsExamples covers checked-in public or internal documentation.
	SurfaceDocsExamples Surface = "docs_examples"
	// SurfaceOnboardingArtifacts covers generated hosted onboarding artifacts.
	SurfaceOnboardingArtifacts Surface = "onboarding_artifacts"
)

// SensitiveClass identifies a raw value family that must not cross a surface.
type SensitiveClass string

const (
	// SensitiveRawToken covers bearer tokens, API tokens, and session tokens.
	SensitiveRawToken SensitiveClass = "raw_token" // #nosec G101 -- classification label string, not a credential
	// SensitiveProviderKey covers model, cloud, SaaS, or source-system keys.
	SensitiveProviderKey SensitiveClass = "provider_key"
	// SensitivePrompt covers raw prompt, response, and semantic source text.
	SensitivePrompt SensitiveClass = "prompt_or_provider_payload"
	// SensitivePrivateURL covers URLs that carry credentials or private routing.
	SensitivePrivateURL SensitiveClass = "private_url"
	// SensitiveSecretValue covers secret values, passwords, and private keys.
	SensitiveSecretValue SensitiveClass = "secret_value"
	// SensitivePersonalIdentifier covers direct personal identifiers.
	SensitivePersonalIdentifier SensitiveClass = "personal_identifier"
	// SensitivePrivateSourceIdentifier covers private source, file, or tenant-specific identifiers.
	SensitivePrivateSourceIdentifier SensitiveClass = "private_source_identifier"
)

// SafeClass identifies a bounded field family that a surface may expose.
type SafeClass string

const (
	// SafeCredentialReference is a credential handle class, never a value.
	SafeCredentialReference SafeClass = "credential_reference" // #nosec G101 -- safe-class taxonomy label, not a credential value
	// SafeCredentialSourceKind is the credential source kind without its handle.
	SafeCredentialSourceKind SafeClass = "credential_source_kind" // #nosec G101 -- safe-class taxonomy label, not a credential value
	// SafeProviderProfileID is a configured provider profile identifier.
	SafeProviderProfileID SafeClass = "provider_profile_id"
	// SafeSourceClass is a low-cardinality source class.
	SafeSourceClass SafeClass = "source_class"
	// SafeTenantWorkspaceID is a tenant or workspace identifier where policy allows it.
	SafeTenantWorkspaceID SafeClass = "tenant_workspace_id"
	// SafeActorClass is a low-cardinality actor type or subject class.
	SafeActorClass SafeClass = "actor_class"
	// SafeReasonCode is a low-cardinality reason or failure class.
	SafeReasonCode SafeClass = "reason_code"
	// SafePolicyState is a low-cardinality policy state or mode.
	SafePolicyState SafeClass = "policy_state"
	// SafeRedactionMarker is a deterministic redaction marker.
	SafeRedactionMarker SafeClass = "redaction_marker"
	// SafeCollectorKind is a low-cardinality collector kind.
	SafeCollectorKind SafeClass = "collector_kind"
)

// Canary is one synthetic sensitive sample used for negative leakage tests.
type Canary struct {
	Class SensitiveClass
	Raw   string
}

// SurfacePolicy describes the redaction contract for one output surface.
type SurfacePolicy struct {
	Surface          Surface
	ForbiddenClasses []SensitiveClass
	SafeClasses      []SafeClass
}

// Registry is the central hosted-governance redaction matrix.
type Registry struct {
	policies []SurfacePolicy
	canaries []Canary
}

// HostedGovernanceRegistry returns the collector-neutral hosted redaction matrix.
func HostedGovernanceRegistry() Registry {
	forbidden := []SensitiveClass{
		SensitiveRawToken,
		SensitiveProviderKey,
		SensitivePrompt,
		SensitivePrivateURL,
		SensitiveSecretValue,
		SensitivePersonalIdentifier,
		SensitivePrivateSourceIdentifier,
	}
	commonSafe := []SafeClass{
		SafeCredentialSourceKind,
		SafeProviderProfileID,
		SafeSourceClass,
		SafeTenantWorkspaceID,
		SafeActorClass,
		SafeReasonCode,
		SafePolicyState,
		SafeRedactionMarker,
		SafeCollectorKind,
	}
	metricSafe := []SafeClass{
		SafeCredentialSourceKind,
		SafeSourceClass,
		SafeActorClass,
		SafeReasonCode,
		SafePolicyState,
		SafeCollectorKind,
	}
	return Registry{
		policies: []SurfacePolicy{
			policy(SurfaceFacts, forbidden, append(commonSafe, SafeCredentialReference)),
			policy(SurfaceLogs, forbidden, commonSafe),
			policy(SurfaceMetricLabels, forbidden, metricSafe),
			policy(SurfaceStatusErrors, forbidden, append(commonSafe, SafeCredentialReference)),
			policy(SurfaceGraphProperties, forbidden, commonSafe),
			policy(SurfaceAPIMCPBodies, forbidden, append(commonSafe, SafeCredentialReference)),
			policy(SurfaceConsoleSurfaces, forbidden, append(commonSafe, SafeCredentialReference)),
			policy(SurfaceAuditEvents, forbidden, append(commonSafe, SafeCredentialReference)),
			policy(SurfaceDocsExamples, forbidden, commonSafe),
			policy(SurfaceOnboardingArtifacts, forbidden, append(commonSafe, SafeCredentialReference)),
		},
		canaries: []Canary{
			{Class: SensitiveRawToken, Raw: "CANARY_RAW_TOKEN_DO_NOT_PERSIST_123"},
			{Class: SensitiveProviderKey, Raw: "CANARY_PROVIDER_KEY_DO_NOT_PERSIST_456"},
			{Class: SensitivePrompt, Raw: "CANARY_PROMPT_PAYLOAD_DO_NOT_PERSIST_789"},
			{Class: SensitivePrivateURL, Raw: "https://user:pass@example.invalid/private-redaction-canary"}, // #nosec G101 -- synthetic canary value used only for negative leakage tests, never a real credential
			{Class: SensitiveSecretValue, Raw: "correct-horse-redaction-canary"},
			{Class: SensitivePersonalIdentifier, Raw: "operator.person@example.invalid"},
			{Class: SensitivePrivateSourceIdentifier, Raw: "repo://private/source-redaction-canary"},
		},
	}
}

// Surfaces returns all registry surfaces in stable order.
func (r Registry) Surfaces() []Surface {
	surfaces := make([]Surface, 0, len(r.policies))
	for _, policy := range r.policies {
		surfaces = append(surfaces, policy.Surface)
	}
	return surfaces
}

// Policy returns the redaction policy for surface.
func (r Registry) Policy(surface Surface) (SurfacePolicy, bool) {
	for _, candidate := range r.policies {
		if candidate.Surface == surface {
			return copyPolicy(candidate), true
		}
	}
	return SurfacePolicy{}, false
}

// Canaries returns every synthetic sensitive canary in stable order.
func (r Registry) Canaries() []Canary {
	return append([]Canary(nil), r.canaries...)
}

// ForbiddenCanaries returns canaries forbidden from the supplied surface.
func (r Registry) ForbiddenCanaries(surface Surface) []Canary {
	policy, ok := r.Policy(surface)
	if !ok {
		return nil
	}
	canaries := make([]Canary, 0, len(r.canaries))
	for _, sample := range r.canaries {
		if containsSensitiveClass(policy.ForbiddenClasses, sample.Class) {
			canaries = append(canaries, sample)
		}
	}
	return canaries
}

// AssertNoForbiddenCanary reports whether payload contains a forbidden canary.
func (r Registry) AssertNoForbiddenCanary(surface Surface, payload []byte) error {
	if _, ok := r.Policy(surface); !ok {
		return fmt.Errorf("redaction registry surface %q is not defined", surface)
	}
	for _, sample := range r.ForbiddenCanaries(surface) {
		if bytes.Contains(payload, []byte(sample.Raw)) {
			return fmt.Errorf("redaction canary leak on surface %q class %q", surface, sample.Class)
		}
	}
	return nil
}

// IsSafeClass reports whether class is approved for surface.
func (r Registry) IsSafeClass(surface Surface, class SafeClass) bool {
	policy, ok := r.Policy(surface)
	if !ok {
		return false
	}
	return containsSafeClass(policy.SafeClasses, class)
}

func policy(surface Surface, forbidden []SensitiveClass, safe []SafeClass) SurfacePolicy {
	return SurfacePolicy{
		Surface:          surface,
		ForbiddenClasses: append([]SensitiveClass(nil), forbidden...),
		SafeClasses:      append([]SafeClass(nil), safe...),
	}
}

func copyPolicy(source SurfacePolicy) SurfacePolicy {
	return SurfacePolicy{
		Surface:          source.Surface,
		ForbiddenClasses: append([]SensitiveClass(nil), source.ForbiddenClasses...),
		SafeClasses:      append([]SafeClass(nil), source.SafeClasses...),
	}
}

func containsSensitiveClass(classes []SensitiveClass, class SensitiveClass) bool {
	for _, candidate := range classes {
		if candidate == class {
			return true
		}
	}
	return false
}

func containsSafeClass(classes []SafeClass, class SafeClass) bool {
	for _, candidate := range classes {
		if candidate == class {
			return true
		}
	}
	return false
}
