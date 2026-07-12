// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	gcpsynth "github.com/eshu-hq/eshu/go/internal/synth/gcp"
)

// amplifiedGCPCollectorLabel names the collector on an amplified GCP cassette so
// an amplified corpus is distinguishable from a hand-authored or recorded one in
// downstream reports. It is intentionally distinct from synth-cassette's default
// so an amplified load run is traceable to this seam.
const amplifiedGCPCollectorLabel = "ifa_amplified_gcp"

// OduFamily identifies which family-native synthetic generator amplifies a base
// Odù across N disjoint scopes. A family exists here only when it has a
// generator that produces disjoint PAYLOAD identities per scope, not merely
// disjoint scope ids.
type OduFamily string

// FamilyGCP is the GCP cloud-resource family, amplified through
// go/internal/synth/gcp.GenerateMultiScope.
const FamilyGCP OduFamily = "gcp"

// AmplifyAtSlot replays one base Odù of the given family across slot.Scopes
// synthetic scopes and returns the canonical v1 cassette bytes, ready for the P2
// concurrentreplay driver. It is the Layer 3 corpus amplifier
// (docs/internal/design/4389-ifa-conformance-platform.md "Layer 3"): one
// synthetic 1-repo Odù becomes an N-scope load run with zero new recordings and
// zero credentials.
//
// It is family-aware by construction and deliberately does NOT rewrite scope_id
// and stable_fact_key generically. The ADR's P3 landmine correction is explicit
// that a generic scope_id/stable_fact_key rewrite is determinism-unsafe for
// cloud-resource families: graph nodes key on payload identity (a GCP
// CloudResource uid folds in full_resource_name), so K scopes that share the
// same underlying payload MERGE onto one node and race last-writer-wins on
// source_fact_id — a false red caused by the load generator itself. This
// amplifier instead delegates to the family-native generator, whose scopes are
// disjoint by construction (each scope's resources embed its own distinct
// ProjectID in full_resource_name; see GenerateMultiScope's
// TestGenerateMultiScopeScopesHaveDisjointFullResourceNames). A family without
// such a generator returns an error rather than falling back to the unsafe
// generic rewrite.
//
// Determinism is inherited from the family generator: the same (family, slot,
// seed) always produces byte-identical bytes, and each scope's generation id is
// the seed-indexed derived identity (replay.DerivedGenerationID) the
// canonicalizer already exports.
func AmplifyAtSlot(family OduFamily, slot ScaleSlot, seed uint64) ([]byte, error) {
	if err := slot.requireAmplifiable(); err != nil {
		return nil, err
	}
	switch family {
	case FamilyGCP:
		raw, err := gcpsynth.GenerateMultiScope(gcpsynth.MultiScopeOptions{
			Seed:           seed,
			Scopes:         slot.Scopes,
			ResourceCount:  slot.ResourceCount,
			CollectorLabel: amplifiedGCPCollectorLabel,
		})
		if err != nil {
			return nil, fmt.Errorf("ifa: amplify %s at slot %q: %w", family, slot.ID, err)
		}
		return raw, nil
	default:
		return nil, fmt.Errorf(
			"ifa: amplify: unsupported Odù family %q; only %q has a family-native disjoint-by-construction generator today, and a generic scope_id/stable_fact_key rewrite is determinism-unsafe (ADR Layer 3 landmine)",
			family, FamilyGCP)
	}
}
