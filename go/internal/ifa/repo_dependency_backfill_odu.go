// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	repoDependencyBackfillOduName                  = "odu:repo-dependency-backfill-proof"
	repoDependencyBackfillCandidateStableKeyPrefix = "backfill-family-candidate:"
	repoDependencyBackfillGenericStableKeyPrefix   = "backfill-generic-distractor:"
	repoDependencyBackfillGenericPayloadBytes      = 3072
)

type repoDependencyBackfillScopeShape struct {
	alias      string
	sourceRows int
	candidates int
}

// These are the retained worst four relationship-backfill partitions repeated
// once so all eight production workers receive a real, skewed work unit. The
// first four rows came from the post-#5218 representative run; repetition is a
// deliberate concurrency load shape, not a claim that the corpus contained two
// copies of each repository.
var repoDependencyBackfillScopeShapes = []repoDependencyBackfillScopeShape{
	{alias: "source-01", sourceRows: 14190, candidates: 12},
	{alias: "source-02", sourceRows: 11322, candidates: 24},
	{alias: "source-03", sourceRows: 4818, candidates: 4},
	{alias: "source-04", sourceRows: 164, candidates: 2},
	{alias: "source-05", sourceRows: 14190, candidates: 12},
	{alias: "source-06", sourceRows: 11322, candidates: 24},
	{alias: "source-07", sourceRows: 4818, candidates: 4},
	{alias: "source-08", sourceRows: 164, candidates: 2},
}

// RepoDependencyBackfillProofOdu returns the lazy, retained-shape Ifá scenario
// for proving relationship-backfill query candidates. It starts with the
// cataloged eight-source repository dependency Odù, preserving its four-way
// fan-in, reciprocal pair, disjoint targets, self-reference, and prefix
// collision, then expands only the eight measured source partitions to the
// retained worst-scope cardinalities.
//
// The 60,904 generic content rows deliberately carry noisy target aliases but
// use non-relationship file and artifact shapes. Their deterministic payloads
// are large enough to exercise JSONB TOAST/detoast behavior in the Postgres
// operator proof. The 84 relationship-family candidates include the original
// truth-producing facts and truth-inert Terraform-shaped rows. One original
// fact also carries its target repo_id so the SQL alias and repo-id UNION arms
// select the same fact and must deduplicate it.
//
// This large scenario is intentionally not registered in CatalogByName: it
// proves no new semantic coverage and should not add roughly 61,000 facts to
// every ordinary Ifá coverage derivation.
func RepoDependencyBackfillProofOdu() Odu {
	odu := repoDependencyConcurrencyOdu().Odu
	odu.Name = repoDependencyBackfillOduName
	markRepoDependencyDualArmFact(odu.Facts)

	targets := repoDependencyTargetsBySource()
	for _, shape := range repoDependencyBackfillScopeShapes {
		currentSourceRows := repoDependencySourceRowCount(odu.Facts, shape.alias)
		currentCandidates := currentSourceRows
		for candidate := currentCandidates; candidate < shape.candidates; candidate++ {
			odu.Facts = append(odu.Facts, repoDependencyBackfillCandidateFact(shape.alias, candidate))
		}
		genericCount := shape.sourceRows - shape.candidates
		for generic := 0; generic < genericCount; generic++ {
			odu.Facts = append(odu.Facts, repoDependencyBackfillGenericFact(
				shape.alias,
				targets[shape.alias],
				generic,
			))
		}
	}
	return odu
}

func repoDependencyTargetsBySource() map[string]string {
	targets := make(map[string]string, len(repoDependencyFixtureEdges))
	for _, edge := range repoDependencyFixtureEdges {
		targets[edge.sourceAlias] = edge.targetAlias
	}
	return targets
}

func repoDependencySourceRowCount(input []facts.Envelope, alias string) int {
	repoID := repoDependencyRepoID(alias)
	count := 0
	for _, fact := range input {
		if fact.FactKind != contentFactKind && fact.FactKind != "file" && fact.FactKind != facts.GCPCloudRelationshipFactKind {
			continue
		}
		if fact.Payload["repo_id"] == repoID {
			count++
		}
	}
	return count
}

func markRepoDependencyDualArmFact(input []facts.Envelope) {
	wantStableKey := "content:" + repoDependencyRepoID("source-05") + ":" + repoDependencyContentPath
	for i := range input {
		if input[i].StableFactKey == wantStableKey {
			input[i].Payload["linked_repo_id"] = repoDependencyRepoID("source-06")
			return
		}
	}
}

func repoDependencyBackfillCandidateFact(alias string, index int) facts.Envelope {
	repoID := repoDependencyRepoID(alias)
	path := fmt.Sprintf("env/ifa-backfill-proof/candidate-%05d.tf", index)
	stableKey := fmt.Sprintf("%s%s:%05d", repoDependencyBackfillCandidateStableKeyPrefix, repoID, index)
	return repoDependencyFactEnvelope(
		contentFactKind,
		repoDependencyScopeID(alias),
		repoDependencyGenerationID(alias),
		stableKey,
		map[string]any{
			"artifact_type": "terraform_hcl",
			"commit_sha":    "0123456789abcdef0123456789abcdef01234567",
			"content_body":  "# relationship-family candidate without a catalog reference\n",
			"content_path":  path,
			"repo_id":       repoID,
		},
		"synthetic/"+alias+"/"+path,
	)
}

func repoDependencyBackfillGenericFact(alias, targetAlias string, index int) facts.Envelope {
	repoID := repoDependencyRepoID(alias)
	path := fmt.Sprintf("src/ifa-backfill-proof/distractor-%05d.go", index)
	stableKey := fmt.Sprintf("%s%s:%05d", repoDependencyBackfillGenericStableKeyPrefix, repoID, index)
	content := "// documentation mentions " + targetAlias + " but is not relationship configuration\n" +
		repoDependencyBackfillNoise(stableKey, repoDependencyBackfillGenericPayloadBytes)
	return repoDependencyFactEnvelope(
		contentFactKind,
		repoDependencyScopeID(alias),
		repoDependencyGenerationID(alias),
		stableKey,
		map[string]any{
			"artifact_type":          "text",
			"commit_sha":             "0123456789abcdef0123456789abcdef01234567",
			"content_body":           content,
			"content_path":           path,
			"mentions_catalog_alias": true,
			"repo_id":                repoID,
		},
		"synthetic/"+alias+"/"+path,
	)
}

func repoDependencyBackfillNoise(key string, size int) string {
	if size <= 0 {
		return ""
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	state := uint64(1469598103934665603)
	for i := 0; i < len(key); i++ {
		state ^= uint64(key[i])
		state *= 1099511628211
	}
	if state == 0 {
		state = 1
	}
	var builder strings.Builder
	builder.Grow(size)
	for i := 0; i < size; i++ {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		builder.WriteByte(alphabet[state&63])
	}
	return builder.String()
}
