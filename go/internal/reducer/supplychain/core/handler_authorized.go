// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// AuthorizedSupplyChainImpactLoad is one loader-authorized impact snapshot.
// StaticEnvelopes are classified source facts; each target has its own scan
// generation and advisory set. The loader checks protected authority before
// applying any limit and owns immutable copies until classification returns.
// ProtectionEngaged reports whether role policy is installed: when no policy
// state exists the protected publication path is not onboarded and the
// handler keeps the plain static load (Complete stays true, asAuthorized
// stays nil), so stores that never installed policy keep draining exactly
// as before protection existed. A registered-but-broken authority still
// fail-closes through the coverage-reason path, never through this flag.
type AuthorizedSupplyChainImpactLoad struct {
	ProtectionEngaged                bool
	StaticEnvelopes                  []facts.Envelope
	Targets                          []AuthorizedScanImpactEvidence
	ScanPublications                 []ScanImpactPublicationWitness
	StaticAdvisoryEvidence           []ProtectedStaticImpactPublicAdvisoryEvidence
	StaticNonAdvisoryEvidenceFactIDs []string
	SuppressionEnvelopes             []facts.Envelope
	Complete                         bool
	CoverageReason                   string
}

// AuthorizedStaticSupplyChainImpactLoad is the existing static expansion
// evaluated inside the protected loader's read-only snapshot. Complete is
// false when any core evidence branch was bounded before it reached a full
// static set; suppression-only truncation remains separately fail-open.
type AuthorizedStaticSupplyChainImpactLoad struct {
	Envelopes            []facts.Envelope
	SuppressionEnvelopes []facts.Envelope
	Complete             bool
	CoverageReason       string
}

// AuthorizedStaticSupplyChainImpactExpansion runs the existing static impact
// expansion over the loader's authority-filtered fact store while its single
// repeatable-read snapshot remains open. A nil publicAdvisories selects the
// full legacy expansion for stores where protection was never onboarded
// (byte-identical to the unwired path, including the os-package advisory
// stage); a non-nil slice, even empty, selects the static-only snapshot
// the engaged path merges with its receipt-backed advisories, so legacy
// target-specific expansion cannot double-count the authorized target load.
// Callers must preserve the distinction: the loader passes literal nil only
// on the disengaged branch and the cloned advisory set on the engaged one.
type AuthorizedStaticSupplyChainImpactExpansion func(
	context.Context, factload.FactLoader, []facts.Envelope,
) (AuthorizedStaticSupplyChainImpactLoad, error)

// AuthorizedSupplyChainImpactEvidenceLoader loads static and target-local
// evidence under one current protected authority snapshot before any limit.
type AuthorizedSupplyChainImpactEvidenceLoader interface {
	ListAuthorizedSupplyChainImpactEvidence(
		context.Context, reducercontract.Intent, AuthorizedStaticSupplyChainImpactExpansion,
	) (AuthorizedSupplyChainImpactLoad, error)
}

const authorizedImpactCoverageIncompleteFailureClass = "authorized_impact_coverage_incomplete"

type authorizedImpactCoverageIncompleteError struct {
	reason string
}

func (e authorizedImpactCoverageIncompleteError) Error() string {
	return fmt.Sprintf("authorized supply chain impact coverage incomplete: %s", e.reason)
}

func (authorizedImpactCoverageIncompleteError) Retryable() bool { return true }

func (authorizedImpactCoverageIncompleteError) FailureClass() string {
	return authorizedImpactCoverageIncompleteFailureClass
}

func boundedAuthorizedImpactCoverageReason(reason string) string {
	switch reason {
	case "source_unavailable", "authority_revoked", "partial_snapshot", "cardinality_limit", "policy_unavailable":
		return reason
	default:
		return "unclassified"
	}
}

func (h SupplyChainImpactHandler) loadAuthorizedSupplyChainImpactEvidence(
	ctx context.Context,
	intent reducercontract.Intent,
) (supplyChainImpactLoadedEvidence, supplyChainImpactTiming, error) {
	started := time.Now()
	var staticLoaded supplyChainImpactLoadedEvidence
	var staticTiming supplyChainImpactTiming
	var staticDuration time.Duration
	staticCalled := false
	staticExpansion := func(ctx context.Context, loader factload.FactLoader, publicAdvisories []facts.Envelope) (AuthorizedStaticSupplyChainImpactLoad, error) {
		if staticCalled {
			return AuthorizedStaticSupplyChainImpactLoad{}, fmt.Errorf("authorized static expansion called more than once")
		}
		staticCalled = true
		staticStarted := time.Now()
		defer func() { staticDuration = time.Since(staticStarted) }()
		staticHandler := h
		staticHandler.AuthorizedEvidenceLoader = nil
		staticHandler.RequireAuthorizedEvidenceLoader = false
		staticHandler.FactLoader = loader
		staticHandler.authorizedStaticOnly = publicAdvisories != nil
		staticHandler.authorizedStaticAdvisories = publicAdvisories
		loaded, timing, err := staticHandler.loadSupplyChainImpactEvidence(ctx, intent)
		if err != nil {
			return AuthorizedStaticSupplyChainImpactLoad{}, err
		}
		staticLoaded, staticTiming = loaded, timing
		return AuthorizedStaticSupplyChainImpactLoad{
			Envelopes: loaded.envelopes, SuppressionEnvelopes: loaded.envelopes,
			Complete: !loaded.truncation.partial(), CoverageReason: "cardinality_limit",
		}, nil
	}
	snapshot, err := h.AuthorizedEvidenceLoader.ListAuthorizedSupplyChainImpactEvidence(ctx, intent, staticExpansion)
	timing := staticTiming
	timing.loadAuthorizedTargetsDuration = time.Since(started) - staticDuration
	if err != nil {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("load authorized supply chain impact evidence: %w", factload.ClassifyFactLoadError(err))
	}
	if !staticCalled {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("authorized impact loader skipped static expansion")
	}
	if !snapshot.Complete {
		reason := boundedAuthorizedImpactCoverageReason(snapshot.CoverageReason)
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, "authorized impact coverage incomplete",
				slog.String("failure_class", authorizedImpactCoverageIncompleteFailureClass),
				slog.String("scope_id", intent.ScopeID),
				slog.String("coverage_reason", reason))
		}
		return supplyChainImpactLoadedEvidence{}, timing, authorizedImpactCoverageIncompleteError{reason: reason}
	}
	if !snapshot.ProtectionEngaged {
		// Protection was never onboarded: the nil advisory set above
		// selected the full legacy expansion, so the plain load already
		// carries the os-package advisory evidence the unwired path
		// carries, and the build derives the same findings. The writer
		// never sees a protected set it cannot authorize.
		return staticLoaded, timing, nil
	}
	if len(snapshot.Targets) != len(snapshot.ScanPublications) {
		return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("authorized scan targets and publication witnesses differ")
	}
	publications := make(map[string]ScanImpactPublicationWitness, len(snapshot.ScanPublications))
	for _, witness := range snapshot.ScanPublications {
		if _, duplicate := publications[witness.ScanTargetID]; duplicate {
			return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("duplicate scan publication witness for %q", witness.ScanTargetID)
		}
		publications[witness.ScanTargetID] = witness
	}
	for _, target := range snapshot.Targets {
		witness, exists := publications[target.ScanTargetID]
		if !exists || witness.ScanGenerationID != target.ScanGenerationID {
			return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("authorized scan target %q lacks current publication witness", target.ScanTargetID)
		}
	}
	for _, envelope := range snapshot.StaticEnvelopes {
		if strings.HasPrefix(envelope.ScopeID, "image://derived/") {
			return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("derived fact %q entered static authorized evidence", envelope.FactID)
		}
	}
	for _, envelope := range snapshot.SuppressionEnvelopes {
		if strings.HasPrefix(envelope.ScopeID, "image://derived/") {
			return supplyChainImpactLoadedEvidence{}, timing, fmt.Errorf("derived suppression %q entered static authorized evidence", envelope.FactID)
		}
	}
	derivedFacts := 0
	advisoryFacts := 0
	analysisFacts := 0
	for _, target := range snapshot.Targets {
		derivedFacts += len(target.OSPackages) + len(target.Analyses) + len(target.Advisories)
		advisoryFacts += len(target.Advisories)
		analysisFacts += len(target.Analyses)
	}
	staticLoaded.envelopes = snapshot.StaticEnvelopes
	staticLoaded.asAuthorized = &snapshot
	staticLoaded.expansionEnvelopes += derivedFacts
	staticLoaded.osPackageAdvisoryFacts += advisoryFacts
	staticLoaded.scannerAnalysisScopeFacts += analysisFacts
	return staticLoaded, timing, nil
}

func (loaded supplyChainImpactLoadedEvidence) evidenceEnvelopeCount() int {
	if loaded.asAuthorized == nil {
		return len(loaded.envelopes)
	}
	return len(loaded.envelopes) + loaded.expansionEnvelopes
}

func buildAuthorizedSupplyChainImpactFindings(
	loaded supplyChainImpactLoadedEvidence,
) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, int, error) {
	staticFindings, quarantined, negatedProductsExcluded, err := buildSupplyChainImpactFindingsAndExclusions(loaded.envelopes)
	if err != nil || loaded.asAuthorized == nil {
		return staticFindings, quarantined, negatedProductsExcluded, err
	}
	findings, targetQuarantined, err := buildAuthorizedScanImpactFindingsWithQuarantine(
		staticFindings, loaded.asAuthorized.Targets,
	)
	return findings, append(quarantined, targetQuarantined...), negatedProductsExcluded, err
}
