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
