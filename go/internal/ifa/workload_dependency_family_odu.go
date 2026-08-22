// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// workloadDependencyFamilyCassettePath is where the family's committed
// cassette lives. Mirrors repoDependencyFamilyCassettePath's role: a future
// live gate replays this same committed cassette, and this file's loader plus
// a lockstep test prove it never drifts from the compiled catalog Odù
// (workload_dependency_family_catalog.go) rather than maintaining two
// fixtures that can silently diverge.
const workloadDependencyFamilyCassettePath = "testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"

// workloadDependencyFamilyCassetteFullPath joins repoRoot onto the cassette
// path.
func workloadDependencyFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, workloadDependencyFamilyCassettePath)
}

// WorkloadDependencyFamilyCassetteFullPath returns the committed cassette path
// consumed by the materializededges package's lockstep test.
func WorkloadDependencyFamilyCassetteFullPath(repoRoot string) string {
	return workloadDependencyFamilyCassetteFullPath(repoRoot)
}

// loadWorkloadDependencyFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes relationships.DiscoverEvidence and
// reducer.ExtractWorkloadCandidates consume.
//
// Unexported: it is the test-side lockstep loader for the committed cassette.
// Production registers the compiled workloadDependencyFamilyOdu() in
// catalogSeed; a lockstep test compares that registered Odù with this strict
// cassette projection so a one-sided edit fails the focused suite.
//
// It uses the production cassette source so root and per-scope replay
// contracts cannot drift from the live gate. The returned Odù deliberately
// removes replay-generated transport fields: the compiled catalog owns the
// semantic fixture, while projector admission tests separately pin the exact
// generated IDs, provenance, and scope projection behavior.
func loadWorkloadDependencyFamilyOdu(cassettePath string) (Odu, error) {
	emitted, err := LoadCassetteEnvelopes(cassettePath)
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: load workload-dependency cassette %s through production source: %w", cassettePath, err)
	}
	if len(emitted) == 0 {
		return Odu{}, fmt.Errorf("ifa: workload-dependency cassette %s carries no facts; an empty Odù makes every assertion vacuous", cassettePath)
	}

	envelopes := make([]facts.Envelope, 0, len(emitted))
	for _, fact := range emitted {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          fact.ScopeID,
			GenerationID:     fact.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			Payload:          fact.Payload,
		})
	}
	return Odu{Name: workloadDependencyFamilyOduName, Facts: envelopes}, nil
}

// LoadWorkloadDependencyFamilyOdu strictly projects the committed cassette
// into the Odù shape consumed by the materialized-edge vacuity guard.
func LoadWorkloadDependencyFamilyOdu(cassettePath string) (Odu, error) {
	return loadWorkloadDependencyFamilyOdu(cassettePath)
}
