package query

import (
	"context"
	"sort"
	"strings"
)

const repositoryDeploymentEvidenceArtifactLimit = 50

// queryRepoDeploymentEvidence reads compact graph evidence pointers for
// repository relationships without embedding raw Postgres evidence payloads.
func queryRepoDeploymentEvidence(ctx context.Context, reader GraphQuery, content ContentStore, params map[string]any) (map[string]any, error) {
	if readModel, err := loadRepositoryDeploymentEvidence(ctx, content, StringVal(params, "repo_id")); err != nil {
		return nil, err
	} else if readModel != nil {
		return readModel, nil
	}

	outgoing, outgoingTruncated := queryRepoDeploymentEvidenceDirection(ctx, reader, params, `
		MATCH (r:Repository {id: $repo_id})-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target:Repository)
		RETURN 'outgoing' AS direction,
		       artifact.id AS artifact_id,
		       artifact.name AS name,
		       artifact.domain AS domain,
		       artifact.path AS path,
		       artifact.evidence_kind AS evidence_kind,
		       artifact.artifact_family AS artifact_family,
		       artifact.extractor AS extractor,
		       artifact.relationship_type AS relationship_type,
		       artifact.resolved_id AS resolved_id,
		       artifact.generation_id AS generation_id,
		       artifact.confidence AS confidence,
		       artifact.environment AS environment,
		       artifact.runtime_platform_kind AS runtime_platform_kind,
		       artifact.matched_alias AS matched_alias,
		       artifact.matched_value AS matched_value,
		       artifact.evidence_source AS evidence_source,
		       artifact.start_line AS start_line,
		       artifact.end_line AS end_line,
		       r.id AS source_repo_id,
		       r.name AS source_repo_name,
		       target.id AS target_repo_id,
		       target.name AS target_repo_name
		ORDER BY path, artifact_id
	`)
	incoming, incomingTruncated := queryRepoDeploymentEvidenceDirection(ctx, reader, params, `
		MATCH (artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})
		WITH artifact, r
		MATCH (source:Repository)-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact)
		RETURN 'incoming' AS direction,
		       artifact.id AS artifact_id,
		       artifact.name AS name,
		       artifact.domain AS domain,
		       artifact.path AS path,
		       artifact.evidence_kind AS evidence_kind,
		       artifact.artifact_family AS artifact_family,
		       artifact.extractor AS extractor,
		       artifact.relationship_type AS relationship_type,
		       artifact.resolved_id AS resolved_id,
		       artifact.generation_id AS generation_id,
		       artifact.confidence AS confidence,
		       artifact.environment AS environment,
		       artifact.runtime_platform_kind AS runtime_platform_kind,
		       artifact.matched_alias AS matched_alias,
		       artifact.matched_value AS matched_value,
		       artifact.evidence_source AS evidence_source,
		       artifact.start_line AS start_line,
		       artifact.end_line AS end_line,
		       source.id AS source_repo_id,
		       source.name AS source_repo_name,
		       r.id AS target_repo_id,
		       r.name AS target_repo_name
		ORDER BY path, artifact_id
	`)
	rows := append(outgoing, incoming...)
	if len(rows) == 0 {
		return nil, nil
	}
	result := buildGraphDeploymentEvidence(rows)
	result["artifact_limit"] = repositoryDeploymentEvidenceArtifactLimit
	result["artifacts_truncated"] = outgoingTruncated || incomingTruncated
	return result, nil
}

func queryRepoDeploymentEvidenceDirection(ctx context.Context, reader GraphQuery, params map[string]any, cypher string) ([]map[string]any, bool) {
	queryParams := copyMap(params)
	queryParams["limit"] = repositoryDeploymentEvidenceArtifactLimit + 1
	rows, err := reader.Run(ctx, cypher+"\n\t\tLIMIT $limit", queryParams)
	if err != nil || len(rows) == 0 {
		return nil, false
	}
	if len(rows) > repositoryDeploymentEvidenceArtifactLimit {
		return rows[:repositoryDeploymentEvidenceArtifactLimit], true
	}
	return rows, false
}

// buildGraphDeploymentEvidence converts EvidenceArtifact graph rows into the
// repository context contract and keeps Postgres drilldown keys explicit.
func buildGraphDeploymentEvidence(rows []map[string]any) map[string]any {
	artifacts := make([]map[string]any, 0, len(rows))
	var (
		families          []string
		evidenceKinds     []string
		relationshipTypes []string
		environments      []string
		sourceRepoIDs     []string
		targetRepoIDs     []string
		ciArtifactCount   int
	)
	for _, row := range rows {
		artifact := map[string]any{
			"id":                StringVal(row, "artifact_id"),
			"direction":         StringVal(row, "direction"),
			"name":              StringVal(row, "name"),
			"domain":            StringVal(row, "domain"),
			"path":              StringVal(row, "path"),
			"evidence_kind":     StringVal(row, "evidence_kind"),
			"artifact_family":   StringVal(row, "artifact_family"),
			"extractor":         StringVal(row, "extractor"),
			"relationship_type": StringVal(row, "relationship_type"),
			"source_repo_id":    StringVal(row, "source_repo_id"),
			"source_repo_name":  StringVal(row, "source_repo_name"),
			"target_repo_id":    StringVal(row, "target_repo_id"),
			"target_repo_name":  StringVal(row, "target_repo_name"),
			"evidence_source":   StringVal(row, "evidence_source"),
		}
		copyOptionalDeploymentEvidenceFields(artifact, row)
		attachDeploymentEvidenceSourceLocation(artifact)

		family := StringVal(row, "artifact_family")
		if family == "github_actions" || family == "jenkins" {
			ciArtifactCount++
		}
		families = append(families, family)
		evidenceKinds = append(evidenceKinds, StringVal(row, "evidence_kind"))
		relationshipTypes = append(relationshipTypes, StringVal(row, "relationship_type"))
		environments = append(environments, StringVal(row, "environment"))
		sourceRepoIDs = append(sourceRepoIDs, StringVal(row, "source_repo_id"))
		targetRepoIDs = append(targetRepoIDs, StringVal(row, "target_repo_id"))
		artifacts = append(artifacts, artifact)
	}

	sort.Slice(artifacts, func(i, j int) bool {
		leftPath := StringVal(artifacts[i], "path")
		rightPath := StringVal(artifacts[j], "path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return StringVal(artifacts[i], "id") < StringVal(artifacts[j], "id")
	})

	return map[string]any{
		"truth_basis":        "graph",
		"artifact_count":     len(artifacts),
		"ci_artifact_count":  ciArtifactCount,
		"environment_count":  len(uniqueSortedStrings(environments)),
		"artifacts":          artifacts,
		"evidence_index":     buildDeploymentEvidenceIndex(artifacts),
		"artifact_families":  uniqueSortedStrings(families),
		"evidence_kinds":     uniqueSortedStrings(evidenceKinds),
		"relationship_types": uniqueSortedStrings(relationshipTypes),
		"environments":       uniqueSortedStrings(environments),
		"source_repo_ids":    uniqueSortedStrings(sourceRepoIDs),
		"target_repo_ids":    uniqueSortedStrings(targetRepoIDs),
	}
}

type deploymentEvidenceIndexBucket struct {
	artifactCount     int
	resolvedIDs       []string
	generationIDs     []string
	evidenceKinds     []string
	artifactFamilies  []string
	relationshipTypes []string
}

// buildDeploymentEvidenceIndex keeps the deployment evidence drilldown path
// visible without duplicating heavyweight evidence payloads from Postgres.
func buildDeploymentEvidenceIndex(artifacts []map[string]any) map[string]any {
	if len(artifacts) == 0 {
		return nil
	}
	byRelationshipType := map[string]*deploymentEvidenceIndexBucket{}
	byArtifactFamily := map[string]*deploymentEvidenceIndexBucket{}
	byEvidenceKind := map[string]*deploymentEvidenceIndexBucket{}
	for _, artifact := range artifacts {
		addDeploymentEvidenceIndexBucket(byRelationshipType, StringVal(artifact, "relationship_type"), artifact)
		addDeploymentEvidenceIndexBucket(byArtifactFamily, StringVal(artifact, "artifact_family"), artifact)
		addDeploymentEvidenceIndexBucket(byEvidenceKind, StringVal(artifact, "evidence_kind"), artifact)
	}
	return map[string]any{
		"lookup_basis":       "resolved_id",
		"relationship_types": finalizeDeploymentEvidenceIndex(byRelationshipType),
		"artifact_families":  finalizeDeploymentEvidenceIndex(byArtifactFamily),
		"evidence_kinds":     finalizeDeploymentEvidenceIndex(byEvidenceKind),
	}
}

func addDeploymentEvidenceIndexBucket(buckets map[string]*deploymentEvidenceIndexBucket, key string, artifact map[string]any) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	bucket := buckets[key]
	if bucket == nil {
		bucket = &deploymentEvidenceIndexBucket{}
		buckets[key] = bucket
	}
	bucket.artifactCount++
	bucket.resolvedIDs = append(bucket.resolvedIDs, StringVal(artifact, "resolved_id"))
	bucket.generationIDs = append(bucket.generationIDs, StringVal(artifact, "generation_id"))
	bucket.evidenceKinds = append(bucket.evidenceKinds, StringVal(artifact, "evidence_kind"))
	bucket.artifactFamilies = append(bucket.artifactFamilies, StringVal(artifact, "artifact_family"))
	bucket.relationshipTypes = append(bucket.relationshipTypes, StringVal(artifact, "relationship_type"))
}

func finalizeDeploymentEvidenceIndex(buckets map[string]*deploymentEvidenceIndexBucket) map[string]any {
	if len(buckets) == 0 {
		return nil
	}
	out := make(map[string]any, len(buckets))
	for key, bucket := range buckets {
		out[key] = map[string]any{
			"artifact_count":     bucket.artifactCount,
			"resolved_ids":       uniqueSortedStrings(bucket.resolvedIDs),
			"generation_ids":     uniqueSortedStrings(bucket.generationIDs),
			"evidence_kinds":     uniqueSortedStrings(bucket.evidenceKinds),
			"artifact_families":  uniqueSortedStrings(bucket.artifactFamilies),
			"relationship_types": uniqueSortedStrings(bucket.relationshipTypes),
		}
	}
	return out
}

func copyOptionalDeploymentEvidenceFields(dst map[string]any, src map[string]any) {
	if resolvedID := StringVal(src, "resolved_id"); resolvedID != "" {
		dst["resolved_id"] = resolvedID
		dst["postgres_lookup_basis"] = "resolved_id"
	}
	if generationID := StringVal(src, "generation_id"); generationID != "" {
		dst["generation_id"] = generationID
	}
	if confidence := relationshipFloatVal(src, "confidence"); confidence > 0 {
		dst["confidence"] = confidence
	}
	if startLine := firstPositiveInt(src, "start_line", "line_number", "line_start", "line"); startLine > 0 {
		dst["start_line"] = startLine
	}
	if endLine := firstPositiveInt(src, "end_line", "line_end"); endLine > 0 {
		dst["end_line"] = endLine
	}
	if environment := StringVal(src, "environment"); environment != "" {
		dst["environment"] = environment
	}
	if runtimePlatformKind := StringVal(src, "runtime_platform_kind"); runtimePlatformKind != "" {
		dst["runtime_platform_kind"] = runtimePlatformKind
	}
	if matchedAlias := StringVal(src, "matched_alias"); matchedAlias != "" {
		dst["matched_alias"] = matchedAlias
	}
	if matchedValue := StringVal(src, "matched_value"); matchedValue != "" {
		dst["matched_value"] = matchedValue
	}
}

func attachDeploymentEvidenceSourceLocation(artifact map[string]any) {
	repoID := StringVal(artifact, "source_repo_id")
	path := StringVal(artifact, "path")
	if repoID == "" || path == "" {
		return
	}
	location := map[string]any{
		"repo_id":   repoID,
		"repo_name": StringVal(artifact, "source_repo_name"),
		"path":      path,
	}
	if startLine := IntVal(artifact, "start_line"); startLine > 0 {
		location["start_line"] = startLine
	}
	if endLine := IntVal(artifact, "end_line"); endLine > 0 {
		location["end_line"] = endLine
	}
	artifact["source_location"] = location
}

func firstPositiveInt(row map[string]any, keys ...string) int {
	for _, key := range keys {
		if value := IntVal(row, key); value > 0 {
			return value
		}
	}
	return 0
}
