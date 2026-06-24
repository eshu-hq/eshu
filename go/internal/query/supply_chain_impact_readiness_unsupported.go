// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// SupplyChainImpactUnsupportedTarget describes one observed vulnerability
// target Eshu cannot match precisely with the current matcher set. Targets
// carry the kind of evidence Eshu saw and a stable reason code so callers can
// tell "we observed something we cannot resolve" from "we collected nothing".
//
// Unsupported targets never imply the target is safe or affected. They are
// coverage-gap evidence: the producer observed a real artifact but the
// matcher could not turn that artifact into a finding.
type SupplyChainImpactUnsupportedTarget struct {
	TargetKind     string `json:"target_kind"`
	Reason         string `json:"reason"`
	Count          int    `json:"count"`
	Ecosystem      string `json:"ecosystem,omitempty"`
	LockfileFlavor string `json:"lockfile_flavor,omitempty"`
	FeatureToken   string `json:"feature_token,omitempty"`
}

const (
	// UnsupportedTargetKindEcosystem marks owned dependency evidence in an
	// ecosystem the supply-chain version matcher cannot evaluate (e.g.,
	// Go modules or RubyGems today). The dependency was observed but
	// cannot be matched, so the result is neither clean nor affected.
	UnsupportedTargetKindEcosystem = "ecosystem"
	// UnsupportedTargetKindPackageManagerFile marks a package-manager file
	// Eshu parsed but recorded an unsupported lockfile feature (e.g., Yarn
	// Berry "patch" entries). Evidence rows still exist but the lockfile
	// chain that would prove exact-version impact is incomplete.
	UnsupportedTargetKindPackageManagerFile = "package_manager_file"
	// UnsupportedTargetKindDependencySource marks VCS, path, URL, editable,
	// local, or other provenance-only dependency rows that identify a real
	// dependency source without proving a registry-resolvable package version.
	UnsupportedTargetKindDependencySource = "dependency_source"
	// UnsupportedTargetKindSBOMTarget marks an SBOM target Eshu observed but
	// could not fully parse — `unsupported_field`, `malformed_document`, or
	// equivalent SBOM warnings tied to the requested subject digest.
	UnsupportedTargetKindSBOMTarget = "sbom_target"
	// UnsupportedTargetKindPackageRegistryMetadata marks package registry
	// metadata Eshu observed but did not parse because the source document
	// exceeded the configured byte limit. The package exists, but version and
	// vulnerability matching evidence remains unsupported for this source.
	UnsupportedTargetKindPackageRegistryMetadata = "package_registry_metadata"
	// UnsupportedTargetKindImageTarget marks a container image target Eshu
	// observed but cannot analyze with the current scanner-worker matrix.
	UnsupportedTargetKindImageTarget = "image_target"
)

// MissingEvidenceUnsupportedTargets signals the scope carries observed
// unsupported target evidence the matcher cannot resolve. Surfaced alongside
// readiness_state=unsupported so callers see one stable identifier in the
// missing_evidence list rather than ad-hoc strings per target_kind.
const MissingEvidenceUnsupportedTargets = "unsupported_targets"

// allowedUnsupportedTargetKinds is the closed set of target_kind identifiers
// the envelope is allowed to surface. Unknown values are dropped to prevent
// silent contract drift between Postgres CTE literals and the Go classifier.
var allowedUnsupportedTargetKinds = map[string]struct{}{
	UnsupportedTargetKindEcosystem:               {},
	UnsupportedTargetKindPackageManagerFile:      {},
	UnsupportedTargetKindDependencySource:        {},
	UnsupportedTargetKindSBOMTarget:              {},
	UnsupportedTargetKindPackageRegistryMetadata: {},
	UnsupportedTargetKindImageTarget:             {},
}

// normalizeUnsupportedTargets trims, drops empty/unknown entries, merges
// duplicates by (target_kind, reason, ecosystem, lockfile_flavor,
// feature_token), and returns a deterministic ordering. Counts on duplicate
// keys are summed so a producer that emits one fact per observation does not
// inflate the envelope with redundant rows.
func normalizeUnsupportedTargets(targets []SupplyChainImpactUnsupportedTarget) []SupplyChainImpactUnsupportedTarget {
	if len(targets) == 0 {
		return nil
	}
	type key struct {
		kind, reason, ecosystem, flavor, feature string
	}
	merged := map[key]SupplyChainImpactUnsupportedTarget{}
	for _, target := range targets {
		kind := strings.TrimSpace(target.TargetKind)
		if kind == "" {
			continue
		}
		if _, ok := allowedUnsupportedTargetKinds[kind]; !ok {
			continue
		}
		entry := SupplyChainImpactUnsupportedTarget{
			TargetKind:     kind,
			Reason:         strings.TrimSpace(target.Reason),
			Count:          target.Count,
			Ecosystem:      strings.TrimSpace(target.Ecosystem),
			LockfileFlavor: strings.TrimSpace(target.LockfileFlavor),
			FeatureToken:   strings.TrimSpace(target.FeatureToken),
		}
		// Reason is part of the API contract: every surfaced entry must
		// carry a stable reason code so callers can interpret the
		// target_kind without guessing. Drop rows whose producer did not
		// supply a reason instead of publishing an envelope that
		// contradicts the OpenAPI schema.
		if entry.Reason == "" {
			continue
		}
		if entry.Count <= 0 {
			entry.Count = 1
		}
		k := key{
			kind:      entry.TargetKind,
			reason:    entry.Reason,
			ecosystem: entry.Ecosystem,
			flavor:    entry.LockfileFlavor,
			feature:   entry.FeatureToken,
		}
		if existing, ok := merged[k]; ok {
			existing.Count += entry.Count
			merged[k] = existing
			continue
		}
		merged[k] = entry
	}
	if len(merged) == 0 {
		return nil
	}
	out := make([]SupplyChainImpactUnsupportedTarget, 0, len(merged))
	for _, entry := range merged {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TargetKind != out[j].TargetKind {
			return out[i].TargetKind < out[j].TargetKind
		}
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		if out[i].Ecosystem != out[j].Ecosystem {
			return out[i].Ecosystem < out[j].Ecosystem
		}
		if out[i].LockfileFlavor != out[j].LockfileFlavor {
			return out[i].LockfileFlavor < out[j].LockfileFlavor
		}
		return out[i].FeatureToken < out[j].FeatureToken
	})
	return out
}
