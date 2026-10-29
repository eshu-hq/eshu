// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const ecsTaskReadLimit = 251

// ECSServiceTaskEvidence holds one active service observation and its bounded
// running-task facts. A nil result means no authorized active service fact.
type ECSServiceTaskEvidence struct {
	RunningCount *int
	Tasks        []ECSTaskFact
	ImageDigests []string
	Truncated    bool
}

// ECSTaskStore reads observed running tasks for an exact, workload-bound ECS
// service ARN. It does not infer ownership from a digest or service name.
type ECSTaskStore interface {
	ListRunningTasks(context.Context, string, querycontract.RepositoryAccessFilter) (*ECSServiceTaskEvidence, error)
}

// PostgresECSTaskStore uses the guarded read-only Postgres connection.
type PostgresECSTaskStore struct{ Reader db.Queryer }

// NewPostgresECSTaskStore binds ECS task reads to a query-only port.
func NewPostgresECSTaskStore(reader db.Queryer) PostgresECSTaskStore {
	return PostgresECSTaskStore{Reader: reader}
}

// ListRunningTasks reads one active ECS service and up to ecsTaskReadLimit
// running task facts from the same scope generation. Two service facts for
// one ARN are ambiguous and fail closed instead of doubling a count.
func (s PostgresECSTaskStore) ListRunningTasks(ctx context.Context, serviceARN string, access querycontract.RepositoryAccessFilter) (*ECSServiceTaskEvidence, error) {
	if s.Reader == nil {
		return nil, fmt.Errorf("ECS task reader is required")
	}
	svcIdx := strings.Index(serviceARN, ":service/")
	if svcIdx < 0 || !ecsServiceARNPrefixOK(serviceARN[:svcIdx]) {
		return nil, fmt.Errorf("exact ECS service ARN is required")
	}
	if access.Empty() {
		return nil, nil
	}
	query := ecsTaskReadAllScopesSQL
	args := []any{serviceARN}
	if access.Scoped() {
		query = ecsTaskReadScopedSQL
		args = append(args, array.Of(access.AllowedRepositoryIDs), array.Of(access.AllowedScopeIDs))
	}
	rows, err := s.Reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read ECS task facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var evidence *ECSServiceTaskEvidence
	serviceFactID := ""
	serviceScopeID := ""
	serviceGenerationID := ""
	for rows.Next() {
		var factID, scopeID, generationID string
		var servicePayload, taskPayload []byte
		if err := rows.Scan(&factID, &scopeID, &generationID, &servicePayload, &taskPayload); err != nil {
			return nil, fmt.Errorf("scan ECS task facts: %w", err)
		}
		if serviceFactID != "" && serviceFactID != factID {
			return nil, fmt.Errorf("multiple active ECS service facts for one ARN")
		}
		serviceFactID = factID
		serviceScopeID, serviceGenerationID = scopeID, generationID
		if evidence == nil {
			var service struct {
				ARN        string `json:"arn"`
				Name       string `json:"name"`
				Attributes struct {
					ClusterARN   string `json:"cluster_arn"`
					RunningCount *int   `json:"running_count"`
				} `json:"attributes"`
			}
			if err := json.Unmarshal(servicePayload, &service); err != nil {
				return nil, fmt.Errorf("decode ECS service fact: %w", err)
			}
			if !validECSServiceIdentity(serviceARN, service.ARN, service.Name, service.Attributes.ClusterARN) {
				return nil, fmt.Errorf("ECS service fact lacks exact cluster identity")
			}
			evidence = &ECSServiceTaskEvidence{RunningCount: service.Attributes.RunningCount}
		}
		if len(taskPayload) == 0 {
			continue
		}
		var task struct {
			ARN        string `json:"arn"`
			Attributes struct {
				Containers []struct {
					Name   string `json:"name"`
					Image  string `json:"image"`
					Digest string `json:"image_digest"`
				} `json:"containers"`
			} `json:"attributes"`
		}
		if err := json.Unmarshal(taskPayload, &task); err != nil {
			return nil, fmt.Errorf("decode ECS task fact: %w", err)
		}
		if !validECSTaskIdentity(serviceARN, task.ARN) {
			return nil, fmt.Errorf("ECS task fact disagrees with service account or cluster")
		}
		item := ECSTaskFact{ARN: task.ARN}
		for _, container := range task.Attributes.Containers {
			item.Containers = append(item.Containers, ECSContainerFact{Name: container.Name, Image: container.Image, Digest: container.Digest})
		}
		evidence.Tasks = append(evidence.Tasks, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ECS task facts: %w", err)
	}
	if evidence == nil {
		return nil, nil
	}
	if len(evidence.Tasks) == ecsTaskReadLimit {
		evidence.Truncated = true
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close ECS task rows: %w", err)
	}
	refs, err := s.Reader.QueryContext(ctx, ecsImageRefsReadSQL, serviceScopeID, serviceGenerationID)
	if err != nil {
		return nil, fmt.Errorf("read ECS image references: %w", err)
	}
	defer func() { _ = refs.Close() }()
	for refs.Next() {
		var digest string
		if err := refs.Scan(&digest); err != nil {
			return nil, fmt.Errorf("scan ECS image reference: %w", err)
		}
		evidence.ImageDigests = append(evidence.ImageDigests, digest)
	}
	if err := refs.Err(); err != nil {
		return nil, fmt.Errorf("iterate ECS image references: %w", err)
	}
	if len(evidence.ImageDigests) == ecsTaskReadLimit {
		evidence.Truncated = true
	}
	return evidence, nil
}

const (
	ecsTaskReadAllScopesSQL = ecsTaskReadPrefixSQL + ecsTaskReadSuffixSQL
	ecsTaskReadScopedSQL    = ecsTaskReadPrefixSQL + `
  AND (service.scope_id = ANY($2) OR service.scope_id = ANY($3))
` + ecsTaskReadSuffixSQL
)

const ecsTaskReadPrefixSQL = `
WITH service AS MATERIALIZED (
  SELECT service.fact_id, service.scope_id, service.generation_id, service.payload
  FROM fact_records AS service
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = service.scope_id
   AND scope.active_generation_id = service.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = service.scope_id
   AND generation.generation_id = service.generation_id
  WHERE service.fact_kind = 'aws_resource'
    AND service.is_tombstone = FALSE
    AND generation.status = 'active'
    AND service.payload->>'resource_type' = 'aws_ecs_service'
    AND service.payload->>'arn' = $1
`

const ecsTaskReadSuffixSQL = `
  ORDER BY service.fact_id
  LIMIT 2
)
SELECT service.fact_id, service.scope_id, service.generation_id, service.payload, task.payload
FROM service
LEFT JOIN LATERAL (
  SELECT task.payload
  FROM fact_records AS task
  WHERE task.scope_id = service.scope_id
    AND task.generation_id = service.generation_id
    AND task.fact_kind = 'aws_resource'
    AND task.is_tombstone = FALSE
    AND task.payload->>'resource_type' = 'aws_ecs_task'
    AND task.payload->>'state' = 'RUNNING'
    AND task.payload->'attributes'->>'cluster_arn' = service.payload->'attributes'->>'cluster_arn'
    AND task.payload->'attributes'->>'group' = 'service:' || (service.payload->>'name')
  ORDER BY task.payload->>'arn'
  LIMIT 251
) AS task ON TRUE
ORDER BY service.fact_id, task.payload->>'arn'
`

func validECSServiceIdentity(expectedARN, observedARN, observedName, clusterARN string) bool {
	if expectedARN != observedARN || observedName == "" {
		return false
	}
	prefix, path, ok := strings.Cut(expectedARN, ":service/")
	if !ok {
		return false
	}
	clusterName, serviceName, ok := strings.Cut(path, "/")
	if !ok || clusterName == "" || serviceName == "" || strings.Contains(serviceName, "/") || serviceName != observedName {
		return false
	}
	return clusterARN == prefix+":cluster/"+clusterName
}

func validECSTaskIdentity(serviceARN, taskARN string) bool {
	prefix, servicePath, ok := strings.Cut(serviceARN, ":service/")
	if !ok || !ecsServiceARNPrefixOK(prefix) {
		return false
	}
	clusterName, _, ok := strings.Cut(servicePath, "/")
	if !ok || clusterName == "" {
		return false
	}
	taskPrefix := prefix + ":task/" + clusterName + "/"
	taskID := strings.TrimPrefix(taskARN, taskPrefix)
	return strings.HasPrefix(taskARN, taskPrefix) && taskID != "" && !strings.Contains(taskID, "/")
}

const ecsImageRefsReadSQL = `
SELECT DISTINCT ref.payload->>'image_digest'
FROM fact_records AS ref
JOIN ingestion_scopes AS scope
  ON scope.scope_id = ref.scope_id
 AND scope.active_generation_id = ref.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = ref.scope_id
 AND generation.generation_id = ref.generation_id
WHERE ref.scope_id = $1
  AND ref.generation_id = $2
  AND ref.fact_kind = 'aws_image_reference'
  AND ref.is_tombstone = FALSE
  AND generation.status = 'active'
  AND ref.payload->>'service_kind' = 'ecs'
  AND NULLIF(BTRIM(ref.payload->>'image_digest'), '') IS NOT NULL
ORDER BY ref.payload->>'image_digest'
LIMIT 251
`
