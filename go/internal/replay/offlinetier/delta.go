// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

// DeltaMaterialization is the result of applying one generation on top of another.
// It carries the CanonicalMaterialization for writing gen2 into the graph (which
// retract logic will fire because FirstGeneration=false) plus the set of directory
// paths that gen2 removes (tombstoned) so callers can assert post-delta graph truth.
type DeltaMaterialization struct {
	// Gen1 is the canonical materialization for the baseline (first) generation.
	// The caller writes it first to establish graph state before gen2 retracts.
	// It is returned here (rather than re-derived by the caller) so gen1's fact
	// stream is drained exactly once — a CollectedGeneration's fact channel is
	// closed after a single range, so draining it twice yields an empty stream.
	Gen1 projector.CanonicalMaterialization
	// Gen2 is the canonical materialization for the second generation. Its
	// FirstGeneration field is false, which enables the production retract
	// statements (canonicalNodeRetractDirectoriesCypher etc.) to fire and
	// remove entities that are absent from or tombstoned in gen2.
	Gen2 projector.CanonicalMaterialization
	// TombstonedDirectoryPaths is the set of directory paths that appear as
	// tombstones in gen2 and therefore must be ABSENT from the graph after
	// the gen2 write. This is the retraction assertion contract: the caller
	// proves removal by reading back count=0 for every path in this set.
	TombstonedDirectoryPaths []string
}

// DeltaMaterializationFromGenerations builds the delta materialization for a
// multi-generation cassette scenario. It drains gen1 to establish the baseline
// materialization (used only to validate the cassette is structurally sane) and
// drains gen2 to build the canonical projection input for the second generation.
//
// The gen2 CanonicalMaterialization is returned with FirstGeneration=false so
// the production retract phase fires: directories absent from gen2 (including
// tombstoned ones) are DETACH DELETE'd from the graph by the production
// canonicalNodeRetractDirectoriesCypher Cypher template.
//
// It returns an error when either generation's fact stream errors, when gen1
// has no repository fact (bad baseline cassette), or when gen2's surviving
// (non-tombstoned) directory set would be empty and the repository is present
// (which would indicate a cassette that silently drops everything).
//
// The returned TombstonedDirectoryPaths list the directories explicitly
// tombstoned in gen2 so callers can verify they are gone after the write.
func DeltaMaterializationFromGenerations(
	gen1 collector.CollectedGeneration,
	gen2 collector.CollectedGeneration,
) (DeltaMaterialization, error) {
	// Drain gen1 to validate the baseline cassette. We do not write gen1 here;
	// the caller writes gen1 first so the graph has state before gen2 retracts.
	gen1Envs, err := drainGeneration(gen1)
	if err != nil {
		return DeltaMaterialization{}, fmt.Errorf("gen1 fact stream: %w", err)
	}
	gen1Mat, err := materializationFromEnvelopes(gen1.Scope.ScopeID, gen1.Generation.GenerationID, gen1Envs)
	if err != nil {
		return DeltaMaterialization{}, fmt.Errorf("gen1 materialization: %w", err)
	}
	// Validate gen1 is a usable baseline.
	if gen1Mat.Repository == nil {
		return DeltaMaterialization{}, fmt.Errorf("gen1 has no repository fact — cassette is invalid")
	}

	// Drain gen2 and separate tombstoned facts from surviving ones.
	gen2Envs, err := drainGeneration(gen2)
	if err != nil {
		return DeltaMaterialization{}, fmt.Errorf("gen2 fact stream: %w", err)
	}

	// Split gen2 facts: surviving (non-tombstoned) go into the materialization;
	// tombstoned directory facts yield the TombstonedDirectoryPaths list.
	var tombstonedDirPaths []string
	for _, env := range gen2Envs {
		if env.IsTombstone && env.FactKind == factKindDirectory {
			if p, ok := env.Payload["path"].(string); ok && p != "" {
				tombstonedDirPaths = append(tombstonedDirPaths, p)
			}
		}
	}

	// Build gen2 materialization from surviving (non-tombstoned) envelopes only.
	// Tombstoned facts must not appear as surviving rows — the production
	// buildRetractStatements removes their graph nodes via the retract phase, so
	// including them in the materialization write would resurrect them.
	survivingGen2Envs := make([]facts.Envelope, 0, len(gen2Envs))
	for _, env := range gen2Envs {
		if !env.IsTombstone {
			survivingGen2Envs = append(survivingGen2Envs, env)
		}
	}

	gen2Mat, err := materializationFromEnvelopes(gen2.Scope.ScopeID, gen2.Generation.GenerationID, survivingGen2Envs)
	if err != nil {
		return DeltaMaterialization{}, fmt.Errorf("gen2 materialization: %w", err)
	}

	// Guard against a cassette that silently drops everything: a gen2 with a
	// repository but zero surviving directories would, once written with
	// FirstGeneration=false, retract every directory in the graph (the retract
	// Cypher deletes all repo directories not in an empty path list). Fail loudly
	// rather than silently full-wipe.
	if gen2Mat.Repository != nil && len(gen2Mat.Directories) == 0 {
		return DeltaMaterialization{}, fmt.Errorf(
			"gen2 has a repository but no surviving directories — refusing to build a delta that would retract every directory")
	}

	// Mark gen2 as a subsequent generation so the production retract phase fires.
	// FirstGeneration=true would skip all retraction, which is the broken-retraction
	// control tested by TestDeltaTombstoneNegativeControlBrokenRetraction.
	gen2Mat.FirstGeneration = false

	return DeltaMaterialization{
		Gen1:                     gen1Mat,
		Gen2:                     gen2Mat,
		TombstonedDirectoryPaths: tombstonedDirPaths,
	}, nil
}

// drainGeneration consumes the fact channel from a CollectedGeneration and
// returns all envelopes. It returns an error if the stream itself errored.
func drainGeneration(gen collector.CollectedGeneration) ([]facts.Envelope, error) {
	envs := make([]facts.Envelope, 0, gen.FactCount)
	for env := range gen.Facts {
		envs = append(envs, env)
	}
	if gen.FactStreamErr != nil {
		if err := gen.FactStreamErr(); err != nil {
			return nil, fmt.Errorf("fact stream: %w", err)
		}
	}
	return envs, nil
}
