// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/hex"
	"fmt"
	"slices"
)

// ProtectedStaticImpactPublicAdvisoryEvidence pins one public advisory fact to
// its registered generation receipt and exact accepted payload hash.
type ProtectedStaticImpactPublicAdvisoryEvidence struct {
	EvidenceFactID      string
	RegistrationID      string
	GenerationReceiptID string
	RegistrationEpoch   int64
	ActivationEpoch     int64
	EvidencePayloadSHA  string
}

// ProtectedStaticImpactFinding carries the full static fact payload and every
// public advisory dependency used to derive it. An empty dependency list is
// an explicit independent finding, not missing authority metadata.
type ProtectedStaticImpactFinding struct {
	FactID           string
	PayloadJSON      string
	AdvisoryEvidence []ProtectedStaticImpactPublicAdvisoryEvidence
}

func protectedStaticImpactEvidenceClasses(write SupplyChainImpactWrite) (map[string]ProtectedStaticImpactPublicAdvisoryEvidence, map[string]struct{}, error) {
	advisory := make(map[string]ProtectedStaticImpactPublicAdvisoryEvidence, len(write.StaticAdvisoryEvidence))
	for _, evidence := range write.StaticAdvisoryEvidence {
		if evidence.EvidenceFactID == "" || evidence.RegistrationID == "" || evidence.GenerationReceiptID == "" ||
			evidence.RegistrationEpoch <= 0 || evidence.ActivationEpoch <= 0 || len(evidence.EvidencePayloadSHA) != 64 {
			return nil, nil, fmt.Errorf("incomplete protected static advisory evidence")
		}
		if _, err := hex.DecodeString(evidence.EvidencePayloadSHA); err != nil {
			return nil, nil, fmt.Errorf("invalid protected static advisory payload hash: %w", err)
		}
		if _, duplicate := advisory[evidence.EvidenceFactID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate protected static advisory evidence %q", evidence.EvidenceFactID)
		}
		advisory[evidence.EvidenceFactID] = evidence
	}
	independent := make(map[string]struct{}, len(write.StaticNonAdvisoryEvidenceFactIDs))
	for _, factID := range write.StaticNonAdvisoryEvidenceFactIDs {
		if factID == "" {
			return nil, nil, fmt.Errorf("empty static independent evidence ID")
		}
		if _, exists := advisory[factID]; exists {
			return nil, nil, fmt.Errorf("static evidence %q has conflicting classes", factID)
		}
		independent[factID] = struct{}{}
	}
	return advisory, independent, nil
}

func protectedStaticImpactFinding(
	factID, payloadJSON string, evidenceFactIDs []string,
	advisory map[string]ProtectedStaticImpactPublicAdvisoryEvidence,
	independent map[string]struct{},
) (ProtectedStaticImpactFinding, error) {
	if factID == "" || payloadJSON == "" {
		return ProtectedStaticImpactFinding{}, fmt.Errorf("static impact finding payload is incomplete")
	}
	finding := ProtectedStaticImpactFinding{FactID: factID, PayloadJSON: payloadJSON}
	seen := make(map[string]struct{}, len(evidenceFactIDs))
	for _, evidenceID := range evidenceFactIDs {
		if evidenceID == "" {
			return ProtectedStaticImpactFinding{}, fmt.Errorf("static impact finding %q has empty evidence", factID)
		}
		if _, duplicate := seen[evidenceID]; duplicate {
			return ProtectedStaticImpactFinding{}, fmt.Errorf("static impact finding %q duplicates evidence %q", factID, evidenceID)
		}
		seen[evidenceID] = struct{}{}
		if dependency, ok := advisory[evidenceID]; ok {
			finding.AdvisoryEvidence = append(finding.AdvisoryEvidence, dependency)
			continue
		}
		if _, ok := independent[evidenceID]; !ok {
			return ProtectedStaticImpactFinding{}, fmt.Errorf("static impact finding %q has unclassified evidence %q", factID, evidenceID)
		}
	}
	slices.SortFunc(finding.AdvisoryEvidence, func(a, b ProtectedStaticImpactPublicAdvisoryEvidence) int {
		if a.EvidenceFactID < b.EvidenceFactID {
			return -1
		}
		if a.EvidenceFactID > b.EvidenceFactID {
			return 1
		}
		return 0
	})
	return finding, nil
}
