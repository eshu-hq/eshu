// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file declares the capability support rows for the evidence family's
// routes. It does NOT register them: registration stays in root package
// query (contract/capability_matrix.go and contract/admission_decisions.go),
// which owns the router and always links into the production binary. Both
// root's matrix and this package's TestMain call these constructors, so
// production and the evidence test binary exercise the same gate from a
// single declaration (the supply/chain family precedent, #6060).

// RelationshipEvidenceSupport is the support row for GET
// /api/v0/evidence/relationships/{resolved_id}: exact truth from the
// authoritative local profile up. The drilldown reads the durable Postgres
// relationship read model, which does not exist on the lightweight profile.
func RelationshipEvidenceSupport() querycontract.CapabilitySupport {
	exact := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}

// CitationPacketSupport is the support row for POST
// /api/v0/evidence/citations: derived truth on every profile, with no
// required profile. Citation hydration reads the bounded Postgres content
// store without graph traversal, so it serves the lightest profile too.
func CitationPacketSupport() querycontract.CapabilitySupport {
	derived := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &derived,
		LocalAuthoritativeMax: &derived,
		LocalFullStackMax:     &derived,
		ProductionMax:         &derived,
	}
}

// AdmissionDecisionSupport is the support row for GET
// /api/v0/evidence/admission-decisions and GET
// /api/v0/investigations/deployable-unit/packet: exact truth from the
// authoritative local profile up. Both read the reducer-owned Postgres
// admission decision read model, which does not exist on the lightweight
// profile.
func AdmissionDecisionSupport() querycontract.CapabilitySupport {
	exact := querycontract.TruthLevelExact
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &exact,
		LocalFullStackMax:     &exact,
		ProductionMax:         &exact,
		RequiredProfile:       querycontract.ProfileLocalAuthoritative,
	}
}
