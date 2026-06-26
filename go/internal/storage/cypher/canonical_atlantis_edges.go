// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"path"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// canonicalNodeAtlantisManagesEdgeCypher links an Atlantis project to the
// Terraform Directory it plans/applies. Source and target are matched by their
// canonical keys (AtlantisProject.uid, Directory.path) supplied per row from Go,
// mirroring the IMPORTS edge (which matches File by path param). The Go builder
// resolves the project dir to the Directory's absolute path using the repo root,
// so this statement does no string manipulation or bound-variable property
// matching (both unreliable on the graph backend).
const canonicalNodeAtlantisManagesEdgeCypher = `UNWIND $rows AS row
MATCH (p:AtlantisProject {uid: row.source_uid})
MATCH (d:Directory {path: row.target_path})
MERGE (p)-[r:MANAGES]->(d)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

// canonicalNodeAtlantisDependsOnEdgeCypher links an Atlantis project to the
// in-file projects it names in depends_on. The Go builder resolves each
// depends_on name to the sibling project's uid within the same atlantis.yaml, so
// both endpoints are matched by uid here. The edge type is ATLANTIS_DEPENDS_ON
// (not the generic DEPENDS_ON) so Atlantis apply-ordering is never conflated with
// repository/package dependency edges by a label-agnostic DEPENDS_ON traversal.
const canonicalNodeAtlantisDependsOnEdgeCypher = `UNWIND $rows AS row
MATCH (p:AtlantisProject {uid: row.source_uid})
MATCH (q:AtlantisProject {uid: row.target_uid})
MERGE (p)-[r:ATLANTIS_DEPENDS_ON]->(q)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

// atlantisProjectEntity is one AtlantisProject content entity reduced to the
// fields the governance edges need.
type atlantisProjectEntity struct {
	uid       string
	name      string
	filePath  string
	dir       string
	dependsOn []string
}

// atlantisEdgeStatements returns the Atlantis governance edge statements
// (MANAGES, DEPENDS_ON) for the AtlantisProject entities in the materialization,
// or nil when there are none so the statements never run for non-Atlantis repos.
// Edges are resolved in Go and matched by canonical key (uid / Directory.path),
// which is robust where bound-variable property matching is not.
func atlantisEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	projects := collectAtlantisProjectEntities(mat.Entities)
	if len(projects) == 0 {
		return nil
	}

	repoRoot := strings.TrimRight(strings.TrimSpace(mat.RepoPath), "/")

	// name -> uid, scoped per containing file so depends_on resolves to a sibling
	// project in the same atlantis.yaml. If two unnamed projects in one file share
	// a dir+workspace (so their derived name collides) the last one wins here; that
	// is a malformed atlantis.yaml (Atlantis itself requires a unique name to be a
	// depends_on target), so resolving to either is acceptable.
	uidByFileName := make(map[string]string, len(projects))
	for _, project := range projects {
		uidByFileName[project.filePath+"\x00"+project.name] = project.uid
	}

	var manages []map[string]any
	var dependsOn []map[string]any
	for _, project := range projects {
		if dir := normalizeAtlantisDir(project.dir); dir != "" && repoRoot != "" {
			manages = append(manages, map[string]any{
				"source_uid":    project.uid,
				"target_path":   path.Join(repoRoot, dir),
				"generation_id": mat.GenerationID,
			})
		}
		for _, depName := range project.dependsOn {
			targetUID, ok := uidByFileName[project.filePath+"\x00"+depName]
			if !ok || targetUID == project.uid {
				continue
			}
			dependsOn = append(dependsOn, map[string]any{
				"source_uid":    project.uid,
				"target_uid":    targetUID,
				"generation_id": mat.GenerationID,
			})
		}
	}

	var stmts []Statement
	if len(manages) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalNodeAtlantisManagesEdgeCypher,
			Parameters: map[string]any{"rows": manages},
		})
	}
	if len(dependsOn) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalNodeAtlantisDependsOnEdgeCypher,
			Parameters: map[string]any{"rows": dependsOn},
		})
	}
	return stmts
}

// collectAtlantisProjectEntities extracts AtlantisProject entities from the
// materialization's entity rows.
func collectAtlantisProjectEntities(entities []projector.EntityRow) []atlantisProjectEntity {
	var projects []atlantisProjectEntity
	for _, entity := range entities {
		if entity.Label != "AtlantisProject" {
			continue
		}
		projects = append(projects, atlantisProjectEntity{
			uid:       entity.EntityID,
			name:      entity.EntityName,
			filePath:  entity.FilePath,
			dir:       metadataString(entity.Metadata, "dir"),
			dependsOn: splitAtlantisList(metadataString(entity.Metadata, "depends_on")),
		})
	}
	return projects
}

// normalizeAtlantisDir cleans a project dir into a repo-relative path, returning
// "" for the repo root ("." or empty) which has no Directory node to manage.
func normalizeAtlantisDir(dir string) string {
	cleaned := path.Clean(strings.TrimSpace(dir))
	if cleaned == "." || cleaned == "/" || cleaned == "" {
		return ""
	}
	return strings.TrimPrefix(cleaned, "./")
}

// splitAtlantisList splits the parser's comma-joined list back into entries.
func splitAtlantisList(joined string) []string {
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

// metadataString reads a string value from an entity metadata map.
func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if value, ok := metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
