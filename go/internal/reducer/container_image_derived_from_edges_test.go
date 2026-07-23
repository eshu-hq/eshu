// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"reflect"
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

	child := func(digest string, repos ...string) ContainerImageIdentityDecision {
		return ContainerImageIdentityDecision{
			ImageRef:            "child" + digest,
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
		decisions []ContainerImageIdentityDecision
		want      []map[string]any
	}{
		{
			name: "single base and single child projects one edge",
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
			name: "two children of one base each get an edge",
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
			name: "the same base digest declared by two Dockerfiles is still one base",
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
			name: "two distinct bases in one repository project nothing",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-a"),
				base("sha256:base1", "repo-a"),
				base("sha256:base2", "repo-a"),
			},
			want: nil,
		},
		{
			name: "a non-exact base projects nothing",
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
			name: "a non-exact child projects nothing",
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
			name: "a base in one repository never attaches to another repository's child",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:child", "repo-b"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name: "an image that is its own base projects no self-loop",
			decisions: []ContainerImageIdentityDecision{
				child("sha256:same", "repo-a"),
				base("sha256:same", "repo-a"),
			},
			want: nil,
		},
		{
			name: "a base with no child in its repository projects nothing",
			decisions: []ContainerImageIdentityDecision{
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name: "an empty digest never projects",
			decisions: []ContainerImageIdentityDecision{
				child("", "repo-a"),
				base("sha256:base", "repo-a"),
			},
			want: nil,
		},
		{
			name:      "no decisions project nothing",
			decisions: nil,
			want:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := containerImageDerivedFromRows(tc.decisions)
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
		{Digest: "sha256:child", SourceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
		{Digest: "sha256:base", BaseImageForRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}

	if err := h.projectContainerImageDerivedFromEdges(
		context.Background(), Intent{ScopeID: "scope-1", GenerationID: "gen-1"}, decisions,
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
		{Digest: "sha256:child", SourceRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
		{Digest: "sha256:base", BaseImageForRepositoryIDs: []string{"repo-1"}, Outcome: ContainerImageIdentityExactDigest},
	}
	intent := Intent{ScopeID: "scope-1", GenerationID: "gen-1"}

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
