// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// dockerfileFileEnvelope builds the `file` fact the Dockerfile parser emits: the
// base image FROM refs live in parsed_file_data.dockerfile_stages, never on a
// content_entity fact (#5460).
func dockerfileFileEnvelope(factID, scopeID, generationID string, stages []map[string]any) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repository:github.com/example/lineage-app",
		"path":          "/repo/Dockerfile",
		"relative_path": "Dockerfile",
		"name":          "Dockerfile",
		"language":      "dockerfile",
	}
	if stages != nil {
		payload["parsed_file_data"] = map[string]any{"dockerfile_stages": stages}
	}
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      scopeID,
		GenerationID: generationID,
		FactKind:     FactKindFileObserved,
		ObservedAt:   time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		SourceRef:    facts.Ref{SourceSystem: "git"},
		Payload:      payload,
	}
}

func dockerfileLineageScope() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      "repo://github.com/example/lineage-app",
		ScopeKind:    "git_repository",
		SourceSystem: "git",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "dockerfile-generation-1",
		ObservedAt:   time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.July, 20, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	return scopeValue, generation
}

// TestBuildProjectionQueuesContainerImageIdentityForDockerfileBaseImage pins the
// enqueue half of base-image lineage (#5460). The reducer extracts a Dockerfile
// FROM base from the repository's `file` fact, but that extraction only runs
// inside a container_image_identity intent. When a Dockerfile is the ONLY
// identity-relevant fact in a generation -- a repository that adds or edits its
// Dockerfile with no new image evidence -- nothing else triggers the domain, so
// without a `file` trigger the lineage would never project and a changed base
// would leave the prior DERIVED_FROM edge stale.
func TestBuildProjectionQueuesContainerImageIdentityForDockerfileBaseImage(t *testing.T) {
	t.Parallel()

	scopeValue, generation := dockerfileLineageScope()
	envelopes := []facts.Envelope{
		dockerfileFileEnvelope("fact-dockerfile-1", scopeValue.ScopeID, generation.GenerationID, []map[string]any{
			{
				"stage_index": 0,
				"base_image":  "ghcr.io/eshu-hq/base-runtime@sha256:ba5e",
			},
		}),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	var identityIntents []ReducerIntent
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			identityIntents = append(identityIntents, intent)
		}
	}
	if got, want := len(identityIntents), 1; got != want {
		t.Fatalf("container image identity intents = %d, want %d: a Dockerfile-only generation must enqueue the identity intent that extracts its base image (#5460)", got, want)
	}
	intent := identityIntents[0]
	if got, want := intent.EntityKey, "container_image_identity:"+scopeValue.ScopeID; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-dockerfile-1"; got != want {
		t.Fatalf("intent.FactID = %q, want the Dockerfile file fact", got)
	}
}

// TestBuildProjectionDoesNotQueueContainerImageIdentityForNonDockerfileFile
// keeps the new trigger narrow. Every repository generation carries `file`
// facts; triggering the identity domain on all of them would enqueue an intent
// for every source file in the corpus, so only a Dockerfile carrying parsed
// base-image stages may trigger.
func TestBuildProjectionDoesNotQueueContainerImageIdentityForNonDockerfileFile(t *testing.T) {
	t.Parallel()

	scopeValue, generation := dockerfileLineageScope()
	goFile := dockerfileFileEnvelope("fact-go-file-1", scopeValue.ScopeID, generation.GenerationID, nil)
	goFile.Payload["language"] = "go"
	goFile.Payload["path"] = "/repo/main.go"
	goFile.Payload["relative_path"] = "main.go"
	goFile.Payload["name"] = "main.go"
	goFile.Payload["parsed_file_data"] = map[string]any{}

	projection, err := buildProjection(scopeValue, generation, []facts.Envelope{goFile})
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}

	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainContainerImageIdentity {
			t.Fatalf("a non-Dockerfile file fact queued a container image identity intent (fact %q)", intent.FactID)
		}
	}
}
