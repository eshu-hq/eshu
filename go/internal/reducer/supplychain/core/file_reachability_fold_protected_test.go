// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// foldProtectedLoader mirrors the engaged branch of the Postgres authorized
// loader (static expansion, then each non-advisory static envelope lists its
// evidence fact IDs through AppendEvidenceFactIDs). The Postgres package pins
// the real loader; this runs the handler and the protected writer's classifier.
type foldProtectedLoader struct {
	loader     factload.FactLoader
	advisories []facts.Envelope
}

func (l foldProtectedLoader) ListAuthorizedSupplyChainImpactEvidence(
	ctx context.Context, _ reducercontract.Intent, static AuthorizedStaticSupplyChainImpactExpansion,
) (AuthorizedSupplyChainImpactLoad, error) {
	staticLoad, err := static(ctx, l.loader, l.advisories)
	if err != nil {
		return AuthorizedSupplyChainImpactLoad{}, err
	}
	out := AuthorizedSupplyChainImpactLoad{
		ProtectionEngaged: true, Complete: staticLoad.Complete,
		StaticEnvelopes: staticLoad.Envelopes, SuppressionEnvelopes: staticLoad.SuppressionEnvelopes,
	}
	adv := map[string]struct{}{}
	for _, a := range l.advisories {
		adv[a.FactID] = struct{}{}
		out.StaticAdvisoryEvidence = append(out.StaticAdvisoryEvidence, ProtectedStaticImpactPublicAdvisoryEvidence{
			EvidenceFactID: a.FactID, RegistrationID: "r", GenerationReceiptID: "g", RegistrationEpoch: 1, ActivationEpoch: 1,
			EvidencePayloadSHA: strings.Repeat("a", 64),
		})
	}
	for _, e := range staticLoad.Envelopes {
		if _, ok := adv[e.FactID]; ok {
			continue
		}
		out.StaticNonAdvisoryEvidenceFactIDs = AppendEvidenceFactIDs(out.StaticNonAdvisoryEvidenceFactIDs, e)
	}
	return out, nil
}

func runFoldProtected(t *testing.T, loader factload.FactLoader, advisories []facts.Envelope) SupplyChainImpactWrite {
	t.Helper()
	w := &recordingSupplyChainImpactWriter{}
	h := SupplyChainImpactHandler{
		FencingTokenIssuer:       newTestImpactFencingTokenIssuer(),
		AuthorizedEvidenceLoader: foldProtectedLoader{loader: loader, advisories: advisories}, Writer: w,
	}
	if _, err := h.Handle(context.Background(), reducercontract.Intent{
		IntentID: "i", ScopeID: "vuln-intel://osv/npm/_batch",
		GenerationID: "g", SourceSystem: "vulnerability_intelligence", Domain: reducercontract.DomainSupplyChainImpact,
		Cause: "x",
	}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return w.write
}

func classifyFoldProtected(t *testing.T, write SupplyChainImpactWrite) error {
	adv, indep, err := protectedStaticImpactEvidenceClasses(write)
	if err != nil {
		return err
	}
	if len(write.Findings) == 0 {
		t.Fatal("no findings")
	}
	for i, f := range write.Findings {
		if _, err := protectedStaticImpactFinding("finding-"+string(rune('a'+i)), "{}", f.EvidenceFactIDs, adv, indep); err != nil {
			return err
		}
	}
	return nil
}

// TestProtectedStaticClassificationAcceptsFoldedReachabilityEvidence is the
// #9855 F1 regression: a fold-sourced reachability finding must classify on
// the protected path exactly as the retain-everything finding does.
func TestProtectedStaticClassificationAcceptsFoldedReachabilityEvidence(t *testing.T) {
	all := foldHandlerScopeFacts()
	advisories := all[:2]
	scope := all[2:]
	plain := &stubSupplyChainImpactFactLoader{scopeFacts: scope, active: foldHandlerFiles()}
	pw := runFoldProtected(t, plain, advisories)
	if err := classifyFoldProtected(t, pw); err != nil {
		t.Fatalf("plain classification error = %v", err)
	}
	folding := &foldingStatsLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{scopeFacts: scope, active: foldHandlerFiles()}}
	fw := runFoldProtected(t, folding, advisories)
	if !folding.sawFold {
		t.Fatal("loader never folded: the test would not exercise the digest path")
	}
	if err := classifyFoldProtected(t, fw); err != nil {
		t.Fatalf("folded classification error = %v", err)
	}
}
