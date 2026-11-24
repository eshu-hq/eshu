// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// fakeEngagedLoader stands in for the protected authorized loader on an
// ENGAGED store. It runs the handler's real static expansion exactly as the
// store does (the public advisory facts come in as the callback's argument, the
// fact loader serves the pass's own scope and the cross-scope Go stage) and
// returns an engaged, complete snapshot with the static set the expansion built.
// The fake seeds no registered public-advisory authority: that proof is a live
// test not yet written, and this one covers the handler's engaged branch.
type fakeEngagedLoader struct {
	public     []facts.Envelope
	factLoader factload.FactLoader
	staticSeen []facts.Envelope
}

func (f *fakeEngagedLoader) ListAuthorizedSupplyChainImpactEvidence(
	ctx context.Context,
	_ reducercontract.Intent,
	static AuthorizedStaticSupplyChainImpactExpansion,
) (AuthorizedSupplyChainImpactLoad, error) {
	loaded, err := static(ctx, f.factLoader, append([]facts.Envelope{}, f.public...))
	if err != nil {
		return AuthorizedSupplyChainImpactLoad{}, err
	}
	f.staticSeen = loaded.Envelopes
	evidence := make([]ProtectedStaticImpactPublicAdvisoryEvidence, 0, len(f.public))
	for _, advisory := range f.public {
		evidence = append(evidence, ProtectedStaticImpactPublicAdvisoryEvidence{
			EvidenceFactID: advisory.FactID, RegistrationID: "registration-9401", GenerationReceiptID: "receipt-9401",
			RegistrationEpoch: 1, ActivationEpoch: 1, EvidencePayloadSHA: "sha-" + advisory.FactID,
		})
	}
	return AuthorizedSupplyChainImpactLoad{
		ProtectionEngaged:      true,
		StaticEnvelopes:        loaded.Envelopes,
		SuppressionEnvelopes:   loaded.SuppressionEnvelopes,
		StaticAdvisoryEvidence: evidence,
		Complete:               loaded.Complete,
	}, nil
}

// runEngaged9401Pass runs the real handler's engaged branch for the orders scope
// with the 9401 advisory as the trusted public set, the legacy module evidence
// and (optionally) the unfinished-scan warning served by the Go stage, and
// returns the CVE-2026-9401 finding the handler wrote.
func runEngaged9401Pass(t *testing.T, withWarning bool) (SupplyChainImpactFinding, SupplyChainImpactWrite) {
	t.Helper()

	advisories, goScan := loadLive9401Split(t)
	var public []facts.Envelope
	for _, envelope := range advisories {
		if envelope.Payload["advisory_id"] == "GO-2026-9401" {
			public = append(public, envelope)
		}
	}
	var goFacts []facts.Envelope
	for _, envelope := range goScan {
		isWarning := envelope.FactKind == facts.ScannerWorkerWarningFactKind
		isLegacyModule := envelope.FactKind == "vulnerability.go_module_evidence" && envelope.Payload["relative_path"] == "legacy/go.mod"
		if isLegacyModule || (isWarning && withWarning) {
			goFacts = append(goFacts, envelope)
		}
	}
	if len(public) != 2 || len(goFacts) == 0 {
		t.Fatalf("fixture split: %d public advisory facts, %d Go scan facts", len(public), len(goFacts))
	}
	// The pass's own scope holds only what an engaged snapshot lets through:
	// the package-consumption join. Advisory kinds come in as the public set.
	loader := &goStageImpactLoader{
		stubSupplyChainImpactFactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: []facts.Envelope{
			packageConsumptionFactWithRange("engaged-9401-consume", "gomod://proxy.golang.org/github.com/acme/legacy-lib",
				"repository:r_ea78e8bb", "v0.1.0"),
		}},
		goScan: goFacts,
	}
	engaged := &fakeEngagedLoader{public: public, factLoader: loader}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer:              newTestImpactFencingTokenIssuer(),
		FactLoader:                      loader,
		AuthorizedEvidenceLoader:        engaged,
		RequireAuthorizedEvidenceLoader: true,
		Writer:                          writer,
	}
	intent := fanOutIntent(1, nil)
	intent.Domain = reducercontract.DomainSupplyChainImpact
	intent.ScopeID = liveOrdersScopeID
	intent.GenerationID = liveOrdersGenerationID
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() over an engaged loader: %v", err)
	}
	for _, finding := range writer.write.Findings {
		if finding.CVEID == "CVE-2026-9401" {
			return finding, writer.write
		}
	}
	t.Fatalf("no CVE-2026-9401 finding written (static set kinds %v, %d findings)", engagedKinds(engaged.staticSeen), len(writer.write.Findings))
	return SupplyChainImpactFinding{}, SupplyChainImpactWrite{}
}

func engagedKinds(envelopes []facts.Envelope) []string {
	kinds := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		kinds = append(kinds, envelope.FactKind)
	}
	return kinds
}

// TestEngagedHandlerReadsAnUnfinishedGoScanAsAnalysisFailed drives the
// handler's engaged branch (a fake authorized loader returning
// ProtectionEngaged, a trusted public advisory for CVE-2026-9401, the legacy
// module evidence and the toolchain_unsupported warning) and expects the written
// finding to read analysis_failed with the scoped gap, as the plain path does.
func TestEngagedHandlerReadsAnUnfinishedGoScanAsAnalysisFailed(t *testing.T) {
	t.Parallel()

	finding, write := runEngaged9401Pass(t, true)
	if !write.ProtectedCompleteSet {
		t.Fatal("write.ProtectedCompleteSet = false: the engaged branch did not run")
	}
	if finding.RuntimeReachability != string(GoVulnReachabilityAnalysisFailed) {
		t.Fatalf("engaged CVE-2026-9401 RuntimeReachability = %q, want analysis_failed", finding.RuntimeReachability)
	}
	if !containsString(finding.MissingEvidence, liveOrdersWarningMsg) {
		t.Fatalf("engaged CVE-2026-9401 MissingEvidence = %#v, want %q", finding.MissingEvidence, liveOrdersWarningMsg)
	}
}

// TestEngagedHandlerWithoutTheWarningReadsModuleOnly is the twin: the same engaged
// set minus the warning reads module_only. It is what makes the verdict test
// non-vacuous: were the engaged branch to drop the warning, the verdict test
// above would read this.
func TestEngagedHandlerWithoutTheWarningReadsModuleOnly(t *testing.T) {
	t.Parallel()

	finding, write := runEngaged9401Pass(t, false)
	if !write.ProtectedCompleteSet {
		t.Fatal("write.ProtectedCompleteSet = false: the engaged branch did not run")
	}
	if finding.RuntimeReachability != string(GoVulnReachabilityModuleOnly) {
		t.Fatalf("engaged CVE-2026-9401 without the warning RuntimeReachability = %q, want module_only", finding.RuntimeReachability)
	}
	if containsString(finding.MissingEvidence, liveOrdersWarningMsg) {
		t.Fatalf("MissingEvidence = %#v carries the warning gap with no warning in the set", finding.MissingEvidence)
	}
}
