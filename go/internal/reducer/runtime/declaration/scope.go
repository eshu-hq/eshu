// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// retractScope splits the repositories one generation covers by how their
// stale declarations are retracted. A repository fact without
// delta_generation is a full snapshot: the whole repository is retracted. A
// delta repository fact lists its changed and deleted repo-relative paths:
// only those files' declarations are retracted.
//
// A repository is named by its repository fact, not only by its runtime
// facts, so a full generation whose last pin file was deleted (zero runtime
// facts) still retracts that repository's stale declarations.
type retractScope struct {
	wholeRepos map[string]struct{}
	deltaRepos map[string]struct{}
	deltaPaths map[string][]string
}

func buildRetractScope(envelopes []facts.Envelope) retractScope {
	scope := retractScope{
		wholeRepos: map[string]struct{}{},
		deltaRepos: map[string]struct{}{},
		deltaPaths: map[string][]string{},
	}
	for _, env := range envelopes {
		if env.FactKind != factload.FactKindRepository || env.IsTombstone {
			continue
		}
		repoID := payloadcore.SemanticPayloadString(env.Payload, "repo_id")
		if repoID == "" {
			repoID = payloadcore.SemanticPayloadString(env.Payload, "graph_id")
		}
		if repoID == "" {
			continue
		}
		if !payloadcore.DeltaPayloadBool(env.Payload, "delta_generation") {
			scope.wholeRepos[repoID] = struct{}{}
			continue
		}
		scope.deltaRepos[repoID] = struct{}{}
		seen := map[string]struct{}{}
		for _, existing := range scope.deltaPaths[repoID] {
			seen[existing] = struct{}{}
		}
		for _, key := range []string{"delta_relative_paths", "delta_deleted_relative_paths"} {
			for _, relativePath := range payloadcore.SemanticPayloadStringSlice(env.Payload, key) {
				relativePath = strings.TrimSpace(relativePath)
				if relativePath == "" {
					continue
				}
				if _, dup := seen[relativePath]; dup {
					continue
				}
				seen[relativePath] = struct{}{}
				scope.deltaPaths[repoID] = append(scope.deltaPaths[repoID], relativePath)
			}
		}
	}
	// A repository that is both full and delta in one generation (two
	// repository facts) takes the whole-repository retract: it is the superset.
	for repoID := range scope.wholeRepos {
		delete(scope.deltaRepos, repoID)
		delete(scope.deltaPaths, repoID)
	}
	return scope
}

// addRowRepositories adds every repository a declaration row targets that no
// repository fact named. Such a row comes from a generation whose repository
// fact is absent; its repository is treated as a full snapshot, the same
// default the codeowners and submodule families use outside a delta.
func (s retractScope) addRowRepositories(rows []map[string]any) {
	for _, row := range rows {
		repoID, _ := row["repo_id"].(string)
		if repoID == "" {
			continue
		}
		if _, delta := s.deltaRepos[repoID]; delta {
			continue
		}
		s.wholeRepos[repoID] = struct{}{}
	}
}

// wholeRepoIDs returns the repositories to retract whole, sorted.
func (s retractScope) wholeRepoIDs() []string {
	out := make([]string, 0, len(s.wholeRepos))
	for repoID := range s.wholeRepos {
		out = append(out, repoID)
	}
	sort.Strings(out)
	return out
}
