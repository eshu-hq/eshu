// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// AuthorizedSupplyChainImpactEvidenceLoader reads static and target-local
// evidence in one authority-filtered read-only repeatable-read snapshot.
type AuthorizedSupplyChainImpactEvidenceLoader struct {
	database db.ExecQueryer
}

// NewAuthorizedSupplyChainImpactEvidenceLoader constructs the protected
// impact loader. A database without a repeatable-read snapshot capability
// cannot produce a complete result and is rejected at wiring time.
func NewAuthorizedSupplyChainImpactEvidenceLoader(database db.ExecQueryer) supplychaincore.AuthorizedSupplyChainImpactEvidenceLoader {
	if database == nil {
		return nil
	}
	if _, ok := database.(db.ReadOnlyRepeatableReadBeginner); !ok {
		return nil
	}
	return AuthorizedSupplyChainImpactEvidenceLoader{database: database}
}

// ListAuthorizedSupplyChainImpactEvidence expands static input and target
// evidence under one physical snapshot. A missing authority witness reports
// incomplete coverage and never permits a complete-set write or retraction.
func (l AuthorizedSupplyChainImpactEvidenceLoader) ListAuthorizedSupplyChainImpactEvidence(
	ctx context.Context,
	intent reducercontract.Intent,
	static supplychaincore.AuthorizedStaticSupplyChainImpactExpansion,
) (supplychaincore.AuthorizedSupplyChainImpactLoad, error) {
	if l.database == nil || static == nil {
		return supplychaincore.AuthorizedSupplyChainImpactLoad{}, fmt.Errorf("authorized impact snapshot dependencies are required")
	}
	if intent.ScopeID == "" || intent.GenerationID == "" || intent.Domain != reducercontract.DomainSupplyChainImpact {
		return supplychaincore.AuthorizedSupplyChainImpactLoad{}, fmt.Errorf("authorized impact intent boundary is invalid")
	}
	var loaded supplychaincore.AuthorizedSupplyChainImpactLoad
	err := NewFactStore(l.database).WithSupplyChainImpactSnapshot(ctx, func(factLoader factload.FactLoader) error {
		snapshotStore, ok := factLoader.(*FactStore)
		if !ok || snapshotStore.database == nil {
			return fmt.Errorf("authorized impact snapshot fact store is unavailable")
		}
		public, advisoryReason, err := loadCurrentAuthorizedPublicAdvisories(ctx, snapshotStore.database)
		if err != nil {
			return err
		}
		staticLoad, err := static(ctx, factLoader, cloneAuthorizedImpactEnvelopes(public.facts))
		if err != nil {
			return fmt.Errorf("expand authorized static impact evidence: %w", err)
		}
		loaded.StaticEnvelopes = cloneAuthorizedImpactEnvelopes(staticLoad.Envelopes)
		loaded.SuppressionEnvelopes = cloneAuthorizedImpactEnvelopes(staticLoad.SuppressionEnvelopes)
		loaded.StaticAdvisoryEvidence = append(loaded.StaticAdvisoryEvidence, public.staticEvidence...)
		advisoryFacts := make(map[string]struct{}, len(public.staticEvidence))
		for _, evidence := range public.staticEvidence {
			advisoryFacts[evidence.EvidenceFactID] = struct{}{}
		}
		for _, envelope := range staticLoad.Envelopes {
			if _, trustedPublic := advisoryFacts[envelope.FactID]; trustedPublic {
				continue
			}
			if authorizedImpactAdvisoryKind(envelope.FactKind) || targetOnlyImpactFactKind(envelope.FactKind) {
				loaded.CoverageReason = "policy_unavailable"
				return nil
			}
			loaded.StaticNonAdvisoryEvidenceFactIDs = append(loaded.StaticNonAdvisoryEvidenceFactIDs, envelope.FactID)
		}
		if !staticLoad.Complete {
			loaded.CoverageReason = boundedAuthorizedImpactLoaderReason(staticLoad.CoverageReason)
			return nil
		}
		if advisoryReason != "" {
			loaded.CoverageReason = boundedAuthorizedImpactLoaderReason(advisoryReason)
			return nil
		}
		targets, publications, reason, err := loadCurrentAuthorizedImpactTargets(ctx, snapshotStore.database, intent, public)
		if err != nil {
			return err
		}
		loaded.Targets, loaded.ScanPublications = targets, publications
		loaded.CoverageReason = reason
		loaded.Complete = reason == ""
		return nil
	})
	if err != nil {
		return supplychaincore.AuthorizedSupplyChainImpactLoad{}, err
	}
	return loaded, nil
}

func cloneAuthorizedImpactEnvelopes(input []facts.Envelope) []facts.Envelope {
	cloned := make([]facts.Envelope, len(input))
	for i := range input {
		cloned[i] = input[i].Clone()
	}
	return cloned
}

func boundedAuthorizedImpactLoaderReason(reason string) string {
	switch reason {
	case "source_unavailable", "authority_revoked", "partial_snapshot", "cardinality_limit", "policy_unavailable":
		return reason
	default:
		return "policy_unavailable"
	}
}

func authorizedImpactAdvisoryKind(kind string) bool {
	switch kind {
	case chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityAffectedProductFactKind, chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind:
		return true
	default:
		return false
	}
}

func targetOnlyImpactFactKind(kind string) bool {
	switch kind {
	case chain.VulnerabilityOSPackageFactKind, chain.VulnerabilityWarningFactKind,
		facts.ScannerWorkerAnalysisFactKind, facts.ScannerWorkerWarningFactKind:
		return true
	default:
		return false
	}
}
