// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/projector/decode"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func buildRepositoryRefs(fact facts.Envelope) []content.RepositoryRef {
	if decode.NormalizeFactKind(fact.FactKind) != "repository" || fact.IsTombstone {
		return nil
	}
	repository, err := decode.CodegraphRepository(fact)
	if err != nil || len(repository.GitRefs) == 0 {
		return nil
	}

	defaultBranch := decode.CodegraphDerefString(repository.DefaultBranch)
	refsByKey := make(map[string]content.RepositoryRef, len(repository.GitRefs))
	for _, entry := range repository.GitRefs {
		if entry.Name == "" || entry.HeadSHA == "" {
			continue
		}
		kind := entry.Kind
		if kind == "" {
			kind = "branch"
		}
		isDefault := entry.IsDefault || (kind == "branch" && entry.Name == defaultBranch)
		key := kind + "\x00" + entry.Name
		refsByKey[key] = content.RepositoryRef{
			Name:       entry.Name,
			Kind:       kind,
			HeadSHA:    entry.HeadSHA,
			Default:    isDefault,
			ObservedAt: fact.ObservedAt,
		}
	}

	keys := make([]string, 0, len(refsByKey))
	for key := range refsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := refsByKey[keys[i]]
		right := refsByKey[keys[j]]
		if left.Default != right.Default {
			return left.Default
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Name < right.Name
	})

	refs := make([]content.RepositoryRef, 0, len(keys))
	for _, key := range keys {
		refs = append(refs, refsByKey[key])
	}
	return refs
}

// contentFullSnapshot reports whether the content records built for this
// generation are the COMPLETE file set of the repository, so the content writer
// may remove every stored path the generation does not carry (#7447 item 5).
//
// It is true only when all of these hold, and the writer's reap deletes data,
// so every one is required:
//
//   - the scope is a repository snapshot scope. A terraform_state scope can
//     carry repo_id in its metadata while emitting no file facts, so an empty
//     Records there would otherwise mean "delete the repository's content";
//   - the generation is not a delta by the generation flag;
//   - the generation carries the repository fact, positive evidence that this
//     is the collector's snapshot of the repository rather than a partial or
//     empty fact load; and
//   - that fact does not declare delta_generation. This is the same marker
//     canonical.BuildMaterialization reads (extractDeltaProjectionScope) to
//     choose a delta retract over a full one, so the content store and the
//     graph agree on what a delta is. A delta_generation fact with no listed
//     paths is treated as a delta here, which is the conservative side: the
//     graph may full-retract it, but content never reaps on it.
//
// A reconciliation generation is a full snapshot and passes.
func contentFullSnapshot(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	inputFacts []facts.Envelope,
) bool {
	if scopeValue.ScopeKind != scope.KindRepository || generation.IsDelta {
		return false
	}
	repoFacts := decode.FilterRepositoryFacts(inputFacts)
	if len(repoFacts) == 0 {
		return false
	}
	delta := decode.PayloadBoolPtr(repoFacts[0].Payload, "delta_generation")
	return delta == nil || !*delta
}

// snapshotFilePaths returns the relative paths of the generation's file facts.
// The collector emits a file fact for every parsed file, and the canonical
// graph's File nodes come from it, but it can skip a file's content fact when
// the body cannot be re-read after parsing. Those paths are in the snapshot
// without a content Record, so a full-snapshot reap must treat them as present
// (content.Materialization.RetainedPaths) instead of deleting the content the
// file already has.
func snapshotFilePaths(inputFacts []facts.Envelope) []string {
	var paths []string
	for i := range inputFacts {
		if decode.NormalizeFactKind(inputFacts[i].FactKind) != "file" {
			continue
		}
		if path, ok := decode.PayloadString(inputFacts[i].Payload, "relative_path"); ok {
			paths = append(paths, path)
		}
	}
	return paths
}
