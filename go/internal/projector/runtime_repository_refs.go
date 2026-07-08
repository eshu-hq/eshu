// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func buildRepositoryRefs(fact facts.Envelope) []content.RepositoryRef {
	if NormalizeFactKind(fact.FactKind) != "repository" || fact.IsTombstone {
		return nil
	}
	repository, err := decodeCodegraphRepository(fact)
	if err != nil || len(repository.GitRefs) == 0 {
		return nil
	}

	defaultBranch := codegraphDerefString(repository.DefaultBranch)
	refsByKey := make(map[string]content.RepositoryRef, len(repository.GitRefs))
	for _, entry := range repository.GitRefs {
		if entry.Name == "" || entry.HeadSHA == "" {
			continue
		}
		kind := entry.Kind
		if kind == "" {
			kind = "branch"
		}
		isDefault := entry.IsDefault || entry.Name == defaultBranch
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
