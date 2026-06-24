// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/component"

func componentExtensionTrustDecision(entry component.RegistryReadbackComponent) ComponentExtensionTrustDecision {
	if entry.Error != nil && entry.Verification == nil {
		return ComponentExtensionTrustDecision{
			Decision: "not_evaluated",
			Code:     entry.Error.Code,
			Reason:   safeComponentExtensionErrorMessage(entry.Error.Code),
		}
	}
	if entry.Verification == nil {
		return ComponentExtensionTrustDecision{Decision: "not_evaluated"}
	}
	if entry.Verification.Allowed {
		return ComponentExtensionTrustDecision{Decision: "allowed"}
	}
	return ComponentExtensionTrustDecision{
		Decision: "blocked",
		Code:     entry.Verification.Code,
		Reason:   safeComponentExtensionErrorMessage(entry.Verification.Code),
	}
}

func componentExtensionPolicyGate(entry component.RegistryReadbackComponent) ComponentExtensionPolicyGate {
	mode := ""
	code := component.ErrorCode("")
	if entry.Verification != nil {
		mode = entry.Verification.Mode
		code = entry.Verification.Code
		if entry.Verification.Allowed {
			return ComponentExtensionPolicyGate{State: "allowed", Mode: mode}
		}
	}
	if entry.Error != nil && entry.Verification == nil {
		code = entry.Error.Code
		return ComponentExtensionPolicyGate{State: "runtime_failure", Code: code}
	}
	switch code {
	case component.ErrorCodeIncompatibleCore:
		return ComponentExtensionPolicyGate{State: "incompatible", Mode: mode, Code: code}
	case component.ErrorCodeProvenanceRequired:
		return ComponentExtensionPolicyGate{State: "missing_provenance", Mode: mode, Code: code}
	case component.ErrorCodeProvenanceInvalid, component.ErrorCodeUnsupportedProvenance:
		return ComponentExtensionPolicyGate{State: "invalid_provenance", Mode: mode, Code: code}
	case component.ErrorCodeUntrustedPublisher, component.ErrorCodeRevokedPackage:
		return ComponentExtensionPolicyGate{State: "disabled_by_policy", Mode: mode, Code: code}
	case "":
		return ComponentExtensionPolicyGate{State: "not_evaluated", Mode: mode}
	default:
		return ComponentExtensionPolicyGate{State: "blocked_by_policy", Mode: mode, Code: code}
	}
}

func componentExtensionSchedulerState(entry component.RegistryReadbackComponent) ComponentExtensionSchedulerState {
	if entry.Error != nil && entry.Verification == nil {
		return ComponentExtensionSchedulerState{State: "runtime_failure", Reason: "component_readback_failed"}
	}
	if entry.Verification != nil && !entry.Verification.Allowed {
		return ComponentExtensionSchedulerState{State: "blocked_by_policy", Reason: string(entry.Verification.Code)}
	}
	if hasComponentExtensionState(entry.States, component.RegistryStateClaimCapable) {
		if entry.Verification == nil || !entry.Verification.Allowed {
			return ComponentExtensionSchedulerState{State: "blocked_by_policy", Reason: "policy_not_evaluated"}
		}
		return ComponentExtensionSchedulerState{State: "claim_capable", Reason: "activation_allows_claims"}
	}
	if hasComponentExtensionState(entry.States, component.RegistryStateEnabled) {
		return ComponentExtensionSchedulerState{State: "enabled_not_claim_capable", Reason: "activation_claims_disabled"}
	}
	return ComponentExtensionSchedulerState{State: "installed_not_enabled", Reason: "activation_missing"}
}

func componentExtensionReadModelAvailability(
	entry component.RegistryReadbackComponent,
) ComponentExtensionReadModelAvailability {
	scheduler := componentExtensionSchedulerState(entry)
	switch scheduler.State {
	case "runtime_failure":
		return ComponentExtensionReadModelAvailability{State: "unavailable", UnavailableReason: "runtime_failure"}
	case "blocked_by_policy":
		return ComponentExtensionReadModelAvailability{State: "unavailable", UnavailableReason: "policy_blocked"}
	case "claim_capable":
		return ComponentExtensionReadModelAvailability{State: "unavailable", UnavailableReason: "missing_conformance_proof"}
	case "enabled_not_claim_capable":
		return ComponentExtensionReadModelAvailability{State: "unavailable", UnavailableReason: "claims_disabled"}
	default:
		return ComponentExtensionReadModelAvailability{State: "unavailable", UnavailableReason: "activation_missing"}
	}
}

func hasComponentExtensionState(states []string, want string) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}
