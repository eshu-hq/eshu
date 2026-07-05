// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalNodeGitlabDefinesJobEdgeCypher links a GitLab CI pipeline to a job it
// declares. Source and target are matched by their canonical keys
// (GitlabPipeline.uid, GitlabJob.uid) supplied per row from Go, mirroring the
// Atlantis edges (uid-matched, no bound-variable property matching, which is
// unreliable on the graph backend). The edge type is DEFINES_JOB (not the
// code-symbol-scoped DEFINES) so a pipeline-to-job edge is never conflated with a
// code DEFINES traversal.
const canonicalNodeGitlabDefinesJobEdgeCypher = `UNWIND $rows AS row
MATCH (p:GitlabPipeline {uid: row.source_uid})
MATCH (j:GitlabJob {uid: row.target_uid})
MERGE (p)-[r:DEFINES_JOB]->(j)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

// canonicalNodeGitlabNeedsEdgeCypher links a GitLab CI job to the in-file sibling
// jobs it names in needs/dependencies. The Go builder resolves each name to the
// sibling job's uid within the same .gitlab-ci.yml, so both endpoints are matched
// by uid here. The edge type is NEEDS (not the generic DEPENDS_ON) so CI job
// ordering is never conflated with repository/package dependency edges by a
// label-agnostic DEPENDS_ON traversal.
const canonicalNodeGitlabNeedsEdgeCypher = `UNWIND $rows AS row
MATCH (a:GitlabJob {uid: row.source_uid})
MATCH (b:GitlabJob {uid: row.target_uid})
MERGE (a)-[r:NEEDS]->(b)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

// retractGitlabDefinesJobEdgesCypher deletes stale DEFINES_JOB edges from this
// materialization's GitlabPipeline source nodes. DEFINES_JOB / NEEDS are MERGE-only
// edges between surviving nodes, so neither repository_cleanup (DETACH DELETE of
// the Repository node only) nor entity_retract (edges of DELETED nodes only)
// removes a stale edge when both endpoints are refreshed to the current
// generation but the relationship changed. Scoping by the projecting source uids
// and deleting only projector/canonical edges whose generation_id differs from
// the current one drops the stale edge; the subsequent MERGE re-writes the
// current edge with the current generation_id. The retract is bounded by the
// pipeline count in one .gitlab-ci.yml (one pipeline per file). It is emitted as
// its own per-label statement (not a multi-type [r:NEEDS|DEFINES_JOB] match,
// which is less reliable on the graph backend). The statement is Drain-marked so
// the NornicDB phase-group executor runs it as a standalone autocommit
// relationship DELETE before the sibling MERGE statements; grouped relationship
// DELETEs can no-op inside the structural_edges ExecuteWrite transaction.
const retractGitlabDefinesJobEdgesCypher = `UNWIND $source_uids AS uid
MATCH (p:GitlabPipeline {uid: uid})-[r:DEFINES_JOB]->(:GitlabJob)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`

// retractGitlabNeedsEdgesCypher deletes stale NEEDS edges from this
// materialization's GitlabJob source nodes, mirroring
// retractGitlabDefinesJobEdgesCypher. A job whose needs change while both
// endpoint jobs survive would otherwise keep the old (GitlabJob)-[:NEEDS]->(GitlabJob)
// edge. Scoped by job source uid and generation-guarded; the MERGE re-writes the
// current edges. Bounded by the job count in one .gitlab-ci.yml and
// Drain-marked for the same mixed structural_edges autocommit path as
// DEFINES_JOB.
const retractGitlabNeedsEdgesCypher = `UNWIND $source_uids AS uid
MATCH (a:GitlabJob {uid: uid})-[r:NEEDS]->(:GitlabJob)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`

// gitlabPipelineEntity is one GitlabPipeline content entity reduced to the fields
// the DEFINES_JOB edge needs.
type gitlabPipelineEntity struct {
	uid      string
	filePath string
}

// gitlabJobEntity is one GitlabJob content entity reduced to the fields the
// DEFINES_JOB and NEEDS edges need.
type gitlabJobEntity struct {
	uid      string
	name     string
	filePath string
	needs    []string
}

// gitlabEdgeStatements returns the GitLab CI pipeline edge statements
// (DEFINES_JOB, NEEDS) for the GitlabPipeline / GitlabJob entities in the
// materialization, or nil when there are none so the statements never run for
// non-GitLab repos. Edges are resolved in Go and matched by canonical key (uid),
// which is robust where bound-variable property matching is not.
func gitlabEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	pipelines := collectGitlabPipelineEntities(mat.Entities)
	jobs := collectGitlabJobEntities(mat.Entities)
	if len(pipelines) == 0 && len(jobs) == 0 {
		return nil
	}

	// One pipeline per file (the canonical .gitlab-ci.yml node); map file -> uid
	// so each job's DEFINES_JOB edge resolves to the pipeline in the same file.
	pipelineUIDByFile := make(map[string]string, len(pipelines))
	for _, pipeline := range pipelines {
		pipelineUIDByFile[pipeline.filePath] = pipeline.uid
	}
	// job name -> uid, scoped per containing file so needs resolves to a sibling
	// job in the same .gitlab-ci.yml.
	jobUIDByFileName := make(map[string]string, len(jobs))
	for _, job := range jobs {
		jobUIDByFileName[job.filePath+"\x00"+job.name] = job.uid
	}

	var definesJob []map[string]any
	var needs []map[string]any
	for _, job := range jobs {
		if pipelineUID, ok := pipelineUIDByFile[job.filePath]; ok {
			definesJob = append(definesJob, map[string]any{
				"source_uid":    pipelineUID,
				"target_uid":    job.uid,
				"generation_id": mat.GenerationID,
			})
		}
		for _, depName := range job.needs {
			targetUID, ok := jobUIDByFileName[job.filePath+"\x00"+depName]
			if !ok || targetUID == job.uid {
				continue
			}
			needs = append(needs, map[string]any{
				"source_uid":    job.uid,
				"target_uid":    targetUID,
				"generation_id": mat.GenerationID,
			})
		}
	}

	var stmts []Statement

	// Retract stale edges BEFORE the MERGE so a re-projection with a changed
	// needs/defines set, where both endpoint nodes survive into the current
	// generation, drops the old generation's edge that repository_cleanup and
	// entity_retract leave behind. The retracts are scoped to THIS
	// materialization's source uids and only touch projector/canonical edges of a
	// prior generation. When there are no source uids the retract is a no-op and
	// is skipped. Statement order within a phase is preserved by the writer, so
	// emitting the retracts first guarantees they execute before the MERGE in the
	// same structural_edges phase.
	if pipelineSourceUIDs := gitlabPipelineSourceUIDs(definesJob); len(pipelineSourceUIDs) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractGitlabDefinesJobEdgesCypher,
			Parameters: map[string]any{
				"source_uids":   pipelineSourceUIDs,
				"generation_id": mat.GenerationID,
			},
			Drain: true,
		})
	}
	if jobSourceUIDs := gitlabJobSourceUIDs(jobs); len(jobSourceUIDs) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractGitlabNeedsEdgesCypher,
			Parameters: map[string]any{
				"source_uids":   jobSourceUIDs,
				"generation_id": mat.GenerationID,
			},
			Drain: true,
		})
	}

	if len(definesJob) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalNodeGitlabDefinesJobEdgeCypher,
			Parameters: map[string]any{"rows": definesJob},
		})
	}
	if len(needs) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalNodeGitlabNeedsEdgeCypher,
			Parameters: map[string]any{"rows": needs},
		})
	}
	return stmts
}

// gitlabPipelineSourceUIDs returns the distinct DEFINES_JOB source (pipeline)
// uids from the resolved definesJob rows, so the DEFINES_JOB retract is scoped to
// exactly the pipelines this materialization re-projects.
func gitlabPipelineSourceUIDs(definesJob []map[string]any) []string {
	seen := make(map[string]struct{}, len(definesJob))
	uids := make([]string, 0, len(definesJob))
	for _, row := range definesJob {
		uid, _ := row["source_uid"].(string)
		if uid == "" {
			continue
		}
		if _, dup := seen[uid]; dup {
			continue
		}
		seen[uid] = struct{}{}
		uids = append(uids, uid)
	}
	return uids
}

// gitlabJobSourceUIDs returns every GitlabJob uid in the materialization. Any job
// is a potential NEEDS source, so the NEEDS retract must scope to all of them —
// including a job that lost its last need this generation, whose stale NEEDS edge
// would otherwise persist because it produces no MERGE row.
func gitlabJobSourceUIDs(jobs []gitlabJobEntity) []string {
	uids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.uid != "" {
			uids = append(uids, job.uid)
		}
	}
	return uids
}

// collectGitlabPipelineEntities extracts GitlabPipeline entities from the
// materialization's entity rows.
func collectGitlabPipelineEntities(entities []projector.EntityRow) []gitlabPipelineEntity {
	var pipelines []gitlabPipelineEntity
	for _, entity := range entities {
		if entity.Label != "GitlabPipeline" {
			continue
		}
		pipelines = append(pipelines, gitlabPipelineEntity{
			uid:      entity.EntityID,
			filePath: entity.FilePath,
		})
	}
	return pipelines
}

// collectGitlabJobEntities extracts GitlabJob entities from the materialization's
// entity rows.
func collectGitlabJobEntities(entities []projector.EntityRow) []gitlabJobEntity {
	var jobs []gitlabJobEntity
	for _, entity := range entities {
		if entity.Label != "GitlabJob" {
			continue
		}
		jobs = append(jobs, gitlabJobEntity{
			uid:      entity.EntityID,
			name:     entity.EntityName,
			filePath: entity.FilePath,
			needs:    splitGitlabList(metadataString(entity.Metadata, "needs")),
		})
	}
	return jobs
}

// splitGitlabList splits the parser's comma-joined needs list back into entries.
func splitGitlabList(joined string) []string {
	if strings.TrimSpace(joined) == "" {
		return nil
	}
	parts := strings.Split(joined, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
