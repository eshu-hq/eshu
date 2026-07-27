// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// dockerfileStagesFact builds the fact shape the Dockerfile parser actually
// emits: a `file` fact (NOT content_entity), source_system git, language
// dockerfile, whose parsed_file_data carries the dockerfile_stages bucket, with
// repo_id anchoring it to the declaring repository. A Dockerfile never becomes a
// content_entity fact -- content_entity carries per-entity data (functions,
// classes, k8s resources), while file-level parse output like dockerfile_stages
// lives on the `file` fact.
func dockerfileStagesFact(factID, repoID, relativePath string, stages ...map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "git-repository-scope:" + repoID,
		GenerationID:     "generation-git",
		FactKind:         factKindFile,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef:        facts.Ref{SourceSystem: "git"},
		Payload: map[string]any{
			"repo_id":       repoID,
			"language":      "dockerfile",
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"dockerfile_stages": stages,
			},
		},
	}
}

// TestExtractContainerImageRefsDockerfileBase proves the #5460 extraction seam:
// a Dockerfile's FROM base becomes a container image reference anchored as that
// repository's declared base, and NOT as one of the repository's built images.
// The distinction is the whole point -- the base arrives on the declaring
// repository's own content_entity envelope, so without a separate anchor it
// would be indistinguishable from an image that repository builds.
func TestExtractContainerImageRefsDockerfileBase(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		dockerfileStagesFact(
			"fact-dockerfile", "repository:team-api", "Dockerfile",
			map[string]any{
				"stage_index": 0,
				"base_image":  "registry.example.com/team/base",
				"base_tag":    "",
				"alias":       "builder",
			},
			map[string]any{
				"stage_index": 1,
				"base_image":  "registry.example.com/team/base@sha256:basedigest",
				"base_tag":    "",
			},
		),
	}

	refs, _, _, err := extractContainerImageRefsWithQuarantine(envelopes, nil, "")
	if err != nil {
		t.Fatalf("extractContainerImageRefsWithQuarantine: %v", err)
	}

	var found *containerImageRefEvidence
	for i := range refs {
		if refs[i].imageRef == "registry.example.com/team/base@sha256:basedigest" {
			found = &refs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("dockerfile base image reference not extracted; got %d refs: %+v", len(refs), refs)
	}
	if got, want := len(found.baseImageForRepositoryIDs), 1; got != want {
		t.Fatalf("baseImageForRepositoryIDs = %v, want exactly %d entry", found.baseImageForRepositoryIDs, want)
	}
	if got, want := found.baseImageForRepositoryIDs[0], "repository:team-api"; got != want {
		t.Fatalf("baseImageForRepositoryIDs[0] = %q, want %q", got, want)
	}
	// The base is declared BY the repository, not built by it. Claiming it as a
	// built image would make it its own child once the DERIVED_FROM projection
	// joins the two sides.
	for _, repositoryID := range found.sourceRepositoryIDs {
		if repositoryID == "repository:team-api" {
			t.Fatalf("dockerfile base image must not be anchored as a built image of its declaring repository: %+v", found)
		}
	}
}

// TestBuildContainerImageIdentityDecisionsDockerfileBase carries the extraction
// through the real classification path and proves the base-image anchor
// survives onto the published decision with an exact_digest outcome -- the two
// conditions the DERIVED_FROM projection requires.
func TestBuildContainerImageIdentityDecisionsDockerfileBase(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		dockerfileStagesFact(
			"fact-dockerfile", "repository:team-api", "Dockerfile",
			map[string]any{
				"stage_index": 0,
				"base_image":  "registry.example.com/team/api@sha256:abc",
				"base_tag":    "",
			},
		),
		ociManifestFact("fact-manifest", "sha256:abc"),
	}

	decisions := BuildContainerImageIdentityDecisions(envelopes)

	var base *ContainerImageIdentityDecision
	for i := range decisions {
		if len(decisions[i].BaseImageForRepositoryIDs) > 0 {
			base = &decisions[i]
			break
		}
	}
	if base == nil {
		t.Fatalf("no decision carried a base-image repository anchor; decisions: %+v", decisions)
	}
	if got, want := base.Outcome, ContainerImageIdentityExactDigest; got != want {
		t.Fatalf("base decision Outcome = %q, want %q (decision %+v)", got, want, base)
	}
	if got, want := base.Digest, "sha256:abc"; got != want {
		t.Fatalf("base decision Digest = %q, want %q", got, want)
	}
	if got, want := base.BaseImageForRepositoryIDs[0], "repository:team-api"; got != want {
		t.Fatalf("base decision BaseImageForRepositoryIDs[0] = %q, want %q", got, want)
	}
}

// TestExtractContainerImageRefsDockerfileBaseUnresolved proves an
// ARG-parameterized FROM never becomes a reference at all. It must not reach
// classification as a literal "${BASE_IMAGE}" image, which would publish a
// junk unresolved decision keyed on an un-expanded shell variable.
func TestExtractContainerImageRefsDockerfileBaseUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		dockerfileStagesFact(
			"fact-dockerfile", "repository:team-api", "Dockerfile",
			map[string]any{
				"stage_index": 0,
				"base_image":  "${BASE_IMAGE}",
				"base_tag":    "",
			},
		),
	}

	refs, _, _, err := extractContainerImageRefsWithQuarantine(envelopes, nil, "")
	if err != nil {
		t.Fatalf("extractContainerImageRefsWithQuarantine: %v", err)
	}
	for _, ref := range refs {
		if len(ref.baseImageForRepositoryIDs) > 0 {
			t.Fatalf("ARG-parameterized FROM must not produce a base image reference, got %+v", ref)
		}
	}
}
