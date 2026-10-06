// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildInvokesCloudActionIntentRowsIgnoresSameNamedFileInAnotherRepo is
// the #7640 regression for cloud actions: a top-level SDK call in one file
// must not become an INVOKES_CLOUD_ACTION edge for a function that lives in
// a same-named file of another repository.
func TestBuildInvokesCloudActionIntentRowsIgnoresSameNamedFileInAnotherRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pathA string
		pathB string
	}{
		{name: "same bare name, different relative path", pathA: "svc/main.go", pathB: "cmd/main.go"},
		{name: "same relative path", pathA: "cmd/main.go", pathB: "cmd/main.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelopes := []facts.Envelope{
				invokesCloudActionRepoEnvelope("repo-a"),
				invokesCloudActionRepoEnvelope("repo-b"),
				invokesCloudActionFileEnvelope("repo-a", tt.pathA, callerFunction("repo-a:handler"), nil),
				invokesCloudActionFileEnvelope("repo-b", tt.pathB, nil, []map[string]any{
					{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
				}),
			}

			intents, unresolved := buildInvokesCloudActionIntentsWithCountForTest(t, envelopes)
			if len(intents) != 0 {
				t.Fatalf("expected no INVOKES_CLOUD_ACTION intent for a top-level call, got %d: %v", len(intents), intents[0].Payload)
			}
			// The dropped call is counted, so an operator watching the
			// INVOKES_CLOUD_ACTION edge sees the drop (#7640).
			if unresolved != 1 {
				t.Fatalf("unresolved cloud-action callers = %d, want 1", unresolved)
			}
		})
	}
}

// TestBuildInvokesCloudActionIntentRowsDoesNotCountResolvedCallers keeps the
// counter honest: a call inside a function of its own file resolves, emits an
// intent, and adds nothing to the unresolved count.
func TestBuildInvokesCloudActionIntentRowsDoesNotCountResolvedCallers(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		invokesCloudActionRepoEnvelope("repo-a"),
		invokesCloudActionFileEnvelope("repo-a", "cmd/main.go", callerFunction("repo-a:handler"), []map[string]any{
			{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
		}),
	}

	intents, unresolved := buildInvokesCloudActionIntentsWithCountForTest(t, envelopes)
	if len(intents) != 1 {
		t.Fatalf("expected one INVOKES_CLOUD_ACTION intent, got %d", len(intents))
	}
	if unresolved != 0 {
		t.Fatalf("unresolved cloud-action callers = %d, want 0", unresolved)
	}
}
