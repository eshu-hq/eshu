// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

// Registry lifecycle states emitted by Registry.Readback.
const (
	RegistryStateInstalled    = "installed"
	RegistryStateEnabled      = "enabled"
	RegistryStateClaimCapable = "claim_capable"
	RegistryStateRevoked      = "revoked"
	RegistryStateIncompatible = "incompatible"
	RegistryStateFailed       = "failed"
)

// ErrorSummary is a stable, serializable error class and message.
type ErrorSummary struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// RegistryReadbackComponent combines installed package metadata with derived
// lifecycle and policy states.
type RegistryReadbackComponent struct {
	InstalledComponent
	States       []string            `json:"states"`
	Verification *VerificationResult `json:"verification,omitempty"`
	Error        *ErrorSummary       `json:"error,omitempty"`
}

// Readback returns installed packages with deterministic lifecycle and policy
// states. A zero Policy skips policy re-verification and only reports stored
// lifecycle state plus local manifest read failures.
func (r Registry) Readback(policy Policy) ([]RegistryReadbackComponent, error) {
	components, err := r.List()
	if err != nil {
		return nil, err
	}
	readback := make([]RegistryReadbackComponent, 0, len(components))
	for _, installed := range components {
		installed.ManifestPath = r.manifestPath(installed.ID, installed.Version)
		entry := RegistryReadbackComponent{
			InstalledComponent: installed,
			States:             lifecycleStates(installed),
		}
		manifest, err := LoadManifest(installed.ManifestPath)
		if err != nil {
			entry.States = appendFailureState(entry.States, ErrorCodeOf(err))
			entry.Error = errorSummary(err, ErrorCodeInvalidManifest)
			readback = append(readback, entry)
			continue
		}
		if !policy.isZero() {
			result := policy.Verify(manifest)
			entry.Verification = &result
			if !result.Allowed {
				entry.States = appendFailureState(entry.States, result.Code)
				entry.Error = &ErrorSummary{
					Code:    resultErrorCode(result, ErrorCodeUntrustedPublisher),
					Message: result.Reason,
				}
			}
		}
		readback = append(readback, entry)
	}
	return readback, nil
}

func lifecycleStates(installed InstalledComponent) []string {
	states := []string{RegistryStateInstalled}
	if len(installed.Activations) == 0 {
		return states
	}
	states = append(states, RegistryStateEnabled)
	for _, activation := range installed.Activations {
		if activation.ClaimsEnabled {
			return append(states, RegistryStateClaimCapable)
		}
	}
	return states
}

func appendFailureState(states []string, code ErrorCode) []string {
	switch code {
	case ErrorCodeRevokedPackage:
		states = append(states, RegistryStateRevoked)
	case ErrorCodeIncompatibleCore:
		states = append(states, RegistryStateIncompatible)
	}
	return append(states, RegistryStateFailed)
}

func errorSummary(err error, fallback ErrorCode) *ErrorSummary {
	code := ErrorCodeOf(err)
	if code == "" {
		code = fallback
	}
	return &ErrorSummary{Code: code, Message: err.Error()}
}
