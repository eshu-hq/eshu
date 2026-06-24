// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// The cross-scope fact kinds the code-import projection consumes
// (package_registry.package, reducer_package_ownership_correlation,
// reducer_package_publication_correlation) are selected by the Postgres
// ListActivePackageOwnershipFacts query; the decoders below filter the returned
// envelopes by kind so the Go and SQL sides stay in lockstep.

// decodePackageOwnershipCorrelationDecisions rebuilds the exact/derived
// ownership decisions from persisted reducer_package_ownership_correlation
// facts. Only the package_id, repository_id, and outcome fields participate in
// owner resolution, so the decoder reads exactly those; every other persisted
// field is provenance the code-import join does not need. Non-ownership fact
// kinds are ignored.
func decodePackageOwnershipCorrelationDecisions(envelopes []facts.Envelope) []PackageSourceCorrelationDecision {
	decisions := make([]PackageSourceCorrelationDecision, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != packageOwnershipCorrelationFactKind || envelope.IsTombstone {
			continue
		}
		packageID := strings.TrimSpace(payloadStr(envelope.Payload, "package_id"))
		repositoryID := strings.TrimSpace(payloadStr(envelope.Payload, "repository_id"))
		outcome := PackageSourceCorrelationOutcome(strings.TrimSpace(payloadStr(envelope.Payload, "outcome")))
		if packageID == "" {
			continue
		}
		decisions = append(decisions, PackageSourceCorrelationDecision{
			PackageID:    packageID,
			RepositoryID: repositoryID,
			Outcome:      outcome,
		})
	}
	return decisions
}

// decodePackagePublicationCorrelationDecisions rebuilds the exact/derived
// publication decisions from persisted reducer_package_publication_correlation
// facts. As with ownership, only package_id, repository_id, and outcome
// participate in owner resolution. Non-publication fact kinds are ignored.
func decodePackagePublicationCorrelationDecisions(envelopes []facts.Envelope) []PackagePublicationDecision {
	decisions := make([]PackagePublicationDecision, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != packagePublicationCorrelationFactKind || envelope.IsTombstone {
			continue
		}
		packageID := strings.TrimSpace(payloadStr(envelope.Payload, "package_id"))
		repositoryID := strings.TrimSpace(payloadStr(envelope.Payload, "repository_id"))
		outcome := PackageSourceCorrelationOutcome(strings.TrimSpace(payloadStr(envelope.Payload, "outcome")))
		if packageID == "" {
			continue
		}
		decisions = append(decisions, PackagePublicationDecision{
			PackageID:    packageID,
			RepositoryID: repositoryID,
			Outcome:      outcome,
		})
	}
	return decisions
}
