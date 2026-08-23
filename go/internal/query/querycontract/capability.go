// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "slices"

// CapabilitySupport records the truth ceiling and minimum profile for one capability.
type CapabilitySupport struct {
	LocalLightweightMax   *TruthLevel
	LocalAuthoritativeMax *TruthLevel
	LocalFullStackMax     *TruthLevel
	ProductionMax         *TruthLevel
	RequiredProfile       QueryProfile
}

// CapabilityRegistration binds a capability ID to its support contract.
type CapabilityRegistration struct {
	Capability string
	Support    CapabilitySupport
}

var (
	capabilityRegistry        = map[string]CapabilitySupport{}
	capabilityOrder           []string
	requestedCapabilityOrder  []string
	duplicateRegistrationKeys []string
)

// RegisterCapabilities adds family-owned capability rows to the shared registry.
// A repeated key retains the historical last-write-wins behavior and is also
// recorded so contract tests can reject duplicate initialization attempts.
func RegisterCapabilities(registrations ...CapabilityRegistration) {
	for _, registration := range registrations {
		if _, exists := capabilityRegistry[registration.Capability]; exists {
			duplicateRegistrationKeys = append(duplicateRegistrationKeys, registration.Capability)
		} else {
			capabilityOrder = append(capabilityOrder, registration.Capability)
		}
		capabilityRegistry[registration.Capability] = registration.Support
	}
}

// SetCapabilitySupport applies the legacy low-level last-write-wins mutation.
// New family packages should call RegisterCapabilities instead.
func SetCapabilitySupport(capability string, support CapabilitySupport) {
	if _, exists := capabilityRegistry[capability]; !exists {
		capabilityOrder = append(capabilityOrder, capability)
	}
	capabilityRegistry[capability] = support
}

// CapabilitySupportFor returns the registered support row for capability.
func CapabilitySupportFor(capability string) (CapabilitySupport, bool) {
	support, ok := capabilityRegistry[capability]
	return support, ok
}

// CapabilityRegistrations returns a copy in canonical registry order.
func CapabilityRegistrations() []CapabilityRegistration {
	order := capabilityOrder
	if validCapabilityOrder(requestedCapabilityOrder) {
		order = requestedCapabilityOrder
	}
	registrations := make([]CapabilityRegistration, 0, len(order))
	for _, capability := range order {
		registrations = append(registrations, CapabilityRegistration{
			Capability: capability,
			Support:    capabilityRegistry[capability],
		})
	}
	return registrations
}

// SetCapabilityOrder declares the canonical externally specified order.
// The declaration may precede registrations because callers cannot observe the
// registry until package initialization has completed.
func SetCapabilityOrder(capabilities []string) {
	requestedCapabilityOrder = slices.Clone(capabilities)
}

func validCapabilityOrder(capabilities []string) bool {
	if len(capabilities) != len(capabilityRegistry) {
		return false
	}
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if _, ok := capabilityRegistry[capability]; !ok {
			return false
		}
		if _, duplicate := seen[capability]; duplicate {
			return false
		}
		seen[capability] = struct{}{}
	}
	return true
}

// DuplicateCapabilityRegistrations returns repeated registration keys in attempt order.
func DuplicateCapabilityRegistrations() []string {
	return slices.Clone(duplicateRegistrationKeys)
}

// CompatibilityCapabilityMatrix returns the live registry for root-package aliases.
// It exists only while callers migrate from query's historical package-private map.
func CompatibilityCapabilityMatrix() map[string]CapabilitySupport {
	return capabilityRegistry
}

func maxTruthLevel(capability string, profile QueryProfile) *TruthLevel {
	support, ok := capabilityRegistry[capability]
	if !ok {
		return nil
	}
	switch profile {
	case ProfileLocalLightweight:
		return support.LocalLightweightMax
	case ProfileLocalAuthoritative:
		if support.LocalAuthoritativeMax != nil {
			return support.LocalAuthoritativeMax
		}
		return support.LocalLightweightMax
	case ProfileLocalFullStack:
		return support.LocalFullStackMax
	case ProfileProduction:
		return support.ProductionMax
	default:
		return support.ProductionMax
	}
}
