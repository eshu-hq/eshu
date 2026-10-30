// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// authorizedImpactProtectionEngagedQuery probes the singleton policy row.
// It names no other relation so an undefined-table failure unambiguously
// means the authority layout is absent.
const authorizedImpactProtectionEngagedQuery = `SELECT 1 FROM eshu_security.role_policy_state WHERE singleton LIMIT 1`

// authorizedImpactProtectionEngaged reports whether role policy is
// installed. A missing authority layout (isolated test bootstrap) or an
// empty policy state (pre-onboarding store) both mean protection was never
// onboarded, so the caller keeps the plain static load. Any other error —
// including a connection failure — propagates so the caller surfaces the
// real problem instead of silently downgrading.
func authorizedImpactProtectionEngaged(ctx context.Context, queryer db.Queryer) (bool, error) {
	rows, err := queryer.QueryContext(ctx, authorizedImpactProtectionEngagedQuery)
	if err != nil {
		if isAuthorityLayoutAbsent(err) {
			return false, nil
		}
		return false, fmt.Errorf("read protected policy engagement: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			if isAuthorityLayoutAbsent(err) {
				return false, nil
			}
			return false, fmt.Errorf("read protected policy engagement: %w", err)
		}
		return false, nil
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read protected policy engagement: %w", err)
	}
	return true, nil
}

// isAuthorityLayoutAbsent reports whether err is the authority layout
// itself being absent: either the policy-state relation is missing from an
// otherwise complete schema, or the eshu_security schema never landed
// (isolated test bootstraps skip the database-global authority layout).
// The probe names no other table or schema, so the name checks keep an
// unrelated undefined-table failure from degrading.
func isAuthorityLayoutAbsent(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code == "42P01" {
		return strings.Contains(pgErr.Message, "role_policy_state")
	}
	return pgErr.Code == "3F000" && strings.Contains(pgErr.Message, "eshu_security")
}

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
	expandDisengaged := func() error {
		// Protection was never onboarded here: advisory gating and
		// target publication are not in use, so the static expansion
		// runs over the plain store, byte-identical to the unwired
		// path. It MUST NOT run over the snapshot clone: the clone's
		// static predicate strips vulnerability.cve/affected_package
		// from scope loads and its advisory bridge errors without
		// scan receipt authority, so routing the disengaged expansion
		// through it starves every finding on a scope with no
		// installed authority to filter by. The writer never sees a
		// protected set it cannot authorize.
		staticLoad, err := static(ctx, NewFactStore(l.database), nil)
		if err != nil {
			return fmt.Errorf("expand authorized static impact evidence: %w", err)
		}
		loaded.StaticEnvelopes = cloneAuthorizedImpactEnvelopes(staticLoad.Envelopes)
		loaded.SuppressionEnvelopes = cloneAuthorizedImpactEnvelopes(staticLoad.SuppressionEnvelopes)
		for _, envelope := range staticLoad.Envelopes {
			loaded.StaticNonAdvisoryEvidenceFactIDs = append(loaded.StaticNonAdvisoryEvidenceFactIDs, envelope.FactID)
		}
		loaded.Complete = true
		return nil
	}
	// Probe engagement before opening the authority snapshot. Probing
	// inside would poison the snapshot transaction on scopes whose
	// authority layout is absent: a failed statement aborts the whole
	// Postgres transaction, and the snapshot commit then reports a
	// rollback for a read that was supposed to stay disengaged.
	engaged, err := authorizedImpactProtectionEngaged(ctx, l.database)
	if err != nil {
		return supplychaincore.AuthorizedSupplyChainImpactLoad{}, err
	}
	if !engaged {
		if err := expandDisengaged(); err != nil {
			return supplychaincore.AuthorizedSupplyChainImpactLoad{}, err
		}
		return loaded, nil
	}
	err = NewFactStore(l.database).WithSupplyChainImpactSnapshot(ctx, func(factLoader factload.FactLoader) error {
		snapshotStore, ok := factLoader.(*FactStore)
		if !ok || snapshotStore.database == nil {
			return fmt.Errorf("authorized impact snapshot fact store is unavailable")
		}
		engaged, err := authorizedImpactProtectionEngaged(ctx, snapshotStore.database)
		if err != nil {
			return err
		}
		if !engaged {
			// Defense in depth for authority revoked between the probe
			// above and the snapshot: same plain-store expansion.
			return expandDisengaged()
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
		loaded.ProtectionEngaged = true
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
