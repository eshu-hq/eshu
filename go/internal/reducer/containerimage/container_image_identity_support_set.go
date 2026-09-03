// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

type containerImageIdentitySupportSet struct {
	SetID               []byte
	ContentHash         []byte
	Supports            []containerImageIdentitySupport
	CurrentSupportCount int
}

// ContainerImageIdentityPriorSupport is one prior authoritative support row
// eligible for carry-forward while collector completeness holds retirement.
type ContainerImageIdentityPriorSupport struct {
	Digest                       string
	ImageRef                     string
	RepositoryID                 string
	Outcome                      string
	IdentityStrength             string
	SourceRevision               string
	SourceRevisionProvenance     string
	Reason                       string
	CanonicalWrites              int
	SourceRepositoryIDs          []string
	BuildProvenanceRepositoryIDs []string
	BaseImageForRepositoryIDs    []string
	WorkloadIDs                  []string
	ServiceIDs                   []string
	SourceLayers                 []string
	EvidenceFactIDs              []string
	MissingEvidence              []string
}

type containerImageIdentitySupport struct {
	Digest                       string   `json:"digest"`
	SupportID                    string   `json:"support_id"`
	ImageRef                     string   `json:"image_ref"`
	RepositoryID                 string   `json:"repository_id"`
	Outcome                      string   `json:"outcome"`
	IdentityStrength             string   `json:"identity_strength"`
	SourceRevision               string   `json:"source_revision"`
	SourceRevisionProvenance     string   `json:"source_revision_provenance"`
	Reason                       string   `json:"reason"`
	CanonicalWrites              int      `json:"canonical_writes"`
	SourceRepositoryIDs          []string `json:"source_repository_ids"`
	BuildProvenanceRepositoryIDs []string `json:"build_provenance_repository_ids"`
	BaseImageForRepositoryIDs    []string `json:"base_image_for_repository_ids"`
	WorkloadIDs                  []string `json:"workload_ids"`
	ServiceIDs                   []string `json:"service_ids"`
	SourceLayers                 []string `json:"source_layers"`
	EvidenceFactIDs              []string `json:"evidence_fact_ids"`
	MissingEvidence              []string `json:"missing_evidence"`
}

func buildContainerImageIdentitySupportSet(
	write ContainerImageIdentityWrite,
	prior []ContainerImageIdentityPriorSupport,
) (containerImageIdentitySupportSet, error) {
	decisions := containerImageIdentityCanonicalDecisions(write.Decisions)
	supports := make([]containerImageIdentitySupport, 0, len(decisions)+len(prior))
	seen := make(map[string]struct{}, len(decisions)+len(prior))
	heldBaseRepositories := make(map[string][]string, len(write.HeldDecisions))
	for _, held := range write.HeldDecisions {
		imageRef := strings.TrimSpace(held.ImageRef)
		if imageRef == "" {
			continue
		}
		heldBaseRepositories[imageRef] = payloadcore.UniqueSortedStrings(append(
			heldBaseRepositories[imageRef],
			held.BaseImageForRepositoryIDs...,
		))
	}
	currentSupportCount := 0
	for _, decision := range decisions {
		row, err := containerImageIdentitySupportFromDecision(decision)
		if err != nil {
			return containerImageIdentitySupportSet{}, err
		}
		if _, ok := seen[row.SupportID]; ok {
			continue
		}
		seen[row.SupportID] = struct{}{}
		supports = append(supports, row)
		currentSupportCount++
	}
	for _, retained := range prior {
		retained.BaseImageForRepositoryIDs = append(
			retained.BaseImageForRepositoryIDs,
			heldBaseRepositories[strings.TrimSpace(retained.ImageRef)]...,
		)
		row, err := containerImageIdentitySupportFromPrior(retained)
		if err != nil {
			return containerImageIdentitySupportSet{}, err
		}
		if _, ok := seen[row.SupportID]; ok {
			continue
		}
		seen[row.SupportID] = struct{}{}
		supports = append(supports, row)
	}
	sort.Slice(supports, func(i, j int) bool {
		return containerImageIdentitySupportSortKey(supports[i]) <
			containerImageIdentitySupportSortKey(supports[j])
	})
	document, err := json.Marshal(supports)
	if err != nil {
		return containerImageIdentitySupportSet{}, fmt.Errorf("marshal container image identity support set: %w", err)
	}
	contentHash := sha256.Sum256(document)
	setHash := sha256.Sum256([]byte(strings.TrimSpace(write.ScopeID) + "\x00" + hex.EncodeToString(contentHash[:])))
	return containerImageIdentitySupportSet{
		SetID:               setHash[:],
		ContentHash:         contentHash[:],
		Supports:            supports,
		CurrentSupportCount: currentSupportCount,
	}, nil
}

func containerImageIdentitySupportFromDecision(
	decision ContainerImageIdentityDecision,
) (containerImageIdentitySupport, error) {
	row := containerImageIdentitySupport{
		Digest:                       strings.TrimSpace(decision.Digest),
		ImageRef:                     strings.TrimSpace(decision.ImageRef),
		RepositoryID:                 strings.TrimSpace(decision.RepositoryID),
		Outcome:                      string(decision.Outcome),
		IdentityStrength:             strings.TrimSpace(decision.IdentityStrength),
		SourceRevision:               strings.TrimSpace(decision.SourceRevision),
		SourceRevisionProvenance:     strings.TrimSpace(decision.SourceRevisionProvenance),
		Reason:                       strings.TrimSpace(decision.Reason),
		CanonicalWrites:              decision.CanonicalWrites,
		SourceRepositoryIDs:          payloadcore.UniqueSortedStrings(decision.SourceRepositoryIDs),
		BuildProvenanceRepositoryIDs: payloadcore.UniqueSortedStrings(decision.BuildProvenanceRepositoryIDs),
		BaseImageForRepositoryIDs:    payloadcore.UniqueSortedStrings(decision.BaseImageForRepositoryIDs),
		WorkloadIDs:                  payloadcore.UniqueSortedStrings(decision.WorkloadIDs),
		ServiceIDs:                   payloadcore.UniqueSortedStrings(decision.ServiceIDs),
		SourceLayers: []string{
			string(truth.LayerObservedResource),
			string(truth.LayerSourceDeclaration),
		},
		EvidenceFactIDs: payloadcore.UniqueSortedStrings(decision.EvidenceFactIDs),
		MissingEvidence: []string{},
	}
	return finalizeContainerImageIdentitySupport(row)
}

func containerImageIdentitySupportFromPrior(
	prior ContainerImageIdentityPriorSupport,
) (containerImageIdentitySupport, error) {
	return finalizeContainerImageIdentitySupport(containerImageIdentitySupport{
		Digest:                       prior.Digest,
		ImageRef:                     prior.ImageRef,
		RepositoryID:                 prior.RepositoryID,
		Outcome:                      prior.Outcome,
		IdentityStrength:             prior.IdentityStrength,
		SourceRevision:               prior.SourceRevision,
		SourceRevisionProvenance:     prior.SourceRevisionProvenance,
		Reason:                       prior.Reason,
		CanonicalWrites:              prior.CanonicalWrites,
		SourceRepositoryIDs:          prior.SourceRepositoryIDs,
		BuildProvenanceRepositoryIDs: prior.BuildProvenanceRepositoryIDs,
		BaseImageForRepositoryIDs:    prior.BaseImageForRepositoryIDs,
		WorkloadIDs:                  prior.WorkloadIDs,
		ServiceIDs:                   prior.ServiceIDs,
		SourceLayers:                 prior.SourceLayers,
		EvidenceFactIDs:              prior.EvidenceFactIDs,
		MissingEvidence:              prior.MissingEvidence,
	})
}

func finalizeContainerImageIdentitySupport(
	row containerImageIdentitySupport,
) (containerImageIdentitySupport, error) {
	row.SupportID = ""
	row.Digest = strings.TrimSpace(row.Digest)
	row.ImageRef = strings.TrimSpace(row.ImageRef)
	row.RepositoryID = strings.TrimSpace(row.RepositoryID)
	row.Outcome = strings.TrimSpace(row.Outcome)
	row.IdentityStrength = strings.TrimSpace(row.IdentityStrength)
	row.SourceRevision = strings.TrimSpace(row.SourceRevision)
	row.SourceRevisionProvenance = strings.TrimSpace(row.SourceRevisionProvenance)
	row.Reason = strings.TrimSpace(row.Reason)
	row.SourceRepositoryIDs = payloadcore.UniqueSortedStrings(row.SourceRepositoryIDs)
	row.BuildProvenanceRepositoryIDs = payloadcore.UniqueSortedStrings(row.BuildProvenanceRepositoryIDs)
	row.BaseImageForRepositoryIDs = payloadcore.UniqueSortedStrings(row.BaseImageForRepositoryIDs)
	row.WorkloadIDs = payloadcore.UniqueSortedStrings(row.WorkloadIDs)
	row.ServiceIDs = payloadcore.UniqueSortedStrings(row.ServiceIDs)
	row.SourceLayers = payloadcore.UniqueSortedStrings(row.SourceLayers)
	row.EvidenceFactIDs = payloadcore.UniqueSortedStrings(row.EvidenceFactIDs)
	row.MissingEvidence = payloadcore.UniqueSortedStrings(row.MissingEvidence)
	if row.Digest == "" {
		return containerImageIdentitySupport{}, fmt.Errorf("container image identity support digest is required")
	}
	identity, err := json.Marshal(row)
	if err != nil {
		return containerImageIdentitySupport{}, fmt.Errorf("marshal container image identity support: %w", err)
	}
	supportHash := sha256.Sum256(identity)
	row.SupportID = hex.EncodeToString(supportHash[:])
	return row, nil
}

func containerImageIdentitySupportSortKey(row containerImageIdentitySupport) string {
	return strings.Join([]string{
		row.Digest,
		row.ImageRef,
		row.RepositoryID,
		row.Outcome,
		row.SupportID,
	}, "\x00")
}
