// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	containerImageIdentityWarningConfigBlobUnavailable = "config_blob_unavailable"
	containerImageIdentityWarningTagListTruncated      = "tag_list_truncated"
	containerImageIdentityWarningMissingManifestDigest = "missing_manifest_digest"

	containerImageIdentityRetireHoldConfigBlobUnavailable = "config_blob_unavailable"
	containerImageIdentityRetireHoldTagListTruncated      = "tag_list_truncated"
	containerImageIdentityRetireHoldMissingManifestDigest = "missing_manifest_digest"
)

type containerImageIdentityRetirementPlan struct {
	Tombstones    []ContainerImageIdentityDecision
	LegacyFactIDs []string
	HeldByReason  map[string]int
}

type containerImageIdentityRepositoryDigest struct {
	repositoryID string
	digest       string
}

type containerImageIdentityRepositoryConfig struct {
	repositoryID string
	configDigest string
}

func containerImageIdentityRetireSubSignals(heldByReason map[string]int) map[string]float64 {
	signals := make(map[string]float64, len(heldByReason))
	for reason, count := range heldByReason {
		if count <= 0 {
			continue
		}
		signals["retire_held_"+reason] = float64(count)
	}
	return signals
}

func planContainerImageIdentityRetirement(
	write ContainerImageIdentityWrite,
	evidence []facts.Envelope,
	warnings []facts.Envelope,
) (containerImageIdentityRetirementPlan, error) {
	configManifestDigests := containerImageIdentityConfigManifestDigests(evidence)
	heldManifestDigests := make(map[containerImageIdentityRepositoryDigest]struct{})
	truncatedRepositories := make(map[string]struct{})
	missingManifestRepositories := make(map[string]struct{})

	for _, envelope := range warnings {
		if envelope.FactKind != facts.OCIRegistryWarningFactKind {
			continue
		}
		warning, err := decodeOCIRegistryWarning(envelope)
		if err != nil {
			return containerImageIdentityRetirementPlan{}, fmt.Errorf(
				"decode active OCI registry retirement warning %q: %w",
				envelope.FactID,
				err,
			)
		}
		repositoryID := strings.TrimSpace(derefString(warning.RepositoryID))
		switch strings.TrimSpace(warning.WarningCode) {
		case containerImageIdentityWarningConfigBlobUnavailable:
			configDigest := strings.TrimSpace(derefString(warning.Digest))
			for _, manifest := range configManifestDigests[containerImageIdentityRepositoryConfig{
				repositoryID: repositoryID,
				configDigest: configDigest,
			}] {
				heldManifestDigests[manifest] = struct{}{}
			}
		case containerImageIdentityWarningTagListTruncated:
			if repositoryID != "" {
				truncatedRepositories[repositoryID] = struct{}{}
			}
		case containerImageIdentityWarningMissingManifestDigest:
			if repositoryID != "" {
				missingManifestRepositories[repositoryID] = struct{}{}
			}
		}
	}

	legacyFactIDs := make(map[string]struct{}, len(write.Decisions))
	tombstones := make([]ContainerImageIdentityDecision, 0, len(write.Decisions))
	heldByReason := make(map[string]int)
	for _, decision := range write.Decisions {
		if reason := containerImageIdentityRetireHoldReason(
			decision,
			heldManifestDigests,
			truncatedRepositories,
			missingManifestRepositories,
		); reason != "" {
			heldByReason[reason]++
			continue
		}
		if decision.CanonicalWrites <= 0 {
			tombstones = append(tombstones, decision)
		}
		if outcome, ok := containerImageIdentityLegacyOutcome(decision); ok {
			legacy := containerImageIdentityDecisionWithOutcome(decision, outcome)
			legacyFactIDs[legacyContainerImageIdentityFactID(write, legacy)] = struct{}{}
		}
	}

	plan := containerImageIdentityRetirementPlan{
		Tombstones:    tombstones,
		LegacyFactIDs: make([]string, 0, len(legacyFactIDs)),
		HeldByReason:  heldByReason,
	}
	for factID := range legacyFactIDs {
		plan.LegacyFactIDs = append(plan.LegacyFactIDs, factID)
	}
	sort.Slice(plan.Tombstones, func(i, j int) bool {
		return strings.TrimSpace(plan.Tombstones[i].ImageRef) <
			strings.TrimSpace(plan.Tombstones[j].ImageRef)
	})
	sort.Strings(plan.LegacyFactIDs)
	return plan, nil
}

func containerImageIdentityLegacyOutcome(
	decision ContainerImageIdentityDecision,
) (ContainerImageIdentityOutcome, bool) {
	parsed, ok := parseContainerImageRef(decision.ImageRef)
	if !ok {
		return "", false
	}
	if parsed.digest != "" {
		return ContainerImageIdentityExactDigest, true
	}
	if parsed.tag != "" {
		return ContainerImageIdentityTagResolved, true
	}
	return "", false
}

func containerImageIdentityConfigManifestDigests(
	evidence []facts.Envelope,
) map[containerImageIdentityRepositoryConfig][]containerImageIdentityRepositoryDigest {
	out := make(map[containerImageIdentityRepositoryConfig][]containerImageIdentityRepositoryDigest)
	for _, envelope := range evidence {
		if envelope.FactKind != facts.OCIImageManifestFactKind {
			continue
		}
		manifest, ok := decodeOCIImageManifestForIndex(envelope)
		if !ok || manifest.Config == nil {
			continue
		}
		repositoryID := strings.TrimSpace(manifest.RepositoryID)
		manifestDigest := strings.TrimSpace(manifest.Digest)
		configDigest := strings.TrimSpace(derefString(manifest.Config.Digest))
		if repositoryID == "" || manifestDigest == "" || configDigest == "" {
			continue
		}
		key := containerImageIdentityRepositoryConfig{
			repositoryID: repositoryID,
			configDigest: configDigest,
		}
		out[key] = append(out[key], containerImageIdentityRepositoryDigest{
			repositoryID: repositoryID,
			digest:       manifestDigest,
		})
	}
	return out
}

func containerImageIdentityRetireHoldReason(
	decision ContainerImageIdentityDecision,
	heldManifestDigests map[containerImageIdentityRepositoryDigest]struct{},
	truncatedRepositories map[string]struct{},
	missingManifestRepositories map[string]struct{},
) string {
	repositoryID := strings.TrimSpace(decision.RepositoryID)
	if repositoryID == "" {
		if parsed, ok := parseContainerImageRef(decision.ImageRef); ok {
			repositoryID = repositoryIDFromKey(parsed.repositoryKey)
		}
	}
	if _, held := heldManifestDigests[containerImageIdentityRepositoryDigest{
		repositoryID: repositoryID,
		digest:       strings.TrimSpace(decision.Digest),
	}]; held {
		return containerImageIdentityRetireHoldConfigBlobUnavailable
	}

	parsed, parsedOK := parseContainerImageRef(decision.ImageRef)
	if _, held := missingManifestRepositories[repositoryID]; held {
		return containerImageIdentityRetireHoldMissingManifestDigest
	}
	if parsedOK && parsed.digest == "" {
		if _, held := truncatedRepositories[repositoryID]; held {
			return containerImageIdentityRetireHoldTagListTruncated
		}
	}
	return ""
}

func containerImageIdentityDecisionWithOutcome(
	decision ContainerImageIdentityDecision,
	outcome ContainerImageIdentityOutcome,
) ContainerImageIdentityDecision {
	candidate := decision
	candidate.CanonicalWrites = 1
	candidate.Outcome = outcome
	return candidate
}
