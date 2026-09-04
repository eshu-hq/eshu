// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"fmt"
	"sort"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

const (
	containerImageIdentityRetireHoldConfigBlobUnavailable = "config_blob_unavailable"
	containerImageIdentityRetireHoldTagListTruncated      = "tag_list_truncated"
	containerImageIdentityRetireHoldMissingManifestDigest = "missing_manifest_digest"
)

type containerImageIdentityWarningDisposition uint8

const (
	containerImageIdentityWarningInvalid containerImageIdentityWarningDisposition = iota
	containerImageIdentityWarningNoRetirementHold
	containerImageIdentityWarningHoldConfigBlob
	containerImageIdentityWarningHoldTagList
	containerImageIdentityWarningHoldMissingManifest
)

type containerImageIdentityRetirementPlan struct {
	Tombstones    []ContainerImageIdentityDecision
	LegacyFactIDs []string
	HeldDecisions []ContainerImageIdentityDecision
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

func containerImageIdentityRetirementNeedsWarnings(
	decisions []ContainerImageIdentityDecision,
) bool {
	for _, decision := range decisions {
		if decision.CanonicalWrites <= 0 {
			return true
		}
	}
	return false
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
	unmappedConfigWarningRepositories := make(map[string]struct{})
	truncatedRepositories := make(map[string]struct{})
	missingManifestRepositories := make(map[string]struct{})

	for _, envelope := range warnings {
		if envelope.FactKind != facts.OCIRegistryWarningFactKind {
			continue
		}
		warning, err := schemadecode.DecodeOCIRegistryWarning(envelope)
		if err != nil {
			return containerImageIdentityRetirementPlan{}, fmt.Errorf(
				"decode active OCI registry retirement warning %q: %w",
				envelope.FactID,
				err,
			)
		}
		warningCode := strings.TrimSpace(warning.WarningCode)
		disposition, err := classifyContainerImageIdentityWarning(warningCode)
		if err != nil {
			return containerImageIdentityRetirementPlan{}, fmt.Errorf(
				"active OCI registry retirement warning %q: %w",
				envelope.FactID,
				err,
			)
		}
		switch disposition {
		case containerImageIdentityWarningInvalid:
			return containerImageIdentityRetirementPlan{}, fmt.Errorf(
				"active OCI registry retirement warning %q classified invalid",
				envelope.FactID,
			)
		case containerImageIdentityWarningNoRetirementHold:
			continue
		case containerImageIdentityWarningHoldConfigBlob:
			repositoryID, err := containerImageIdentityRetirementWarningRepositoryID(
				warningCode,
				payloadcore.DerefString(warning.RepositoryID),
			)
			if err != nil {
				return containerImageIdentityRetirementPlan{}, err
			}
			configDigest := strings.TrimSpace(payloadcore.DerefString(warning.Digest))
			if !validContainerImageIdentityRetirementDigest(configDigest) {
				return containerImageIdentityRetirementPlan{}, fmt.Errorf(
					"active OCI registry retirement warning %q requires a valid digest",
					warningCode,
				)
			}
			manifests := configManifestDigests[containerImageIdentityRepositoryConfig{
				repositoryID: repositoryID,
				configDigest: configDigest,
			}]
			if len(manifests) == 0 {
				unmappedConfigWarningRepositories[repositoryID] = struct{}{}
				continue
			}
			for _, manifest := range manifests {
				heldManifestDigests[manifest] = struct{}{}
			}
		case containerImageIdentityWarningHoldTagList:
			repositoryID, err := containerImageIdentityRetirementWarningRepositoryID(
				warningCode,
				payloadcore.DerefString(warning.RepositoryID),
			)
			if err != nil {
				return containerImageIdentityRetirementPlan{}, err
			}
			truncatedRepositories[repositoryID] = struct{}{}
		case containerImageIdentityWarningHoldMissingManifest:
			repositoryID, err := containerImageIdentityRetirementWarningRepositoryID(
				warningCode,
				payloadcore.DerefString(warning.RepositoryID),
			)
			if err != nil {
				return containerImageIdentityRetirementPlan{}, err
			}
			missingManifestRepositories[repositoryID] = struct{}{}
		}
	}

	legacyFactIDs := make(map[string]struct{}, len(write.Decisions))
	tombstones := make([]ContainerImageIdentityDecision, 0, len(write.Decisions))
	heldDecisions := make([]ContainerImageIdentityDecision, 0)
	heldByReason := make(map[string]int)
	for _, decision := range write.Decisions {
		if reason := containerImageIdentityRetireHoldReason(
			decision,
			heldManifestDigests,
			unmappedConfigWarningRepositories,
			truncatedRepositories,
			missingManifestRepositories,
		); reason != "" {
			heldByReason[reason]++
			heldDecisions = append(heldDecisions, decision)
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
		HeldDecisions: heldDecisions,
		HeldByReason:  heldByReason,
	}
	for factID := range legacyFactIDs {
		plan.LegacyFactIDs = append(plan.LegacyFactIDs, factID)
	}
	sort.Slice(plan.Tombstones, func(i, j int) bool {
		return strings.TrimSpace(plan.Tombstones[i].ImageRef) <
			strings.TrimSpace(plan.Tombstones[j].ImageRef)
	})
	sort.Slice(plan.HeldDecisions, func(i, j int) bool {
		return containerImageIdentityDecisionSortKey(plan.HeldDecisions[i]) <
			containerImageIdentityDecisionSortKey(plan.HeldDecisions[j])
	})
	sort.Strings(plan.LegacyFactIDs)
	return plan, nil
}

func classifyContainerImageIdentityWarning(
	warningCode string,
) (containerImageIdentityWarningDisposition, error) {
	switch strings.TrimSpace(warningCode) {
	case ociregistryv1.WarningCodeUnsupportedReferrersAPI,
		ociregistryv1.WarningCodeComputedManifestDigest,
		ociregistryv1.WarningCodeConfigBlobOversized:
		return containerImageIdentityWarningNoRetirementHold, nil
	case ociregistryv1.WarningCodeConfigBlobUnavailable:
		return containerImageIdentityWarningHoldConfigBlob, nil
	case ociregistryv1.WarningCodeTagListTruncated:
		return containerImageIdentityWarningHoldTagList, nil
	case ociregistryv1.WarningCodeMissingManifestDigest:
		return containerImageIdentityWarningHoldMissingManifest, nil
	default:
		return containerImageIdentityWarningInvalid, fmt.Errorf(
			"unrecognized OCI registry warning code %q",
			strings.TrimSpace(warningCode),
		)
	}
}

func containerImageIdentityRetirementWarningRepositoryID(
	warningCode string,
	raw string,
) (string, error) {
	repositoryID := strings.TrimSpace(raw)
	repositoryKey := strings.TrimPrefix(repositoryID, "oci-registry://")
	registry, repository, ok := strings.Cut(repositoryKey, "/")
	if repositoryID == "" ||
		repositoryKey == repositoryID ||
		!ok ||
		strings.TrimSpace(registry) == "" ||
		strings.Trim(strings.TrimSpace(repository), "/") == "" ||
		strings.ContainsAny(repositoryKey, " \t\r\n") {
		return "", fmt.Errorf(
			"active OCI registry retirement warning %q requires a valid repository_id",
			warningCode,
		)
	}
	return repositoryID, nil
}

func validContainerImageIdentityRetirementDigest(raw string) bool {
	const (
		sha256Prefix   = "sha256:"
		sha256HexChars = 64
	)

	digest := strings.TrimSpace(raw)
	if len(digest) != len(sha256Prefix)+sha256HexChars || !strings.HasPrefix(digest, sha256Prefix) {
		return false
	}
	for _, char := range digest[len(sha256Prefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func containerImageIdentityLegacyOutcome(
	decision ContainerImageIdentityDecision,
) (reducercontract.ContainerImageIdentityOutcome, bool) {
	parsed, ok := ParseContainerImageRef(decision.ImageRef)
	if !ok {
		return "", false
	}
	if parsed.Digest != "" {
		return reducercontract.ContainerImageIdentityExactDigest, true
	}
	if parsed.Tag != "" {
		return reducercontract.ContainerImageIdentityTagResolved, true
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
		manifest, ok := schemadecode.DecodeOCIImageManifestForIndex(envelope)
		if !ok || manifest.Config == nil {
			continue
		}
		repositoryID := strings.TrimSpace(manifest.RepositoryID)
		manifestDigest := strings.TrimSpace(manifest.Digest)
		configDigest := strings.TrimSpace(payloadcore.DerefString(manifest.Config.Digest))
		if repositoryID == "" ||
			!validContainerImageIdentityRetirementDigest(manifestDigest) ||
			configDigest == "" {
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
	unmappedConfigWarningRepositories map[string]struct{},
	truncatedRepositories map[string]struct{},
	missingManifestRepositories map[string]struct{},
) string {
	repositoryID := strings.TrimSpace(decision.RepositoryID)
	parsed, parsedOK := ParseContainerImageRef(decision.ImageRef)
	if repositoryID == "" && parsedOK {
		repositoryID = repositoryIDFromKey(parsed.RepositoryKey)
	}
	if _, held := heldManifestDigests[containerImageIdentityRepositoryDigest{
		repositoryID: repositoryID,
		digest:       strings.TrimSpace(decision.Digest),
	}]; held {
		return containerImageIdentityRetireHoldConfigBlobUnavailable
	}
	if _, held := unmappedConfigWarningRepositories[repositoryID]; held {
		return containerImageIdentityRetireHoldConfigBlobUnavailable
	}

	if _, held := missingManifestRepositories[repositoryID]; held {
		return containerImageIdentityRetireHoldMissingManifestDigest
	}
	if parsedOK && parsed.Digest == "" {
		if _, held := truncatedRepositories[repositoryID]; held {
			return containerImageIdentityRetireHoldTagListTruncated
		}
	}
	return ""
}

func containerImageIdentityDecisionWithOutcome(
	decision ContainerImageIdentityDecision,
	outcome reducercontract.ContainerImageIdentityOutcome,
) ContainerImageIdentityDecision {
	candidate := decision
	candidate.CanonicalWrites = 1
	candidate.Outcome = outcome
	return candidate
}
