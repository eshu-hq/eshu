package query

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type repositoryDeploymentEvidenceReadModel struct {
	Available bool
	Rows      []map[string]any
	Limit     int
	Truncated bool
}

type repositoryDeploymentEvidenceReadModelStore interface {
	repositoryDeploymentEvidence(context.Context, string) (repositoryDeploymentEvidenceReadModel, error)
}

func loadRepositoryDeploymentEvidence(ctx context.Context, content ContentStore, repoID string) (map[string]any, error) {
	store, ok := content.(repositoryDeploymentEvidenceReadModelStore)
	if !ok || repoID == "" {
		return nil, nil
	}
	readModel, err := store.repositoryDeploymentEvidence(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if !readModel.Available || len(readModel.Rows) == 0 {
		return nil, nil
	}
	result := buildGraphDeploymentEvidence(readModel.Rows)
	result["artifact_limit"] = readModel.Limit
	result["artifacts_truncated"] = readModel.Truncated
	return result, nil
}

// repositoryDeploymentEvidence builds the deployment-evidence response rows
// from durable resolved relationships instead of graph-side artifact traversals.
func (cr *ContentReader) repositoryDeploymentEvidence(ctx context.Context, repoID string) (repositoryDeploymentEvidenceReadModel, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return repositoryDeploymentEvidenceReadModel{}, nil
	}
	artifactLimit := repositoryDeploymentEvidenceArtifactLimit
	rows, err := cr.db.QueryContext(ctx, repositoryDeploymentEvidenceReadModelSQL, repoID, artifactLimit+1)
	if err != nil {
		return repositoryDeploymentEvidenceReadModel{}, fmt.Errorf("query repository deployment evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]map[string]any, 0, artifactLimit)
	truncated := false
	for rows.Next() {
		artifacts, err := scanRepositoryDeploymentEvidenceRows(rows, repoID)
		if err != nil {
			return repositoryDeploymentEvidenceReadModel{}, err
		}
		if len(result)+len(artifacts) > artifactLimit {
			remaining := artifactLimit - len(result)
			if remaining > 0 {
				result = append(result, artifacts[:remaining]...)
			}
			truncated = true
			break
		}
		result = append(result, artifacts...)
	}
	if err := rows.Err(); err != nil {
		return repositoryDeploymentEvidenceReadModel{}, fmt.Errorf("iterate repository deployment evidence: %w", err)
	}
	return repositoryDeploymentEvidenceReadModel{Available: true, Rows: result, Limit: artifactLimit, Truncated: truncated}, nil
}

const repositoryDeploymentEvidenceReadModelSQL = `
WITH scoped_relationships AS (
	SELECT 'outgoing' AS direction, r.*
	FROM resolved_relationships AS r
	JOIN relationship_generations AS g
	  ON g.generation_id = r.generation_id
	WHERE g.status = 'active'
	  AND r.source_repo_id = $1
	UNION ALL
	SELECT 'incoming' AS direction, r.*
	FROM resolved_relationships AS r
	JOIN relationship_generations AS g
	  ON g.generation_id = r.generation_id
	WHERE g.status = 'active'
	  AND r.target_repo_id = $1
)
SELECT r.direction,
       r.resolved_id,
       r.generation_id,
       COALESCE(r.source_repo_id, '') AS source_repo_id,
       COALESCE(source_scope.name, r.source_repo_id, '') AS source_name,
       COALESCE(r.target_repo_id, '') AS target_repo_id,
       COALESCE(target_scope.name, r.target_repo_id, '') AS target_name,
       r.relationship_type,
       r.confidence,
       r.details
FROM scoped_relationships AS r
LEFT JOIN LATERAL (
	SELECT COALESCE(payload->>'name', payload->>'repo_name', payload->>'repo_slug', source_key, scope_id) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (scope_id = r.source_repo_id OR source_key = r.source_repo_id OR payload->>'repo_id' = r.source_repo_id OR payload->>'id' = r.source_repo_id)
	ORDER BY scope_id
	LIMIT 1
) AS source_scope ON true
LEFT JOIN LATERAL (
	SELECT COALESCE(payload->>'name', payload->>'repo_name', payload->>'repo_slug', source_key, scope_id) AS name
	FROM ingestion_scopes
	WHERE scope_kind = 'repository'
	  AND (scope_id = r.target_repo_id OR source_key = r.target_repo_id OR payload->>'repo_id' = r.target_repo_id OR payload->>'id' = r.target_repo_id)
	ORDER BY scope_id
	LIMIT 1
) AS target_scope ON true
ORDER BY r.direction, r.relationship_type, source_name, target_name, r.resolved_id
LIMIT $2
`

func scanRepositoryDeploymentEvidenceRows(rows *sql.Rows, repoID string) ([]map[string]any, error) {
	var (
		direction        string
		resolvedID       string
		generationID     string
		sourceID         string
		sourceName       string
		targetID         string
		targetName       string
		relationshipType string
		confidence       float64
		detailsRaw       []byte
	)
	if err := rows.Scan(&direction, &resolvedID, &generationID, &sourceID, &sourceName, &targetID, &targetName, &relationshipType, &confidence, &detailsRaw); err != nil {
		return nil, fmt.Errorf("scan repository deployment evidence: %w", err)
	}
	details := map[string]any{}
	if len(detailsRaw) > 0 {
		if err := json.Unmarshal(detailsRaw, &details); err != nil {
			return nil, fmt.Errorf("decode repository deployment evidence details: %w", err)
		}
	}
	previews := evidencePreviewMaps(details["evidence_preview"])
	artifacts := make([]map[string]any, 0, len(previews))
	for _, preview := range previews {
		artifact := deploymentEvidenceArtifactFromPreview(preview, direction, resolvedID, generationID, sourceID, sourceName, targetID, targetName, relationshipType, confidence)
		if len(artifact) == 0 {
			continue
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func deploymentEvidenceArtifactFromPreview(preview map[string]any, direction, resolvedID, generationID, sourceID, sourceName, targetID, targetName, relationshipType string, fallbackConfidence float64) map[string]any {
	kind := strings.TrimSpace(StringVal(preview, "kind"))
	details := mapValue(preview, "details")
	path := firstDeploymentArtifactString(details, "path", "first_party_ref_path", "config_path", "file_path")
	matchedValue := firstDeploymentArtifactString(details, "matched_value", "first_party_ref_normalized", "source_ref", "image_ref")
	matchedAlias := firstDeploymentArtifactString(details, "matched_alias", "first_party_ref_name", "name")
	if kind == "" || (path == "" && matchedValue == "" && matchedAlias == "") {
		return nil
	}
	confidence := relationshipFloatVal(preview, "confidence")
	if confidence <= 0 {
		confidence = fallbackConfidence
	}
	artifact := map[string]any{
		"direction":         direction,
		"artifact_id":       deploymentEvidenceArtifactID(resolvedID, kind, path, matchedValue),
		"name":              firstNonEmpty(path, kind),
		"domain":            "deployment",
		"path":              path,
		"evidence_kind":     kind,
		"artifact_family":   deploymentArtifactFamily(kind),
		"extractor":         firstDeploymentArtifactString(details, "extractor", "parser", "source"),
		"relationship_type": relationshipType,
		"resolved_id":       resolvedID,
		"generation_id":     generationID,
		"confidence":        confidence,
		"matched_alias":     matchedAlias,
		"matched_value":     matchedValue,
		"evidence_source":   "resolver/cross-repo",
		"source_repo_id":    sourceID,
		"source_repo_name":  sourceName,
		"target_repo_id":    targetID,
		"target_repo_name":  targetName,
	}
	if environment := firstDeploymentArtifactString(details, "environment"); environment != "" {
		artifact["environment"] = environment
	} else if environment := deploymentEnvironmentFromPath(path); environment != "" {
		artifact["environment"] = environment
	}
	if startLine := firstPositiveInt(details, "start_line", "line_number", "line_start", "line"); startLine > 0 {
		artifact["start_line"] = startLine
	}
	if endLine := firstPositiveInt(details, "end_line", "line_end"); endLine > 0 {
		artifact["end_line"] = endLine
	}
	if runtimeKind := firstDeploymentArtifactString(details, "runtime_platform_kind", "platform_kind"); runtimeKind != "" {
		artifact["runtime_platform_kind"] = runtimeKind
	}
	return artifact
}

func evidencePreviewMaps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if ok {
			result = append(result, row)
		}
	}
	return result
}

func firstDeploymentArtifactString(details map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(StringVal(details, key))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func deploymentEvidenceArtifactID(resolvedID, evidenceKind, path, matchedValue string) string {
	hash := sha1.Sum([]byte(strings.Join([]string{resolvedID, evidenceKind, path, matchedValue}, "\x00")))
	return "evidence-artifact:" + hex.EncodeToString(hash[:8])
}

func deploymentArtifactFamily(kind string) string {
	switch {
	case strings.HasPrefix(kind, "ARGOCD_"):
		return "argocd"
	case strings.HasPrefix(kind, "HELM_"):
		return "helm"
	case strings.HasPrefix(kind, "KUSTOMIZE_"):
		return "kustomize"
	case strings.HasPrefix(kind, "TERRAFORM_"):
		return "terraform"
	case strings.HasPrefix(kind, "GITHUB_ACTIONS_"):
		return "github_actions"
	case strings.HasPrefix(kind, "JENKINS_"):
		return "jenkins"
	case strings.HasPrefix(kind, "ANSIBLE_"):
		return "ansible"
	case strings.HasPrefix(kind, "DOCKER_"):
		return "docker"
	default:
		return strings.ToLower(kind)
	}
}

func deploymentEnvironmentFromPath(path string) string {
	for _, segment := range strings.Split(path, "/") {
		for _, token := range strings.FieldsFunc(strings.ToLower(segment), func(r rune) bool {
			return r == '-' || r == '_' || r == '.'
		}) {
			if isDeploymentEnvironmentToken(token) {
				return segment
			}
		}
	}
	return ""
}

func isDeploymentEnvironmentToken(token string) bool {
	switch token {
	case "dev", "development", "test", "qa", "stage", "staging", "uat", "preprod", "prod", "production", "sandbox", "preview":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
