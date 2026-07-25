// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestContainerImageIdentityHandlerProjectsDerivedFromEdgeFromCrossScopeCIEvidence
// is the #5810 regression: DERIVED_FROM (base-image lineage, #5460) gates its
// child on BuildProvenanceRepositoryIDs, and a CI run reporting it built a
// digest is one of the only two ways to populate that field (the other being
// an OCI config source label). But projectContainerImageDerivedFromEdges is
// owner-scoped to the repository whose scope the intent runs in
// (container_image_derived_from_edges.go), while ci.run/ci.artifact facts are
// written by the ci_cd_run collector in the CI run's OWN, different scope.
// Before #5810, Handle() never loaded those facts cross-scope, so a
// repository whose child image is CI-built (no OCI source label, no
// Kubernetes manifest naming the child) could never durably produce a
// DERIVED_FROM edge outside a same-scope unit test that bypasses real scope
// separation -- exactly the false-green shape #5810 calls out.
//
// The intent here is scoped to the repository itself
// ("repository:team-api"). That repository's OWN scope-local facts
// (loader.scopeFacts) carry ONLY the Dockerfile `file` fact declaring the
// runtime base -- no CI evidence at all. The ci.run and ci.artifact envelopes
// that prove the repository's CI pipeline built the child digest are
// delivered ONLY through the new cross-scope
// activeContainerImageCIFactLoader (loader.ciActive), simulating persistence
// in the CI run's own, separate scope. The OCI registry manifest observations
// needed to resolve both digests to exact_digest arrive through the EXISTING
// activeContainerImageIdentityFactLoader (loader.active) cross-scope bridge,
// exactly as they do in production for a repository-scoped refresh.
func TestContainerImageIdentityHandlerProjectsDerivedFromEdgeFromCrossScopeCIEvidence(t *testing.T) {
	t.Parallel()

	const (
		baseDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		childDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		ownerRepoID = "repository:team-api"
	)

	loader := &stubContainerImageIdentityFactLoader{
		// The repository-owned intent's OWN scope-local facts: the Dockerfile
		// alone, declaring its runtime base by digest. Nothing else -- no CI
		// evidence, no Kubernetes manifest naming the child.
		scopeFacts: []facts.Envelope{
			dockerfileStagesFact(
				"fact-dockerfile", ownerRepoID, "Dockerfile",
				map[string]any{
					"stage_index": 0,
					"base_image":  "registry.example.com/team/api@" + baseDigest,
					"base_tag":    "",
				},
			),
		},
		// The existing cross-scope OCI/identity bridge: registry observations
		// for both digests, exactly as a repository-scoped refresh already
		// sees them in production.
		active: []facts.Envelope{
			ociManifestFact("oci-manifest-base", baseDigest),
			ociManifestFact("oci-manifest-child", childDigest),
		},
		// The NEW #5810 cross-scope CI bridge: a CI run that reports building
		// the child digest for this same repository, persisted in the CI
		// run's own scope and reachable ONLY through
		// ListActiveContainerImageCIFacts.
		ciActive: []facts.Envelope{
			ciRunFact("run-1", "github_actions", ownerRepoID, "commit-abc"),
			ciArtifactFact("artifact-1", "run-1", childDigest),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:            loader,
		Writer:                writer,
		DerivedFromEdgeWriter: derivedFromWriter,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-derived-from-ci-bridge",
		ScopeID:      ownerRepoID,
		GenerationID: "generation-repo",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "dockerfile observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.ciActiveCall != 1 {
		t.Fatalf("ListActiveContainerImageCIFacts() calls = %d, want 1", loader.ciActiveCall)
	}

	if got, want := len(derivedFromWriter.writeRows), 1; got != want {
		t.Fatalf("WriteDerivedFromEdges rows = %#v, want exactly %d row", derivedFromWriter.writeRows, want)
	}
	rows := derivedFromWriter.writeRows[0]
	if got, want := len(rows), 1; got != want {
		t.Fatalf("WriteDerivedFromEdges row count = %d, want %d: %#v", got, want, rows)
	}
	row := rows[0]
	if got, want := row["digest"], childDigest; got != want {
		t.Fatalf("row[digest] = %v, want %q (the CI-built child)", got, want)
	}
	if got, want := row["base_digest"], baseDigest; got != want {
		t.Fatalf("row[base_digest] = %v, want %q (the Dockerfile base)", got, want)
	}
}

// TestContainerImageIdentityHandlerCIScopeIntentWithCrossScopeCIEvidenceWritesNothing
// is the companion owner-gate proof: swap the SAME facts onto a CI-scope
// intent (not a repository scope) and confirm NO DERIVED_FROM row is written.
// The owner gate (repositoryIDFromReducerScope, projectContainerImageDerivedFromEdges)
// must stay intact -- seeing both endpoints cross-scope is not enough for a
// non-owning scope to write the edge, exactly the multi-writer defect
// TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing already
// pins at the projectContainerImageDerivedFromEdges level. This test pins the
// same invariant at the Handle() level with the new CI loader wired in, so
// the #5810 cross-scope bridge cannot regress owner-scoping.
func TestContainerImageIdentityHandlerCIScopeIntentWithCrossScopeCIEvidenceWritesNothing(t *testing.T) {
	t.Parallel()

	const (
		baseDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		childDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		ownerRepoID = "repository:team-api"
	)

	loader := &stubContainerImageIdentityFactLoader{
		// A CI-scope intent owns no Dockerfile, so its own scope-local facts
		// carry nothing relevant.
		scopeFacts: nil,
		active: []facts.Envelope{
			dockerfileStagesFact(
				"fact-dockerfile", ownerRepoID, "Dockerfile",
				map[string]any{
					"stage_index": 0,
					"base_image":  "registry.example.com/team/api@" + baseDigest,
					"base_tag":    "",
				},
			),
			ociManifestFact("oci-manifest-base", baseDigest),
			ociManifestFact("oci-manifest-child", childDigest),
		},
		ciActive: []facts.Envelope{
			ciRunFact("run-1", "github_actions", ownerRepoID, "commit-abc"),
			ciArtifactFact("artifact-1", "run-1", childDigest),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:            loader,
		Writer:                writer,
		DerivedFromEdgeWriter: derivedFromWriter,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-ci-scope-no-write",
		ScopeID:      "ci_cd_run:github_actions:team-api",
		GenerationID: "generation-ci",
		SourceSystem: "ci_cd_run",
		Domain:       DomainContainerImageIdentity,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(derivedFromWriter.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1 (retract still fires for a non-owning scope)", len(derivedFromWriter.retractCalls))
	}
	if len(derivedFromWriter.writeRows) != 0 {
		t.Fatalf("writeRows = %#v, want none -- a CI-run scope owns no Dockerfile and must not write", derivedFromWriter.writeRows)
	}
}

// TestDedupeEnvelopesByFactIDCollapsesRepeatedFactID proves the standalone
// helper's contract in isolation: envelopes sharing a FactID collapse to the
// FIRST occurrence, and relative order is preserved for the survivors.
func TestDedupeEnvelopesByFactIDCollapsesRepeatedFactID(t *testing.T) {
	t.Parallel()

	first := facts.Envelope{FactID: "fact-1", FactKind: "ci.run"}
	duplicate := facts.Envelope{FactID: "fact-1", FactKind: "ci.run-cross-scope-copy"}
	other := facts.Envelope{FactID: "fact-2", FactKind: "ci.artifact"}

	deduped := dedupeEnvelopesByFactID([]facts.Envelope{first, duplicate, other})

	if got, want := len(deduped), 2; got != want {
		t.Fatalf("len(deduped) = %d, want %d: %#v", got, want, deduped)
	}
	if got, want := deduped[0].FactID, "fact-1"; got != want {
		t.Fatalf("deduped[0].FactID = %q, want %q", got, want)
	}
	if got, want := deduped[0].FactKind, "ci.run"; got != want {
		t.Fatalf("deduped[0].FactKind = %q, want %q (must keep the FIRST occurrence, not the duplicate)", got, want)
	}
	if got, want := deduped[1].FactID, "fact-2"; got != want {
		t.Fatalf("deduped[1].FactID = %q, want %q", got, want)
	}
}

// TestContainerImageIdentityHandlerDedupesCICDArtifactSeenThroughBothLoaders is
// the #5810 double-quarantine regression: a CI-scope intent's own scope-local
// load (loadFactsForKinds) and the new cross-scope
// activeContainerImageCIFactLoader can both return the SAME ci.artifact
// envelope once that CI scope's own generation is active. Before the
// FactID-keyed dedupe in Handle(), a MALFORMED fact seen through both paths
// would be independently decoded (and independently quarantined) twice for
// one intent, double-counting a single bad fact's input_invalid signal. This
// proves it is counted exactly once.
func TestContainerImageIdentityHandlerDedupesCICDArtifactSeenThroughBothLoaders(t *testing.T) {
	t.Parallel()

	malformed := facts.Envelope{
		FactID:   "malformed-artifact-cross-scope",
		FactKind: facts.CICDArtifactFactKind,
		Payload: map[string]any{
			// "run_id" intentionally absent -- see
			// TestContainerImageIdentityHandlerQuarantinesCICDArtifactMissingRunID.
			"provider":        "github_actions",
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": testContainerDigest,
		},
	}
	loader := &stubContainerImageIdentityFactLoader{
		// The SAME envelope (same FactID) reachable through the intent's own
		// scope-local load AND the cross-scope CI loader -- exactly what
		// happens for a CI-scope-triggered intent once its generation is
		// active.
		scopeFacts: []facts.Envelope{malformed},
		ciActive:   []facts.Envelope{malformed},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{FactLoader: loader, Writer: writer}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-dedupe-quarantine",
		ScopeID:      "ci_cd_run:github_actions:team-api",
		GenerationID: "generation-ci",
		SourceSystem: "ci_cd_run",
		Domain:       DomainContainerImageIdentity,
		Cause:        "ci artifact observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := result.SubSignals["input_invalid_facts"]; got != 1 {
		t.Fatalf("SubSignals[input_invalid_facts] = %v, want 1 (the SAME malformed fact seen through two loaders must count once)", got)
	}
}
