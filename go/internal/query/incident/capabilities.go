// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file owns the incident family's capability rows. The rows live here —
// declared by the family that implements the routes, following the
// service/capabilities.go precedent — so the family's own test binary
// observes the same profile gates production does without importing package
// query (which would cycle through the root alias shim). The query root's
// matrix no longer repeats these rows: two copies drift silently.
// Registration is last-write-wins and idempotent for identical rows; the
// contract suite rejects duplicate initialization, so keep each capability
// in exactly one home. See #6060.
var (
	incidentTruthDerived = querycontract.TruthLevelDerived
)

func init() {
	querycontract.RegisterCapabilities(
		querycontract.CapabilityRegistration{
			Capability: model.Capability,
			Support: querycontract.CapabilitySupport{
				LocalLightweightMax:   &incidentTruthDerived,
				LocalAuthoritativeMax: &incidentTruthDerived,
				LocalFullStackMax:     &incidentTruthDerived,
				ProductionMax:         &incidentTruthDerived,
			},
		},
	)
}
