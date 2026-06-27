// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestPlatformInfraMaterializationHandlerLocksInfrastructurePlatformIDs(t *testing.T) {
	t.Parallel()

	loader := &recordingKindFactLoader{
		byKind: []facts.Envelope{
			{
				ScopeID:  "scope-1",
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":   "repo:infra",
					"repo_name": "infra",
				},
			},
			{
				ScopeID:  "scope-1",
				FactKind: "parsed_file_data",
				Payload: map[string]any{
					"terraform_resources": []any{
						map[string]any{
							"resource_type": "aws_ecs_cluster",
							"resource_name": "node10",
						},
					},
				},
			},
		},
	}
	graphExecutor := &recordingCypherExecutor{}
	locker := &recordingPlatformGraphLocker{}
	handler := PlatformInfraMaterializationHandler{
		FactLoader:                 loader,
		InfrastructureMaterializer: NewInfrastructurePlatformMaterializer(graphExecutor),
		PlatformGraphLocker:        locker,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-platform-lock",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		Domain:       DomainPlatformInfraMaterialization,
		EnqueuedAt:   time.Now(),
		AvailableAt:  time.Now(),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	wantLocks := []string{"platform:ecs:aws:cluster/node10:none:none"}
	if !slices.Equal(locker.platformIDs, wantLocks) {
		t.Fatalf("platform locks = %v, want %v", locker.platformIDs, wantLocks)
	}
	if got, want := len(graphExecutor.calls), 1; got != want {
		t.Fatalf("graph write count = %d, want %d", got, want)
	}
	if graphExecutor.calls[0].cypher == "" {
		t.Fatal("graph write cypher is empty")
	}
}

type recordingPlatformGraphLocker struct {
	platformIDs []string
}

func (l *recordingPlatformGraphLocker) WithPlatformLocks(
	ctx context.Context,
	platformIDs []string,
	fn func(context.Context) error,
) error {
	l.platformIDs = append([]string(nil), platformIDs...)
	return fn(ctx)
}
