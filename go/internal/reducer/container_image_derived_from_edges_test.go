// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// TestContainerImageDerivedFromRows pins the #5460 attribution contract for
// ContainerImage-[:DERIVED_FROM]->ContainerImage rows.
//
// Two rules carry the accuracy weight. First, exact-only on BOTH endpoints
// (#5472 decision 4): the canonical writer matches a ContainerImage by digest,
// so a non-exact endpoint has no node to attach to and a tag-only base cannot
// answer the CVE-inheritance question the edge exists to serve. Second,
// conservative attribution: nothing in Dockerfile-parsed evidence links a
// specific built image digest to a specific Dockerfile, so a repository whose
// Dockerfiles resolve to more than one distinct base is ambiguous and projects
// NO edge rather than an all-pairs fan-out that would fabricate CVE lineage.
func TestContainerImageDerivedFromRows(t *testing.T) {
	t.Parallel()

	// child is an image the repository BUILT: build evidence (an OCI config
	// source label, or a CI run reporting this digest) attributes it, which is
	// what qualifies it as a lineage child.
	child := func(digest string, repos ...string) ContainerImageIdentityDecision {
		return ContainerImageIdentityDecision{
			ImageRef:                     "child" + digest,
			Digest:                       digest,
			SourceRepositoryIDs:          repos,
			BuildProvenanceRepositoryIDs: repos,
			Outcome:                      ContainerImageIdentityExactDigest,
		}
	}
	// referencedImage is an image the repository merely DEPLOYS -- a
	// digest-pinned third-party image named in its Kubernetes manifest. That
	// reference arrives on the repository's own content_entity fact, so it lands
	// in SourceRepositoryIDs exactly like a built image, but carries no build
	// provenance.
	referencedImage := func(digest string, repos ...string) ContainerImageIdentityDecision {
		return ContainerImageIdentityDecision{
			ImageRef:            "referenced" + digest,
			Digest:              digest,
			SourceRepositoryIDs: repos,
			Outcome:             ContainerImageIdentityExactDigest,
		}
	}
	base := func(digest string, repos ...string) ContainerImageIdentityDecision {
		return ContainerImageIdentityDecision{
			ImageRef:                  "base" + digest,
			Digest:                    digest,
			BaseImageForRepositoryIDs: repos,
			Outcome:                   ContainerImageIdentityExactDigest,
		}
	}

	tests := []struct {
		name      string
		owning    string
		decisions []ContainerImageIdentityDecision
		want      []map[string]any
	}{
		{
			name:   "a non-repository scope owns no Dockerfile and projects nothing",
			owning: "",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "an intent for a different repository projects nothing",
			owning: "repo-other",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "single base and single child projects one edge",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: []map[string]any{
				{
					"digest":            "sha256:child",
					"base_digest":       "sha256:base",
					"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
				},
			},
		},
		{
			// A repository that merely deploys a digest-pinned third-party image
			// did not build it, so it cannot inherit that repository's Dockerfile
			// base. The reference lands in SourceRepositoryIDs like a built image
			// would, which is exactly why the child side gates on build
			// provenance instead.
			name:   "an image the repository only references projects nothing",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				referencedImage("sha256:thirdparty", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			// The mixed case: only the built image becomes a child; the
			// co-deployed third-party image beside it must not.
			name:   "only the built image is a child when a referenced image sits beside it",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:built", "repo-a"),
				referencedImage("sha256:thirdparty", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: []map[string]any{
				{
					"digest":            "sha256:built",
					"base_digest":       "sha256:base",
					"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
				},
			},
		},
		{
			name:   "two children of one base each get an edge",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child1", "repo-a"),
				child("sha256:child2", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: []map[string]any{
				{
					"digest":            "sha256:child1",
					"base_digest":       "sha256:base",
					"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
				},
				{
					"digest":            "sha256:child2",
					"base_digest":       "sha256:base",
					"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
				},
			},
		},
		{
			name:   "the same base digest declared by two Dockerfiles is still one base",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: []map[string]any{
				{
					"digest":            "sha256:child",
					"base_digest":       "sha256:base",
					"attribution_basis": containerImageDerivedFromBasisRepositorySingleBase,
				},
			},
		},
		{
			name:   "two distinct bases in one repository project nothing",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base1", "repo-a"),
				base("sha256:base2", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "a non-exact base projects nothing",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				func() ContainerImageIdentityDecision {
					d := base("sha256:base", "repo-a")
					d.Outcome = ContainerImageIdentityTagResolved
					return d
				}(),
			},
			want: nil,
		},
		{
			name:   "a non-exact child projects nothing",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				func() ContainerImageIdentityDecision {
					d := child("sha256:child", "repo-a")
					d.Outcome = ContainerImageIdentityAmbiguousTag
					return d
				}(),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "a base in one repository never attaches to another repository's child",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-b"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "an image that is its own base projects no self-loop",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:same", "repo-a"),
				base("sha256:same", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "a base with no child in its repository projects nothing",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:   "an empty digest never projects",
			owning: "repo-a",
			decisions: []ContainerImageIdentityDecision{
				child("", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:      "no decisions project nothing",
			owning:    "repo-a",
			decisions: nil,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := containerImageDerivedFromRows(tc.decisions, tc.owning)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("containerImageDerivedFromRows =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}

type recordingContainerImageDerivedFromEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageDerivedFromEdgeWriter) WriteDerivedFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, _ string,
) error {
	w.writeRows = append(w.writeRows, rows)
	return w.writeErr
}

func (w *recordingContainerImageDerivedFromEdgeWriter) RetractDerivedFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

func TestProjectContainerImageDerivedFromEdgesNoOpWithoutWriter(t *testing.T) {
	t.Parallel()

	h := ContainerImageIdentityHandler{}
	if err := h.projectContainerImageDerivedFromEdges(
		context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, nil,
	); err != nil {
		t.Fatalf("projectContainerImageDerivedFromEdges returned error with no writer: %v", err)
	}
}

func TestProjectContainerImageDerivedFromEdgesRetractsFirstThenWrites(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageDerivedFromEdgeWriter{}
	h := ContainerImageIdentityHandler{DerivedFromEdgeWriter: writer}
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:child", SourceRepositoryIDs: []string{"repository:repo-1"}, BuildProvenanceRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
		{Digest: "sha256:base", BaseImageForRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageDerivedFromEdges(
		context.Background(), Intent{ScopeID: "git-repository-scope:repository:repo-1", GenerationID: "gen-1"}, decisions,
	); err != nil {
		t.Fatalf("projectContainerImageDerivedFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1", len(writer.retractCalls))
	}
	if writer.retractCalls[0] != containerImageDerivedFromProvenanceEvidenceSource {
		t.Fatalf("retract evidence_source = %q, want %q", writer.retractCalls[0], containerImageDerivedFromProvenanceEvidenceSource)
	}
	// The DERIVED_FROM evidence_source must differ from the BUILT_FROM one the
	// same handler writes, or one domain's retract would delete the other's
	// edges.
	if containerImageDerivedFromProvenanceEvidenceSource == containerImageBuiltFromProvenanceEvidenceSource {
		t.Fatal("DERIVED_FROM and BUILT_FROM must not share an evidence_source")
	}
	if len(writer.writeRows) != 1 || len(writer.writeRows[0]) != 1 {
		t.Fatalf("writeRows = %#v, want one write of one row", writer.writeRows)
	}
	if writer.writeRows[0][0]["base_digest"] != "sha256:base" {
		t.Fatalf("row = %#v, want base_digest sha256:base", writer.writeRows[0][0])
	}
}

// TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing pins the
// owner-restriction that fixes the multi-writer defect the golden gate caught:
// a base reference reaches an OCI/CI/cloud-scope intent through the active
// cross-scope fact load, but that intent's scope owns no Dockerfile, so it must
// project NO edge even though it can see both endpoints. Before the fix the OCI
// scope wrote the edge (dead-lettering on the NornicDB fast path) and stamped
// its own scope_id, giving the edge a nondeterministic owner. The retract still
// fires unconditionally, scoped to this intent's own evidence_source.
func TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageDerivedFromEdgeWriter{}
	h := ContainerImageIdentityHandler{DerivedFromEdgeWriter: writer}
	// Both endpoints are visible (anchored to a git repository) but the intent
	// scope is an OCI registry, not that repository.
	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:child", SourceRepositoryIDs: []string{"repository:repo-1"}, BuildProvenanceRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
		{Digest: "sha256:base", BaseImageForRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageDerivedFromEdges(
		context.Background(),
		Intent{ScopeID: "oci_registry:ghcr.io:team:api", GenerationID: "gen-1"},
		decisions,
	); err != nil {
		t.Fatalf("projectContainerImageDerivedFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1 (retract still fires for a non-owning scope)", len(writer.retractCalls))
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows = %#v, want none -- a non-repository scope owns no Dockerfile and must not write", writer.writeRows)
	}
}

// TestProjectContainerImageDerivedFromEdgesRetractsEvenWhenNoRowsToWrite is the
// stale-edge guard: a generation that stops being attributable -- a second
// Dockerfile added, making the repository ambiguous -- must still clear the
// edge the previous generation wrote.
func TestProjectContainerImageDerivedFromEdgesRetractsEvenWhenNoRowsToWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageDerivedFromEdgeWriter{}
	h := ContainerImageIdentityHandler{DerivedFromEdgeWriter: writer}

	if err := h.projectContainerImageDerivedFromEdges(
		context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-2"}, nil,
	); err != nil {
		t.Fatalf("projectContainerImageDerivedFromEdges returned error: %v", err)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("retractCalls = %d, want 1 even with nothing to write", len(writer.retractCalls))
	}
	if len(writer.writeRows) != 0 {
		t.Fatalf("writeRows = %#v, want none for an empty projection", writer.writeRows)
	}
}

func TestProjectContainerImageDerivedFromEdgesPropagatesWriterErrors(t *testing.T) {
	t.Parallel()

	decisions := []ContainerImageIdentityDecision{
		{Digest: "sha256:child", SourceRepositoryIDs: []string{"repository:repo-1"}, BuildProvenanceRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
		{Digest: "sha256:base", BaseImageForRepositoryIDs: []string{"repository:repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}
	intent := Intent{ScopeID: "git-repository-scope:repository:repo-1", GenerationID: "gen-1"}

	writeFail := &recordingContainerImageDerivedFromEdgeWriter{writeErr: errors.New("boom")}
	if err := (ContainerImageIdentityHandler{DerivedFromEdgeWriter: writeFail}).
		projectContainerImageDerivedFromEdges(context.Background(), intent, decisions); err == nil {
		t.Fatal("expected an error when the write fails")
	}

	retractFail := &recordingContainerImageDerivedFromEdgeWriter{retractErr: errors.New("boom")}
	if err := (ContainerImageIdentityHandler{DerivedFromEdgeWriter: retractFail}).
		projectContainerImageDerivedFromEdges(context.Background(), intent, decisions); err == nil {
		t.Fatal("expected an error when the retract fails")
	}
}

// TestContainerImageDerivedFromRowsStayEmptyForCIRunScope is the DERIVED_FROM
// half of the #5829 graph-truth pin (the BUILT_FROM half is in
// container_image_provenance_edges_test.go).
//
// BuildProvenanceRepositoryIDs gates BOTH provenance edges, so widening it in
// applyCIRunDigestRevision widens this edge's child gate too
// (containerImageDerivedFromRows' slices.Contains check). It cannot reach a row
// here, for a reason worth pinning rather than re-deriving: the widened
// provenance only ever lands in the CI-run scope, because ci.run/ci.artifact
// are loaded scope-locally, and a ci_cd_run scope resolves to an EMPTY
// owningRepositoryID -- it owns no Dockerfile, so it has no base to attribute
// from and returns nil before any gate is consulted.
//
// The consequence is the property this asserts: CI-run build provenance can
// never make a DERIVED_FROM edge appear, so the intra-scope widening stays
// confined to BUILT_FROM.
func TestContainerImageDerivedFromRowsStayEmptyForCIRunScope(t *testing.T) {
	t.Parallel()

	const ciScopeID = "ci_cd_run:github_actions:eshu-hq:supply-chain-demo"
	owningRepositoryID := repositoryIDFromReducerScope(ciScopeID)
	if owningRepositoryID != "" {
		t.Fatalf("repositoryIDFromReducerScope(%q) = %q, want empty: a CI-run scope owns no repository", ciScopeID, owningRepositoryID)
	}

	decisions := BuildContainerImageIdentityDecisions(ciDigestProvenanceEnvelopes())
	carriesProvenance := false
	for _, decision := range decisions {
		if slices.Contains(decision.BuildProvenanceRepositoryIDs, ciDigestProvenanceBuildRepo) {
			carriesProvenance = true
			break
		}
	}
	if !carriesProvenance {
		t.Fatalf("no decision carries %q in BuildProvenanceRepositoryIDs; the fixture no longer exercises the widened gate", ciDigestProvenanceBuildRepo)
	}

	if rows := containerImageDerivedFromRows(decisions, owningRepositoryID); rows != nil {
		t.Fatalf("containerImageDerivedFromRows = %#v, want nil for a CI-run scope", rows)
	}
}
