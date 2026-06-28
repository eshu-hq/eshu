// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier

import (
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// MaterializationFromGeneration drains a replayed CollectedGeneration's fact
// stream and builds the canonical projection input for its scope generation. It
// is the exported cassette -> projector seam the offline replay tier drives: the
// rows it returns are written verbatim by the production
// storage/cypher.CanonicalNodeWriter against a real graph backend.
//
// It returns an error when the generation carries no repository fact or any
// fact is malformed, so a bad cassette fails loudly rather than projecting an
// empty graph that would look green.
func MaterializationFromGeneration(gen collector.CollectedGeneration) (projector.CanonicalMaterialization, error) {
	envelopes := make([]facts.Envelope, 0, gen.FactCount)
	for env := range gen.Facts {
		envelopes = append(envelopes, env)
	}
	if gen.FactStreamErr != nil {
		if err := gen.FactStreamErr(); err != nil {
			return projector.CanonicalMaterialization{}, fmt.Errorf("fact stream error: %w", err)
		}
	}
	return materializationFromEnvelopes(gen.Scope.ScopeID, gen.Generation.GenerationID, envelopes)
}

// Cassette fact kinds the offline tier maps into a canonical materialization.
// These are the durable fact-kind labels carried by the committed
// nested-directory-tree cassette; the cassette format is collector-agnostic, so
// the tier owns the fact-kind -> materialization-row mapping here rather than
// pulling in the full git collector.
const (
	factKindRepository = "git.repository"
	factKindDirectory  = "git.directory"
)

// materializationFromEnvelopes builds the canonical projection input for one
// scope generation from its replayed fact envelopes. It maps each repository
// fact to the Repository row and each directory fact to a DirectoryRow,
// preserving the depth ordering the canonical writer relies on. It returns an
// error for malformed facts (missing required keys) so a bad cassette fails
// loudly instead of projecting an empty graph and looking green.
//
// The function is the cassette -> projector seam exercised by the offline tier:
// the rows it produces are written verbatim by the production
// storage/cypher.CanonicalNodeWriter.
func materializationFromEnvelopes(
	scopeID, generationID string,
	envelopes []facts.Envelope,
) (projector.CanonicalMaterialization, error) {
	mat := projector.CanonicalMaterialization{
		ScopeID:         scopeID,
		GenerationID:    generationID,
		FirstGeneration: true,
	}

	for i, env := range envelopes {
		switch env.FactKind {
		case factKindRepository:
			repo, err := repositoryRowFromPayload(env.Payload)
			if err != nil {
				return projector.CanonicalMaterialization{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			if mat.Repository != nil {
				return projector.CanonicalMaterialization{}, fmt.Errorf("fact[%d] %s: duplicate repository fact", i, env.FactKind)
			}
			mat.Repository = &repo
			mat.RepoID = repo.RepoID
			mat.RepoPath = repo.Path
		case factKindDirectory:
			dir, err := directoryRowFromPayload(env.Payload)
			if err != nil {
				return projector.CanonicalMaterialization{}, fmt.Errorf("fact[%d] %s: %w", i, env.FactKind, err)
			}
			mat.Directories = append(mat.Directories, dir)
		default:
			return projector.CanonicalMaterialization{}, fmt.Errorf("fact[%d]: unsupported fact_kind %q for offline tier", i, env.FactKind)
		}
	}

	if mat.Repository == nil {
		return projector.CanonicalMaterialization{}, fmt.Errorf("cassette generation %q has no repository fact", generationID)
	}

	// Canonical directory writes are ordered root-first by depth so a parent node
	// is MERGE'd before any child references it. Sort here so the tier does not
	// depend on the cassette author keeping facts in depth order.
	sort.SliceStable(mat.Directories, func(a, b int) bool {
		return mat.Directories[a].Depth < mat.Directories[b].Depth
	})

	return mat, nil
}

func repositoryRowFromPayload(payload map[string]any) (projector.RepositoryRow, error) {
	repoID, err := requireString(payload, "repo_id")
	if err != nil {
		return projector.RepositoryRow{}, err
	}
	name, err := requireString(payload, "name")
	if err != nil {
		return projector.RepositoryRow{}, err
	}
	path, err := requireString(payload, "path")
	if err != nil {
		return projector.RepositoryRow{}, err
	}
	return projector.RepositoryRow{
		RepoID:    repoID,
		Name:      name,
		Path:      path,
		LocalPath: path,
	}, nil
}

func directoryRowFromPayload(payload map[string]any) (projector.DirectoryRow, error) {
	path, err := requireString(payload, "path")
	if err != nil {
		return projector.DirectoryRow{}, err
	}
	name, err := requireString(payload, "name")
	if err != nil {
		return projector.DirectoryRow{}, err
	}
	parentPath, err := requireString(payload, "parent_path")
	if err != nil {
		return projector.DirectoryRow{}, err
	}
	repoID, err := requireString(payload, "repo_id")
	if err != nil {
		return projector.DirectoryRow{}, err
	}
	depth, err := requireInt(payload, "depth")
	if err != nil {
		return projector.DirectoryRow{}, err
	}
	return projector.DirectoryRow{
		Path:       path,
		Name:       name,
		ParentPath: parentPath,
		RepoID:     repoID,
		Depth:      depth,
	}, nil
}

// requireString reads a non-empty string payload field or returns an error.
func requireString(payload map[string]any, key string) (string, error) {
	raw, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("missing required field %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("field %q is %T, want string", key, raw)
	}
	if s == "" {
		return "", fmt.Errorf("field %q is empty", key)
	}
	return s, nil
}

// requireInt reads an integer payload field, accepting the float64 JSON decodes
// numbers into as well as the native int types.
func requireInt(payload map[string]any, key string) (int, error) {
	raw, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("missing required field %q", key)
	}
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("field %q is %T, want number", key, raw)
	}
}
