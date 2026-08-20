// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// repoDependencyFamilyCassettePath is where the family's committed cassette
// lives. Mirrors deployableUnitFamilyCassettePath's role: the live gates
// replay this same committed cassette, and this file's loader plus a
// lockstep test prove it never drifts from the compiled catalog Odù
// (repo_dependency_family_catalog.go) rather than maintaining two fixtures
// that can silently diverge.
const repoDependencyFamilyCassettePath = "testdata/cassettes/repodependency/ifa-repo-dependency-family.json"

// repoDependencyFamilyCassetteFile mirrors deployableUnitFamilyCassetteFile's
// field set for the same reason: schema_version, stable_fact_key,
// collector_kind, and source_confidence all gate whether a fact is accepted
// at all, so this projection must never be more permissive than production.
type repoDependencyFamilyCassetteFile struct {
	Scopes []struct {
		ScopeID      string            `json:"scope_id"`
		GenerationID string            `json:"generation_id"`
		Metadata     map[string]string `json:"metadata"`
		Facts        []struct {
			FactKind         string         `json:"fact_kind"`
			SchemaVersion    string         `json:"schema_version"`
			StableFactKey    string         `json:"stable_fact_key"`
			CollectorKind    string         `json:"collector_kind"`
			SourceConfidence string         `json:"source_confidence"`
			IsTombstone      bool           `json:"is_tombstone"`
			Payload          map[string]any `json:"payload"`
		} `json:"facts"`
	} `json:"scopes"`
}

// RepoDependencyFamilyCassetteFullPath joins repoRoot onto the committed
// cassette path for materializededges' moved family lockstep tests.
func RepoDependencyFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, repoDependencyFamilyCassettePath)
}

// loadRepoDependencyFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes relationships.DiscoverEvidence consumes.
//
// Unexported: it is the test-side lockstep loader for the committed
// cassette. Production registers the compiled repoDependencyFamilyOdu() in
// catalogSeed; a lockstep test compares that registered Odù with this strict
// cassette projection so a one-sided edit fails the focused suite.
//
// It fails closed unless the cassette carries seven unique repository scopes,
// exactly one evidence-bearing source last, and the production scheduling
// followups. That keeps target identity and source coordinates explicit while
// preventing an empty or reduced Odù from making downstream assertions vacuous.
// LoadRepoDependencyFamilyOdu projects the committed multi-scope cassette into
// the Odù shape consumed by the materializededges package's vacuity guard.
func LoadRepoDependencyFamilyOdu(cassettePath string) (Odu, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return Odu{}, fmt.Errorf("ifa: read repo-dependency cassette %s: %w", cassettePath, err)
	}
	var parsed repoDependencyFamilyCassetteFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Odu{}, fmt.Errorf("ifa: parse repo-dependency cassette %s: %w", cassettePath, err)
	}
	if len(parsed.Scopes) != 7 {
		return Odu{}, fmt.Errorf("ifa: repo-dependency cassette %s declares %d scopes, want exactly 7", cassettePath, len(parsed.Scopes))
	}

	seenScopes := make(map[string]struct{}, len(parsed.Scopes))
	seenGenerations := make(map[string]struct{}, len(parsed.Scopes))
	seenRepos := make(map[string]struct{}, len(parsed.Scopes))
	envelopes := make([]facts.Envelope, 0, 18)
	sourceScopeIndex := -1
	for scopeIndex, scope := range parsed.Scopes {
		if strings.TrimSpace(scope.ScopeID) == "" || strings.TrimSpace(scope.GenerationID) == "" {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %d has blank coordinates", scopeIndex)
		}
		if _, duplicate := seenScopes[scope.ScopeID]; duplicate {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette repeats scope_id %q", scope.ScopeID)
		}
		seenScopes[scope.ScopeID] = struct{}{}
		if _, duplicate := seenGenerations[scope.GenerationID]; duplicate {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette repeats generation_id %q", scope.GenerationID)
		}
		seenGenerations[scope.GenerationID] = struct{}{}
		repoID := strings.TrimSpace(scope.Metadata["repo_id"])
		if repoID == "" {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %q has no metadata repo_id", scope.ScopeID)
		}
		if strings.TrimSpace(scope.Metadata["repo_path"]) == "" {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %q has no metadata repo_path", scope.ScopeID)
		}
		if _, duplicate := seenRepos[repoID]; duplicate {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette repeats repository identity %q", repoID)
		}
		seenRepos[repoID] = struct{}{}

		repositoryFacts := 0
		evidenceFacts := 0
		followups := make(map[string]bool)
		for _, fact := range scope.Facts {
			if fact.FactKind == repositoryFactKind {
				repositoryFacts++
				factRepoID, ok := fact.Payload["repo_id"].(string)
				if !ok {
					return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %q repository fact repo_id has type %T, want string", scope.ScopeID, fact.Payload["repo_id"])
				}
				factRepoID = strings.TrimSpace(factRepoID)
				if factRepoID != repoID {
					return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %q repository fact identity %q does not match metadata repo_id %q", scope.ScopeID, factRepoID, repoID)
				}
			}
			if fact.FactKind != repositoryFactKind && fact.FactKind != "shared_followup" {
				evidenceFacts++
			}
			if fact.FactKind == "shared_followup" {
				domain, _ := fact.Payload["reducer_domain"].(string)
				followups[domain] = true
			}
			envelopes = append(envelopes, facts.Envelope{
				ScopeID: scope.ScopeID, GenerationID: scope.GenerationID, FactKind: fact.FactKind,
				SchemaVersion: fact.SchemaVersion, StableFactKey: fact.StableFactKey,
				CollectorKind: fact.CollectorKind, SourceConfidence: fact.SourceConfidence,
				IsTombstone: fact.IsTombstone, Payload: fact.Payload,
			})
		}
		if repositoryFacts != 1 {
			return Odu{}, fmt.Errorf("ifa: repo-dependency cassette scope %q carries %d repository facts, want exactly 1", scope.ScopeID, repositoryFacts)
		}
		if evidenceFacts > 0 {
			if sourceScopeIndex >= 0 {
				return Odu{}, fmt.Errorf("ifa: repo-dependency cassette has multiple evidence-bearing source scopes")
			}
			sourceScopeIndex = scopeIndex
			if !followups["workload_materialization"] || !followups["deployment_mapping"] {
				return Odu{}, fmt.Errorf("ifa: repo-dependency source scope %q is missing production followup facts", scope.ScopeID)
			}
		} else if len(scope.Facts) != 1 {
			return Odu{}, fmt.Errorf("ifa: repo-dependency target scope %q carries non-repository facts", scope.ScopeID)
		}
	}
	if sourceScopeIndex != len(parsed.Scopes)-1 {
		return Odu{}, fmt.Errorf("ifa: repo-dependency evidence-bearing source scope must be last, got index %d", sourceScopeIndex)
	}
	if len(envelopes) != 18 {
		return Odu{}, fmt.Errorf("ifa: repo-dependency cassette carries %d facts, want exactly 18", len(envelopes))
	}
	return Odu{Name: repoDependencyFamilyOduName, Facts: envelopes}, nil
}

// RepoDependencyFamilySourceCoordinates returns the evidence-bearing source
// coordinates used by the materialized-edge guard's production intent seam.
func RepoDependencyFamilySourceCoordinates(odu Odu) (string, string, error) {
	for _, fact := range odu.Facts {
		if fact.FactKind == contentFactKind || fact.FactKind == factschema.FactKindCodegraphFile {
			return fact.ScopeID, fact.GenerationID, nil
		}
	}
	return "", "", fmt.Errorf("ifa: repo-dependency Odù has no evidence-bearing source coordinates")
}
