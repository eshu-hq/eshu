// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"net/http"
	"slices"
)

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
// It panics rather than serving a partial inventory when a declared order is
// incomplete, duplicated, or names an unregistered capability.
func CapabilityRegistrations() []CapabilityRegistration {
	order := capabilityOrder
	if requestedCapabilityOrder != nil {
		if !validCapabilityOrder(requestedCapabilityOrder) {
			panic("querycontract: canonical capability order is incomplete, duplicated, or names an unknown capability")
		}
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

// HardcodedSecretCapability names the hardcoded-secret investigation capability.
// It lives here so the staying registration in root package query's
// contract_hardcoded_secret_capability.go keeps resolving after the
// code-family file that implements the route moves out in a later #6060
// phase. The family file keeps its own literal until that move: the
// capability-sweep gate resolves string literals, not cross-package const
// aliases, so aliasing it now would trade a temporary duplication for an
// unverifiable capability argument.
const HardcodedSecretCapability = "security.hardcoded_secrets"

// HardcodedSecretSupport returns this family's capability contract: the
// per-profile truth ceiling for hardcoded-secret investigation.
//
// This is the ONLY declaration of these values. Root package query's staying
// contract_hardcoded_secret_capability.go registers the result for production.
// It is a function, not an exported var, and every call allocates its own
// truth levels rather than pointing at package-level ones. CapabilitySupport
// carries its ceilings as pointers, so a shared var would hand every caller --
// including root's production registration -- write access to the same ints
// (see semanticsearch.Support, the template this copies).
//
// The four ceilings get separate variables on purpose. Returning four pointers
// to one local would leave them aliased inside the returned struct, so writing
// through any one of them would silently move the other three.
func HardcodedSecretSupport() CapabilitySupport {
	localLightweightMax := TruthLevelDerived
	localAuthoritativeMax := TruthLevelDerived
	localFullStackMax := TruthLevelDerived
	productionMax := TruthLevelDerived
	return CapabilitySupport{
		LocalLightweightMax:   &localLightweightMax,
		LocalAuthoritativeMax: &localAuthoritativeMax,
		LocalFullStackMax:     &localFullStackMax,
		ProductionMax:         &productionMax,
	}
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

// RequireContextOverview writes the structured unsupported-capability
// envelope and returns false when the profile cannot serve
// platform_impact.context_overview, the shared capability behind the
// repository, service, and workload context, story, summary, dossier, and
// investigation readbacks. message names the specific surface so the
// operator sees which call needs an authoritative platform profile. It
// lives here (not in a handler family) so the repository routes and the
// entity-workload stayer share one gate without importing each other
// (#6060, lane B B3).
func RequireContextOverview(w http.ResponseWriter, r *http.Request, profile QueryProfile, message string) bool {
	const capability = "platform_impact.context_overview"
	if CapabilityUnsupported(profile, capability) {
		WriteContractError(w, r, http.StatusNotImplemented, message,
			"unsupported_capability", capability, profile, RequiredProfile(capability))
		return false
	}
	return true
}
