// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer //nolint:dirgate // code_import family moves to repodependency/import after packages/correlation (docs/internal/design/reducer-target-tree.md), not under code/

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
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
func decodePackageOwnershipCorrelationDecisions(
	envelopes []facts.Envelope,
) ([]correlation.PackageSourceDecision, []quarantinedFact, error) {
	decisions := make([]correlation.PackageSourceDecision, 0)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != correlation.PackageOwnershipFactKind || envelope.IsTombstone {
			continue
		}
		ownership, err := decodeReducerPackageOwnershipCorrelation(envelope)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
				continue
			}
		}
		packageID := strings.TrimSpace(ownership.PackageID)
		repositoryID := strings.TrimSpace(derefString(ownership.RepositoryID))
		outcome := correlation.PackageSourceOutcome(strings.TrimSpace(derefString(ownership.Outcome)))
		if packageID == "" {
			continue
		}
		decisions = append(decisions, correlation.PackageSourceDecision{
			PackageID:    packageID,
			RepositoryID: repositoryID,
			Outcome:      outcome,
		})
	}
	return decisions, quarantined, nil
}

// decodePackagePublicationCorrelationDecisions rebuilds the exact/derived
// publication decisions from persisted reducer_package_publication_correlation
// facts. As with ownership, only package_id, repository_id, and outcome
// participate in owner resolution. Non-publication fact kinds are ignored.
func decodePackagePublicationCorrelationDecisions(
	envelopes []facts.Envelope,
) ([]correlation.PackagePublicationDecision, []quarantinedFact, error) {
	decisions := make([]correlation.PackagePublicationDecision, 0)
	var quarantined []quarantinedFact
	for _, envelope := range envelopes {
		if envelope.FactKind != correlation.PackagePublicationFactKind || envelope.IsTombstone {
			continue
		}
		publication, err := decodeReducerPackagePublicationCorrelation(envelope)
		if err != nil {
			q, isQuarantine, fatal := partitionDecodeFailures(envelope, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if isQuarantine {
				quarantined = append(quarantined, q)
				continue
			}
		}
		packageID := strings.TrimSpace(publication.PackageID)
		repositoryID := strings.TrimSpace(derefString(publication.RepositoryID))
		outcome := correlation.PackageSourceOutcome(strings.TrimSpace(derefString(publication.Outcome)))
		if packageID == "" {
			continue
		}
		decisions = append(decisions, correlation.PackagePublicationDecision{
			PackageID:    packageID,
			RepositoryID: repositoryID,
			Outcome:      outcome,
		})
	}
	return decisions, quarantined, nil
}
