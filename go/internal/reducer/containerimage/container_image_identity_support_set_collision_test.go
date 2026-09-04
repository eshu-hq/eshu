// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"reflect"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

func TestBuildContainerImageIdentitySupportSetPreservesConvergedEvidence(t *testing.T) {
	t.Parallel()

	const digest = "sha256:5704570457045704570457045704570457045704570457045704570457045704"
	imageRef := "registry.example.com/team/app@" + digest
	runtime := ContainerImageIdentityDecision{
		ImageRef:            imageRef,
		Digest:              digest,
		RepositoryID:        "repository:runtime",
		SourceRepositoryIDs: []string{"repository:runtime"},
		Outcome:             reducercontract.ContainerImageIdentityExactDigest,
		CanonicalWrites:     1,
		EvidenceFactIDs:     []string{"aws-image-reference", "oci-manifest"},
		IdentityStrength:    "explicit_digest",
	}
	artifact := runtime
	artifact.RepositoryID = "repository:ci"
	artifact.SourceRepositoryIDs = []string{"repository:ci"}
	artifact.EvidenceFactIDs = []string{"ci-artifact", "ci-run", "oci-manifest"}
	artifact.IdentityStrength = "artifact_digest_with_registry_observation"

	forward, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:   "repository:runtime",
		Decisions: []ContainerImageIdentityDecision{runtime, artifact},
	}, nil)
	if err != nil {
		t.Fatalf("build converged support set: %v", err)
	}
	reverse, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:   "repository:runtime",
		Decisions: []ContainerImageIdentityDecision{artifact, runtime},
	}, nil)
	if err != nil {
		t.Fatalf("build reversed converged support set: %v", err)
	}

	if got, want := len(forward.Supports), 2; got != want {
		t.Fatalf("supports = %#v, want %d independent supports", forward.Supports, want)
	}
	if got, want := forward.CurrentSupportCount, 2; got != want {
		t.Fatalf("current support count = %d, want %d", got, want)
	}
	if !reflect.DeepEqual(forward.Supports, reverse.Supports) ||
		!reflect.DeepEqual(forward.SetID, reverse.SetID) ||
		!reflect.DeepEqual(forward.ContentHash, reverse.ContentHash) {
		t.Fatalf("support set changed with input order:\nforward=%#v\nreverse=%#v", forward, reverse)
	}
	assertContainerImageSupportEvidence(t, forward.Supports, "explicit_digest", []string{
		"aws-image-reference", "oci-manifest",
	})
	assertContainerImageSupportEvidence(t, forward.Supports, "artifact_digest_with_registry_observation", []string{
		"ci-artifact", "ci-run", "oci-manifest",
	})
	assertContainerImageSupportRepository(t, forward.Supports, "explicit_digest", "repository:runtime")
	assertContainerImageSupportRepository(t, forward.Supports, "artifact_digest_with_registry_observation", "repository:ci")
}

func TestBuildContainerImageIdentitySupportSetDeduplicatesSemanticSupports(t *testing.T) {
	t.Parallel()

	decision := ContainerImageIdentityDecision{
		ImageRef:         "registry.example.com/team/app@sha256:aaaaaaaa",
		Digest:           "sha256:aaaaaaaa",
		RepositoryID:     "repository:example",
		Outcome:          reducercontract.ContainerImageIdentityExactDigest,
		CanonicalWrites:  1,
		EvidenceFactIDs:  []string{"fact-b", "fact-a", "fact-a"},
		IdentityStrength: "explicit_digest",
	}
	duplicate := decision
	duplicate.EvidenceFactIDs = []string{"fact-a", "fact-b"}

	set, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:   "repository:example",
		Decisions: []ContainerImageIdentityDecision{decision, duplicate},
	}, nil)
	if err != nil {
		t.Fatalf("build duplicate support set: %v", err)
	}
	if got, want := len(set.Supports), 1; got != want {
		t.Fatalf("supports = %#v, want %d semantic support", set.Supports, want)
	}
	if got, want := set.CurrentSupportCount, 1; got != want {
		t.Fatalf("current support count = %d, want %d semantic support", got, want)
	}
}

func TestBuildContainerImageIdentitySupportSetKeepsExactAndTagEvidenceSeparate(t *testing.T) {
	t.Parallel()

	const imageRef = "registry.example.com/team/app:prod"
	exact := ContainerImageIdentityDecision{
		ImageRef:            imageRef,
		Digest:              "sha256:exact",
		SourceRepositoryIDs: []string{"repository:build"},
		Outcome:             reducercontract.ContainerImageIdentityExactDigest,
		CanonicalWrites:     1,
		EvidenceFactIDs:     []string{"exact-fact"},
		IdentityStrength:    "explicit_digest",
	}
	tag := ContainerImageIdentityDecision{
		ImageRef:            imageRef,
		Digest:              "sha256:tag",
		SourceRepositoryIDs: []string{"repository:build", "repository:deploy"},
		Outcome:             reducercontract.ContainerImageIdentityTagResolved,
		CanonicalWrites:     1,
		EvidenceFactIDs:     []string{"tag-fact"},
		IdentityStrength:    "tag_observation_with_digest",
	}
	tombstone := tag
	tombstone.CanonicalWrites = 0

	set, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:            "repository:example",
		Decisions:          []ContainerImageIdentityDecision{tag, exact},
		TombstoneDecisions: []ContainerImageIdentityDecision{tombstone},
	}, nil)
	if err != nil {
		t.Fatalf("build exact/tag support set: %v", err)
	}
	if got, want := len(set.Supports), 2; got != want {
		t.Fatalf("supports = %#v, want %d independent supports", set.Supports, want)
	}
	for _, support := range set.Supports {
		switch support.Outcome {
		case string(reducercontract.ContainerImageIdentityExactDigest):
			if !reflect.DeepEqual(support.SourceRepositoryIDs, []string{"repository:build"}) {
				t.Fatalf("exact source repositories = %v, want exact-only attribution", support.SourceRepositoryIDs)
			}
		case string(reducercontract.ContainerImageIdentityTagResolved):
			if !reflect.DeepEqual(support.SourceRepositoryIDs, []string{"repository:build", "repository:deploy"}) {
				t.Fatalf("tag source repositories = %v, want tag-only attribution", support.SourceRepositoryIDs)
			}
		default:
			t.Fatalf("unexpected support outcome %q", support.Outcome)
		}
	}
}

func assertContainerImageSupportEvidence(
	t *testing.T,
	supports []containerImageIdentitySupport,
	strength string,
	want []string,
) {
	t.Helper()
	for _, support := range supports {
		if support.IdentityStrength != strength {
			continue
		}
		if !reflect.DeepEqual(support.EvidenceFactIDs, want) {
			t.Fatalf("%s evidence = %v, want %v", strength, support.EvidenceFactIDs, want)
		}
		return
	}
	t.Fatalf("support strength %q not found in %#v", strength, supports)
}

func assertContainerImageSupportRepository(
	t *testing.T,
	supports []containerImageIdentitySupport,
	strength string,
	want string,
) {
	t.Helper()
	for _, support := range supports {
		if support.IdentityStrength != strength {
			continue
		}
		if support.RepositoryID != want || !reflect.DeepEqual(support.SourceRepositoryIDs, []string{want}) {
			t.Fatalf("%s repository attribution = %q %v, want %q", strength, support.RepositoryID, support.SourceRepositoryIDs, want)
		}
		return
	}
	t.Fatalf("support strength %q not found in %#v", strength, supports)
}
