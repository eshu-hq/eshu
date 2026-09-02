// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cicdrun"
)

// This file is the transitional compatibility surface for the CI/CD run
// correlation domain that moved to [cicdrun] (issue #6061). Reducer-root call
// sites and the external packages that name these types keep their current
// spelling; each entry is deleted once its last caller has moved into a
// family subpackage.

// CICDRunCorrelationOutcome forwards to [cicdrun.CICDRunCorrelationOutcome].
type CICDRunCorrelationOutcome = cicdrun.CICDRunCorrelationOutcome

// The CICDRunCorrelation outcome values forward to their [cicdrun] equivalents.
const (
	CICDRunCorrelationExact      = cicdrun.CICDRunCorrelationExact
	CICDRunCorrelationDerived    = cicdrun.CICDRunCorrelationDerived
	CICDRunCorrelationAmbiguous  = cicdrun.CICDRunCorrelationAmbiguous
	CICDRunCorrelationUnresolved = cicdrun.CICDRunCorrelationUnresolved
	CICDRunCorrelationRejected   = cicdrun.CICDRunCorrelationRejected
)

// CICDRunCorrelationDecision forwards to [cicdrun.CICDRunCorrelationDecision].
type CICDRunCorrelationDecision = cicdrun.CICDRunCorrelationDecision

// CICDRunCorrelationWrite forwards to [cicdrun.CICDRunCorrelationWrite].
type CICDRunCorrelationWrite = cicdrun.CICDRunCorrelationWrite

// CICDRunCorrelationWriteResult forwards to
// [cicdrun.CICDRunCorrelationWriteResult].
type CICDRunCorrelationWriteResult = cicdrun.CICDRunCorrelationWriteResult

// CICDRunCorrelationWriter forwards to [cicdrun.CICDRunCorrelationWriter].
type CICDRunCorrelationWriter = cicdrun.CICDRunCorrelationWriter

// CICDRunCorrelationHandler forwards to [cicdrun.CICDRunCorrelationHandler].
type CICDRunCorrelationHandler = cicdrun.CICDRunCorrelationHandler

// PostgresCICDRunCorrelationWriter forwards to
// [cicdrun.PostgresCICDRunCorrelationWriter].
type PostgresCICDRunCorrelationWriter = cicdrun.PostgresCICDRunCorrelationWriter

// cicdRunCorrelationFactKind forwards to [cicdrun.CICDRunCorrelationFactKind].
const cicdRunCorrelationFactKind = cicdrun.CICDRunCorrelationFactKind

// cicdWorkflowImageBuiltFromEvidenceSource forwards to
// [cicdrun.CICDWorkflowImageBuiltFromEvidenceSource].
const cicdWorkflowImageBuiltFromEvidenceSource = cicdrun.CICDWorkflowImageBuiltFromEvidenceSource

// BuildCICDRunCorrelationDecisions forwards to
// [cicdrun.BuildCICDRunCorrelationDecisions].
func BuildCICDRunCorrelationDecisions(envelopes []facts.Envelope) []CICDRunCorrelationDecision {
	return cicdrun.BuildCICDRunCorrelationDecisions(envelopes)
}

// cicdRunKeyFromParts forwards to [cicdrun.CICDRunKeyFromParts].
func cicdRunKeyFromParts(provider, runID string, runAttempt *string) string {
	return cicdrun.CICDRunKeyFromParts(provider, runID, runAttempt)
}

// trimmedCICDPtr forwards to [cicdrun.TrimmedCICDPtr].
func trimmedCICDPtr(value *string) string {
	return cicdrun.TrimmedCICDPtr(value)
}
