// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

// Maturity is the catalog-level lifecycle state of a capability. The
// matrix-derived value (deriveMaturity) reflects the runtime support contract.
// Operational states that the matrix cannot express on its own (gated by a
// pending chart, degraded by a known incident) are supplied by the catalog
// overlay and take precedence over the derived value.
type Maturity string

const (
	// MaturityGeneralAvailability means the capability is supported in the
	// production profile.
	MaturityGeneralAvailability Maturity = "general_availability"
	// MaturityExperimental means the production profile marks the capability
	// experimental.
	MaturityExperimental Maturity = "experimental"
	// MaturityPreview means the capability is supported in at least one local
	// profile but not yet in production.
	MaturityPreview Maturity = "preview"
	// MaturityGated is an overlay-only state: the capability exists and the
	// matrix may even mark it supported, but it is off in the default surface
	// until an operator opts in. It covers a capability withheld from a public
	// surface (for example a pending public chart), an env-var gate the matrix
	// cannot express (for example ESHU_EMIT_DATAFLOW value-flow reachability or
	// ESHU_ASK_ENABLED), and a feeding collector that is off in a default deploy
	// and enabled only through ESHU_COLLECTOR_INSTANCES_JSON plus credentials
	// (the supply-chain list capabilities). The maturity_reason names the gate.
	MaturityGated Maturity = "gated"
	// MaturityDegraded is an overlay-only state: the capability is exposed but
	// operating below contract because of a tracked condition.
	MaturityDegraded Maturity = "degraded"
	// MaturityNotImplemented means no profile supports the capability.
	MaturityNotImplemented Maturity = "not_implemented"
)

// overlayMaturities is the set of maturity states that only the overlay may
// assign because they cannot be derived from the runtime support matrix.
var overlayMaturities = map[Maturity]struct{}{
	MaturityGated:    {},
	MaturityDegraded: {},
}

// statusSupported, statusExperimental, and statusUnsupported are the only
// per-profile status values the capability matrix uses today.
const (
	statusSupported    = "supported"
	statusExperimental = "experimental"
	statusUnsupported  = "unsupported"
)

// ProfileSupport is the slice of a matrix profile row used for maturity
// derivation. Only the support status is needed; truth ceilings and proof
// signals are carried elsewhere on the catalog entry.
type ProfileSupport struct {
	// Status is the matrix status for the profile: supported, experimental, or
	// unsupported.
	Status string
}

// deriveMaturity computes the matrix-derived maturity from the per-profile
// support statuses keyed by profile id. The production profile decides the
// headline state; when it is absent the best local profile decides instead so
// that local-only capabilities surface as preview rather than not implemented.
func deriveMaturity(profiles map[string]ProfileSupport) Maturity {
	production, hasProduction := profiles[string(ProfileProduction)]
	if hasProduction {
		switch production.Status {
		case statusSupported:
			return MaturityGeneralAvailability
		case statusExperimental:
			return MaturityExperimental
		}
	}

	anySupported := false
	anyExperimental := false
	for profile, support := range profiles {
		if profile == string(ProfileProduction) {
			continue
		}
		switch support.Status {
		case statusSupported:
			anySupported = true
		case statusExperimental:
			anyExperimental = true
		}
	}

	switch {
	case anySupported:
		return MaturityPreview
	case anyExperimental:
		return MaturityExperimental
	default:
		return MaturityNotImplemented
	}
}
