// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ask

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

// Capability is the capability id POST /api/v0/ask serves.
const Capability = "ask.natural_language_answer"

// Support returns this family's capability contract: derived truth on every
// profile down to local-lightweight, matching the inline row this replaces
// in contract/ask.go.
//
// This is the single declaration of the row. contract/ask.go registers it
// for production and this package's main_test.go registers it for tests that
// cannot link root (#6642), so there is no second copy to drift. It returns
// fresh truth-level pointers on every call, one per ceiling, so no caller
// can mutate another's ceiling.
func Support() querycontract.CapabilitySupport {
	lightweightMax := querycontract.TruthLevelDerived
	authoritativeMax := querycontract.TruthLevelDerived
	fullStackMax := querycontract.TruthLevelDerived
	productionMax := querycontract.TruthLevelDerived
	return querycontract.CapabilitySupport{
		LocalLightweightMax:   &lightweightMax,
		LocalAuthoritativeMax: &authoritativeMax,
		LocalFullStackMax:     &fullStackMax,
		ProductionMax:         &productionMax,
		RequiredProfile:       querycontract.ProfileLocalLightweight,
	}
}
