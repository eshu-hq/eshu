// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDeltaMaterializationRejectsMalformedTombstonedDirectory(t *testing.T) {
	t.Parallel()

	gen1 := offlineTierGeneration("gen1", []facts.Envelope{
		offlineTierRepositoryFact("replay-delta-malformed"),
		offlineTierDirectoryFact("/repos/replay-delta-malformed/keep", "keep", false),
	})
	gen2 := offlineTierGeneration("gen2", []facts.Envelope{
		offlineTierRepositoryFact("replay-delta-malformed"),
		offlineTierDirectoryFact("/repos/replay-delta-malformed/keep", "keep", false),
		{
			FactKind:    factKindDirectory,
			IsTombstone: true,
			Payload: map[string]any{
				"name":        "removed",
				"parent_path": "/repos/replay-delta-malformed",
				"repo_id":     "replay-delta-malformed",
				"depth":       0,
			},
		},
	})

	_, err := DeltaMaterializationFromGenerations(gen1, gen2)
	if err == nil {
		t.Fatal("DeltaMaterializationFromGenerations error = nil, want malformed tombstone error")
	}
	if !strings.Contains(err.Error(), `missing required field "path"`) {
		t.Fatalf("DeltaMaterializationFromGenerations error = %v, want missing path", err)
	}
}

func offlineTierGeneration(generationID string, envs []facts.Envelope) collector.CollectedGeneration {
	s := scope.IngestionScope{
		ScopeID:       "git:repository:replay-delta-malformed",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "git:repository:replay-delta-malformed",
	}
	g := scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      s.ScopeID,
	}
	return collector.FactsFromSlice(s, g, envs)
}

func offlineTierRepositoryFact(repoID string) facts.Envelope {
	return facts.Envelope{
		FactKind: factKindRepository,
		Payload: map[string]any{
			"repo_id": repoID,
			"name":    repoID,
			"path":    "/repos/" + repoID,
		},
	}
}

func offlineTierDirectoryFact(path, name string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactKind:    factKindDirectory,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"path":        path,
			"name":        name,
			"parent_path": "/repos/replay-delta-malformed",
			"repo_id":     "replay-delta-malformed",
			"depth":       0,
		},
	}
}
