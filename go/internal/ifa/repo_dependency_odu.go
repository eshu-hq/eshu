// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	repoDependencyConcurrencyOduName = "odu:repo-dependency-concurrency"
	repoDependencySourceCount        = 8
	repoDependencyContentPath        = "env/ifa-prod-proof/main.tf"
	sharedFollowupFactKind           = "shared_followup"
)

type repoDependencyFixtureEdge struct {
	sourceAlias string
	targetAlias string
}

var repoDependencyFixtureEdges = []repoDependencyFixtureEdge{
	{sourceAlias: "source-01", targetAlias: "target-hub"},
	{sourceAlias: "source-02", targetAlias: "target-hub"},
	{sourceAlias: "source-03", targetAlias: "target-hub"},
	{sourceAlias: "source-04", targetAlias: "target-hub"},
	{sourceAlias: "source-05", targetAlias: "source-06"},
	{sourceAlias: "source-06", targetAlias: "source-05"},
	{sourceAlias: "source-07", targetAlias: "target-07"},
	{sourceAlias: "source-08", targetAlias: "target-08"},
}

// repoDependencyConcurrencyOdu supplies the production relationship extractor
// with eight independently scoped source repositories. Four sources converge
// on one replay target, two form a reciprocal pair, and two remain disjoint.
func repoDependencyConcurrencyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, repoDependencySourceCount*3+3)
	for _, edge := range repoDependencyFixtureEdges {
		factsForOdu = append(factsForOdu, repoDependencySourceFacts(edge)...)
	}
	// Hostile references must not become relationship evidence: source-01
	// names itself, while target-07-extra only prefixes a real catalog alias.
	factsForOdu = append(
		factsForOdu,
		repoDependencyContentFact("source-01", "env/ifa-prod-proof/self.tf", "source-01"),
		repoDependencyContentFact("source-07", "env/ifa-prod-proof/prefix.tf", "target-07-extra"),
	)
	for _, alias := range []string{"target-hub", "target-07", "target-08"} {
		factsForOdu = append(factsForOdu, repoDependencyRepositoryFact(alias))
	}

	return CatalogOdu{
		Odu: Odu{
			Name:  repoDependencyConcurrencyOduName,
			Facts: factsForOdu,
		},
		Detail: "eight independently scoped Terraform app_repo sources with four-way target fan-in, one reciprocal pair, and two disjoint targets",
	}
}

func repoDependencySourceFacts(edge repoDependencyFixtureEdge) []facts.Envelope {
	repoID := repoDependencyRepoID(edge.sourceAlias)
	scopeID := repoDependencyScopeID(edge.sourceAlias)
	generationID := repoDependencyGenerationID(edge.sourceAlias)
	rootURI := "synthetic/" + edge.sourceAlias

	return []facts.Envelope{
		repoDependencyRepositoryFact(edge.sourceAlias),
		repoDependencyContentFact(edge.sourceAlias, repoDependencyContentPath, edge.targetAlias),
		repoDependencyFactEnvelope(
			sharedFollowupFactKind,
			scopeID,
			generationID,
			"shared_followup:"+repoID+":deployment_mapping",
			map[string]any{
				"entity_key":     "deployment:" + edge.sourceAlias,
				"reason":         "repository snapshot emitted deployment mapping follow-up",
				"reducer_domain": "deployment_mapping",
				"repo_id":        repoID,
			},
			rootURI,
		),
	}
}

func repoDependencyContentFact(sourceAlias, path, targetAlias string) facts.Envelope {
	repoID := repoDependencyRepoID(sourceAlias)
	return repoDependencyFactEnvelope(
		contentFactKind,
		repoDependencyScopeID(sourceAlias),
		repoDependencyGenerationID(sourceAlias),
		"content:"+repoID+":"+path,
		map[string]any{
			"artifact_type": "terraform_hcl",
			"commit_sha":    "0123456789abcdef0123456789abcdef01234567",
			"content_body":  fmt.Sprintf("app_repo = %q\n", targetAlias),
			"content_path":  path,
			"repo_id":       repoID,
		},
		"synthetic/"+sourceAlias+"/"+path,
	)
}

func repoDependencyRepositoryFact(alias string) facts.Envelope {
	repoID := repoDependencyRepoID(alias)
	generationID := repoDependencyGenerationID(alias)
	return repoDependencyFactEnvelope(
		repositoryFactKind,
		repoDependencyScopeID(alias),
		generationID,
		"repository:"+repoID,
		map[string]any{
			"name":          alias,
			"repo_id":       repoID,
			"repo_slug":     "synthetic/" + alias,
			"source_run_id": generationID,
		},
		"synthetic/"+alias,
	)
}

func repoDependencyFactEnvelope(
	factKind string,
	scopeID string,
	generationID string,
	stableFactKey string,
	payload map[string]any,
	sourceURI string,
) facts.Envelope {
	return facts.Envelope{
		FactID: facts.StableID(
			"IfaRepoDependencyFact",
			map[string]any{
				"fact_kind":     factKind,
				"generation_id": generationID,
				"scope_id":      scopeID,
				"stable_key":    stableFactKey,
			},
		),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        stableFactKey,
			SourceURI:      sourceURI,
			SourceRecordID: stableFactKey,
		},
	}
}

func repoDependencyRepoID(alias string) string {
	return "repository:" + alias
}

func repoDependencyScopeID(alias string) string {
	return "git-repository-scope:" + repoDependencyRepoID(alias)
}

func repoDependencyGenerationID(alias string) string {
	return "generation:repo-dependency:" + alias
}
