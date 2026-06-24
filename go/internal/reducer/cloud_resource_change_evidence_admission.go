// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/cloudinventory"
)

const (
	maxCloudResourceChangeEvidencePerResource = 25
	maxCloudResourceChangePropertyPaths       = 25
)

// CloudResourceChangeEvidenceRecord is one provider resource-change source fact
// projected into the fields the shared admission path may attach as freshness
// evidence. RawIdentity is the changed provider resource identity; it is used
// only for canonical uid resolution and must not be persisted in readback
// evidence.
type CloudResourceChangeEvidenceRecord struct {
	Provider                 string
	RawIdentity              string
	EvidenceKey              string
	ChangeType               string
	ChangeTime               time.Time
	Operation                string
	ClientType               string
	ActorClass               string
	ActorFingerprint         string
	ChangedPropertyPaths     []string
	ChangedPropertyTruncated bool
	TombstoneCandidate       bool
}

// CloudResourceChangeEvidence is the sanitized freshness row attached to an
// admitted canonical CloudResource. It intentionally excludes provider
// locators, raw actor ids, before/after values, and provider bodies.
type CloudResourceChangeEvidence struct {
	EvidenceKey              string
	ChangeType               string
	ChangeTime               time.Time
	Operation                string
	ClientType               string
	ActorClass               string
	ActorFingerprint         string
	ChangedPropertyPaths     []string
	ChangedPropertyTruncated bool
	TombstoneCandidate       bool
}

// CloudResourceChangeEvidenceLoader loads resource-change source facts for the
// inventory admission generation. Provider implementations may map the supplied
// inventory scope/generation to a sibling resource-change lane, but the final
// read must stay bounded to one current source generation so stale freshness
// evidence cannot leak into a newer admission.
type CloudResourceChangeEvidenceLoader interface {
	// LoadCloudResourceChangeEvidence returns resource-change records relevant
	// to the supplied inventory admission scope and generation.
	LoadCloudResourceChangeEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
	) ([]CloudResourceChangeEvidenceRecord, error)
}

// attachCloudResourceChangeEvidence merges sanitized change evidence onto the
// admitted resource that shares each record's cloud_resource_uid. Change
// evidence is freshness only: a record whose uid was not admitted from resource
// evidence is dropped, so resourcechanges can never fabricate canonical
// inventory truth or final deletions.
func attachCloudResourceChangeEvidence(
	resources []AdmittedCloudResource,
	records []CloudResourceChangeEvidenceRecord,
) {
	if len(resources) == 0 || len(records) == 0 {
		return
	}
	byUID := make(map[string]int, len(resources))
	for i := range resources {
		byUID[resources[i].CloudResourceUID] = i
	}

	candidatesByResource := make(map[int][]CloudResourceChangeEvidence, len(resources))
	for _, record := range records {
		evidence, ok := cloudResourceChangeEvidenceFromRecord(record)
		if !ok {
			continue
		}
		resolution := cloudinventory.ResolveProviderIdentity(record.Provider, record.RawIdentity)
		if resolution.Outcome != cloudinventory.ResolutionOutcomeAdmitted {
			continue
		}
		idx, ok := byUID[resolution.CloudResourceUID]
		if !ok {
			continue
		}
		candidatesByResource[idx] = append(candidatesByResource[idx], evidence)
	}

	for idx, candidates := range candidatesByResource {
		evidence, truncated := boundedCloudResourceChangeEvidence(candidates)
		resources[idx].ResourceChangeEvidence = evidence
		resources[idx].ResourceChangeEvidenceTruncated = truncated
	}
}

func cloudResourceChangeEvidenceFromRecord(
	record CloudResourceChangeEvidenceRecord,
) (CloudResourceChangeEvidence, bool) {
	changeType, ok := normalizeCloudResourceChangeType(record.ChangeType)
	if !ok || record.ChangeTime.IsZero() {
		return CloudResourceChangeEvidence{}, false
	}
	paths, pathsTruncated := boundedCloudResourceChangePropertyPaths(record.ChangedPropertyPaths)
	return CloudResourceChangeEvidence{
		EvidenceKey:              strings.TrimSpace(record.EvidenceKey),
		ChangeType:               changeType,
		ChangeTime:               record.ChangeTime.UTC(),
		Operation:                strings.TrimSpace(record.Operation),
		ClientType:               strings.TrimSpace(record.ClientType),
		ActorClass:               strings.TrimSpace(record.ActorClass),
		ActorFingerprint:         strings.TrimSpace(record.ActorFingerprint),
		ChangedPropertyPaths:     paths,
		ChangedPropertyTruncated: record.ChangedPropertyTruncated || pathsTruncated,
		TombstoneCandidate:       changeType == "deleted" && record.TombstoneCandidate,
	}, true
}

func boundedCloudResourceChangeEvidence(
	candidates []CloudResourceChangeEvidence,
) ([]CloudResourceChangeEvidence, bool) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].ChangeTime.Equal(candidates[j].ChangeTime) {
			return candidates[i].ChangeTime.After(candidates[j].ChangeTime)
		}
		return candidates[i].EvidenceKey < candidates[j].EvidenceKey
	})
	out := make([]CloudResourceChangeEvidence, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, evidence := range candidates {
		key := cloudResourceChangeEvidenceDedupeKey(evidence)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, evidence)
	}
	if len(out) > maxCloudResourceChangeEvidencePerResource {
		return out[:maxCloudResourceChangeEvidencePerResource], true
	}
	return out, false
}

func cloudResourceChangeEvidenceDedupeKey(evidence CloudResourceChangeEvidence) string {
	if key := strings.TrimSpace(evidence.EvidenceKey); key != "" {
		return key
	}
	return strings.Join([]string{
		evidence.ChangeType,
		evidence.ChangeTime.UTC().Format(time.RFC3339Nano),
		evidence.Operation,
		evidence.ActorFingerprint,
		strings.Join(evidence.ChangedPropertyPaths, "\x00"),
	}, "\x00")
}

func boundedCloudResourceChangePropertyPaths(paths []string) ([]string, bool) {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sort.Strings(out)
	if len(out) > maxCloudResourceChangePropertyPaths {
		return out[:maxCloudResourceChangePropertyPaths], true
	}
	return out, false
}

func normalizeCloudResourceChangeType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "created":
		return "created", true
	case "updated":
		return "updated", true
	case "deleted":
		return "deleted", true
	default:
		return "", false
	}
}
