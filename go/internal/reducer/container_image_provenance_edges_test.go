// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestContainerImageBuiltFromRowsAdmitsExactDigestOnly(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest:                       "sha256:exact",
			SourceRepositoryIDs:          []string{"repo-1"},
			BuildProvenanceRepositoryIDs: []string{"repo-1"},
			Outcome:                      ContainerImageIdentityExactDigest,
		},
		{
			Digest:                       "sha256:tagresolved",
			SourceRepositoryIDs:          []string{"repo-2"},
			BuildProvenanceRepositoryIDs: []string{"repo-2"},
			Outcome:                      ContainerImageIdentityTagResolved,
		},
		{
			Digest:                       "sha256:ambiguous",
			SourceRepositoryIDs:          []string{"repo-3"},
			BuildProvenanceRepositoryIDs: []string{"repo-3"},
			Outcome:                      ContainerImageIdentityAmbiguousTag,
		},
		{
			Digest:                       "sha256:unresolved",
			SourceRepositoryIDs:          []string{"repo-4"},
			BuildProvenanceRepositoryIDs: []string{"repo-4"},
			Outcome:                      ContainerImageIdentityUnresolved,
		},
		{
			Digest:                       "sha256:stale",
			SourceRepositoryIDs:          []string{"repo-5"},
			BuildProvenanceRepositoryIDs: []string{"repo-5"},
			Outcome:                      ContainerImageIdentityStaleTag,
		},
		{
			// exact_digest but no build-provenance repository resolved -- must
			// never fabricate a row.
			Digest:                       "sha256:noreporesolved",
			BuildProvenanceRepositoryIDs: nil,
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (exact_digest with a build-provenance repo only): %#v", len(rows), rows)
	}
	if rows[0]["digest"] != "sha256:exact" || rows[0]["repository_id"] != "repo-1" {
		t.Fatalf("row = %#v, want digest=sha256:exact repository_id=repo-1", rows[0])
	}
}

// TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository is the
// #5796 regression: a repository whose only anchor to the image is a
// runtime/deploy reference (SourceRepositoryIDs set from the referencing
// repository's own scope) must never be attributed as the image's builder.
// Only BuildProvenanceRepositoryIDs -- populated by genuine build evidence --
// may produce a BUILT_FROM row.
func TestContainerImageBuiltFromRowsExcludesRuntimeReferenceOnlyRepository(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest: "sha256:deployreferenceonly",
			// A Kubernetes manifest in "repository:deploy-only" merely
			// references this digest-pinned third-party image; that anchors
			// SourceRepositoryIDs but is not build evidence.
			SourceRepositoryIDs:          []string{"repository:deploy-only"},
			BuildProvenanceRepositoryIDs: nil,
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0: a deploy-only reference must not emit a BUILT_FROM row: %#v", len(rows), rows)
	}
}

func TestContainerImageBuiltFromRowsFansOutMultipleBuildProvenanceRepositories(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{
			Digest: "sha256:multi",
			// SourceRepositoryIDs deliberately differs from
			// BuildProvenanceRepositoryIDs to prove the row builder fans out
			// over the latter, not the former.
			SourceRepositoryIDs:          []string{"repo-deploy-only"},
			BuildProvenanceRepositoryIDs: []string{"repo-b", "repo-a", "repo-a"},
			Outcome:                      ContainerImageIdentityExactDigest,
		},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one edge per distinct build-provenance repository): %#v", len(rows), rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row["digest"] != "sha256:multi" {
			t.Fatalf("row digest = %v, want sha256:multi", row["digest"])
		}
		if row["repository_id"] == "repo-deploy-only" {
			t.Fatalf("row attributed the deploy-only reference repository, want only build-provenance repos: %#v", rows)
		}
		seen[row["repository_id"].(string)] = true
	}
	if !seen["repo-a"] || !seen["repo-b"] {
		t.Fatalf("expected rows for both repo-a and repo-b, got %#v", rows)
	}
}

func TestContainerImageBuiltFromRowsRejectsBlankDigest(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{Digest: "  ", BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for a blank digest: %#v", len(rows), rows)
	}
}

type recordingContainerImageProvenanceEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageProvenanceEdgeWriter) WriteBuiltFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, evidenceSource string,
) error {
	w.writeRows = append(w.writeRows, rows)
	_ = evidenceSource
	return w.writeErr
}

func (w *recordingContainerImageProvenanceEdgeWriter) RetractBuiltFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

func TestProjectContainerImageBuiltFromEdgesNoOpWithoutWriter(t *testing.T) {
	t.Parallel()

	h := ContainerImageIdentityHandler{}
	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, nil); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error with no writer: %v", err)
	}
}

func TestProjectContainerImageBuiltFromEdgesRetractsFirstThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1", len(writer.retractCalls))
	}
	if writer.retractCalls[0] != containerImageBuiltFromProvenanceEvidenceSource {
		t.Fatalf("retract evidence_source = %q, want %q", writer.retractCalls[0], containerImageBuiltFromProvenanceEvidenceSource)
	}
	if len(writer.writeRows) != 1 || len(writer.writeRows[0]) != 1 {
		t.Fatalf("writeRows = %#v, want one write of one row", writer.writeRows)
	}
}

func TestProjectContainerImageBuiltFromEdgesRetractsEvenWhenNoRowsToWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-2"}, nil); err != nil {
		t.Fatalf("projectContainerImageBuiltFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1 even with nothing to write", len(writer.retractCalls))
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows = %#v, want none for an empty projection", writer.writeRows)
	}
}

func TestProjectContainerImageBuiltFromEdgesPropagatesWriterError(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageProvenanceEdgeWriter{writeErr: errors.New("boom")}
	h := ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer}
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:exact", SourceRepositoryIDs: []string{"repo-1"}, BuildProvenanceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageBuiltFromEdges(context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions); err == nil {
		t.Fatal("expected an error when the writer fails")
	}
}

// TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd proves the
// #5796 fix's other required direction, end to end through the real
// evidence-extraction pipeline rather than a hand-built decision: a CI run
// that reported producing this artifact digest (ci.run.repository_id, joined
// via ci.artifact.artifact_digest in addCICDArtifactImageReference) IS build
// evidence, so it must still reach BuildProvenanceRepositoryIDs and still
// produce a BUILT_FROM row after the gate narrowed off SourceRepositoryIDs.
//
// This is the exact evidence shape the B-7 golden-corpus rc-165 assertion
// depends on (the cicdrun cassette's ci.run carries repository_id
// repository:r_69256c06, joined to the ociregistry cassette's manifest
// digest) -- this test is the unit-level proof that rc-165 survives the
// narrowing without needing the live golden-corpus gate. Neither
// container_image_build_provenance_test.go (which covers the OCI
// config-source-label path and its negative) nor
// container_image_derived_from_edges_test.go (DERIVED_FROM-specific) cover
// this CI-run wiring reaching a decision through the public
// BuildContainerImageIdentityDecisions entrypoint.
func TestContainerImageBuiltFromRowsFromCICDRunBuildProvenanceEndToEnd(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		ciRunFact("run-image", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-image", "run-image", testContainerDigest),
		ociManifestFact("oci-manifest", testContainerDigest),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api@"+testContainerDigest]
	assertContainerImageDecision(t, got, ContainerImageIdentityExactDigest, testContainerDigest, 1)
	if !stringSliceContains(got.BuildProvenanceRepositoryIDs, "repo-api") {
		t.Fatalf("BuildProvenanceRepositoryIDs = %#v, want repo-api: a CI run reporting it produced this digest is build evidence", got.BuildProvenanceRepositoryIDs)
	}

	rows := containerImageBuiltFromRows(decisions)
	if len(rows) != 1 || rows[0]["repository_id"] != "repo-api" {
		t.Fatalf("containerImageBuiltFromRows = %#v, want one row for repo-api", rows)
	}
}

// TestContainerImageBuiltFromRowsPinCICompetingRefDigestToOneRepositoryPair is
// the graph-truth half of the #5829 fix (the decision-level half lives in
// container_image_ci_run_digest_provenance_test.go).
//
// applyCIRunDigestRevision now confers BuildProvenanceRepositoryIDs, and that
// field is the sole gate on this edge (#5796) and on the DERIVED_FROM child
// side (#5460). Conferring it could therefore change what the graph gets, so the
// emitted ROW SET is what has to be asserted -- not just the decision field.
//
// Two properties are pinned, and they are pinned separately on purpose:
//
//   - PER DECISION: every decision that resolves this digest emits exactly the
//     (digest, repository:r_69256c06) pair. This is the failing-then-green
//     assertion. Without the fix the explicit-image-reference decision -- the
//     one that wins the shared stable fact key and is therefore the row
//     Postgres keeps -- emits ZERO rows, so the edge would exist only by
//     accident of the sibling decision that never gets persisted.
//   - ACROSS DECISIONS: the distinct (digest, repository) set is exactly ONE
//     pair naming exactly ONE repository. This is the #5827 guard. BUILT_FROM
//     MERGEs on (start, end, type) while
//     projectContainerImageBuiltFromEdges retracts per
//     (scope_id, generation_id, evidence_source), so if a later change let the
//     ci.run anchor reach a SECOND scope's decision, both scopes would emit
//     this same pair and one scope's retract would delete an edge the other
//     still supports -- silently, with no gate going red. Today the anchor
//     cannot: ci.run/ci.artifact are loaded scope-locally
//     (containerImageIdentityFactKinds via loadFactsForKinds in
//     container_image_identity.go) and are absent from
//     listActiveContainerImageIdentityFactsQuery's cross-scope filter
//     (internal/storage/postgres/facts_active_container_image_identity.go), so
//     the widening is INTRA-scope. This assertion is what keeps it that way.
func TestContainerImageBuiltFromRowsPinCICompetingRefDigestToOneRepositoryPair(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions(ciDigestProvenanceEnvelopes())

	want := map[string]any{
		"digest":        ciDigestProvenanceDigest,
		"repository_id": ciDigestProvenanceBuildRepo,
	}
	resolved := 0
	for _, decision := range decisions {
		if decision.Digest != ciDigestProvenanceDigest {
			continue
		}
		resolved++
		rows := containerImageBuiltFromRows([]ContainerImageIdentityDecision{decision})
		if len(rows) != 1 || !maps.Equal(rows[0], want) {
			t.Fatalf(
				"decision %q (%s) emitted BUILT_FROM rows %#v, want exactly one %#v",
				decision.ImageRef, decision.Reason, rows, want,
			)
		}
	}
	if resolved == 0 {
		t.Fatalf("no decision resolved digest %q; the fixture no longer reproduces the corpus shape", ciDigestProvenanceDigest)
	}

	pairs := map[string]struct{}{}
	rows := containerImageBuiltFromRows(decisions)
	for _, row := range rows {
		if !maps.Equal(row, want) {
			t.Fatalf("BUILT_FROM row %#v, want %#v: no other (digest, repository) pair may be emitted here", row, want)
		}
		pairs[fmt.Sprintf("%v\x00%v", row["digest"], row["repository_id"])] = struct{}{}
	}
	if len(pairs) != 1 {
		t.Fatalf("distinct BUILT_FROM pairs = %d, want exactly 1 (#5827: a second pair means a second retract owner)", len(pairs))
	}
	// Cross-decision dedup: both decisions above now carry the same
	// build-provenance repository, so the batch must carry the pair ONCE.
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %d decisions resolving one pair must UNWIND it once, not %d times", len(rows), resolved, len(rows))
	}
}

// TestContainerImageBuiltFromRowsEmitNothingForDeployOnlyScope is the negative
// half of the pin above, and the property that keeps this digest's BUILT_FROM
// edge to a single retract owner.
//
// The deploying repository sees the same image by explicit reference and the
// same registry manifest, but no ci.run/ci.artifact -- exactly what the
// scope-local load gives a git-repository scope. Its decisions resolve
// exact_digest and carry the deploying repository in SourceRepositoryIDs, and
// they must still emit NO row: SourceRepositoryIDs is not build evidence
// (#5796), so only the CI scope can ever own this digest's BUILT_FROM edge.
func TestContainerImageBuiltFromRowsEmitNothingForDeployOnlyScope(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		{
			FactID:   "content-entity-deploy-only",
			FactKind: factKindContentEntity,
			Payload: map[string]any{
				"repository_id": ciDigestProvenanceRunRepo,
				"entity_metadata": map[string]any{
					"container_images": []any{ciDigestProvenanceImageRef},
				},
			},
		},
		ociManifestFactForRepository(
			"oci-manifest-deploy-only",
			ciDigestProvenanceRegistry,
			ciDigestProvenanceImageRepo,
			ciDigestProvenanceDigest,
		),
	})

	resolved := 0
	for _, decision := range decisions {
		if decision.Digest == ciDigestProvenanceDigest {
			resolved++
		}
	}
	if resolved == 0 {
		t.Fatalf("no deploy-only decision resolved digest %q; the fixture no longer reproduces the corpus shape", ciDigestProvenanceDigest)
	}

	if rows := containerImageBuiltFromRows(decisions); len(rows) != 0 {
		t.Fatalf("containerImageBuiltFromRows = %#v, want none: a deploy-only scope owns no build evidence for this digest", rows)
	}
}
