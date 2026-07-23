// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
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
