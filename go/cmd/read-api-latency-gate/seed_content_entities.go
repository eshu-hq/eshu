// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// infraSeedRepoCount is how many repositories the anonymous infra content rows
// are spread across. The infra read model is derived one repository at a time,
// so a realistic spread keeps the per-repository cost visible instead of
// hiding it inside a single enormous repository.
const infraSeedRepoCount = 200

// contentEntityBatchSize bounds one COPY batch, so rows are shaped and written
// in chunks instead of holding the whole 1M-row corpus in memory.
const contentEntityBatchSize = 50000

// graphOnlyInfraLabels have no content_entities row in production: other
// collectors write those nodes, and the infra aggregate reads them from the
// graph. The seed mirrors that, so they stay graph-only here too.
var graphOnlyInfraLabels = map[string]bool{
	"TerraformStateResource": true,
	"CloudResource":          true,
}

// contentDerivedInfraLabels is infraLabels minus graphOnlyInfraLabels, in
// infraLabels order: the labels whose nodes also exist as content_entities
// rows, and so as rows of the derived infra read model.
func contentDerivedInfraLabels() []string {
	labels := make([]string, 0, len(infraLabels))
	for _, l := range infraLabels {
		if !graphOnlyInfraLabels[l] {
			labels = append(labels, l)
		}
	}
	return labels
}

// ContentEntityRow is one planned content_entities row, mirroring a seeded
// graph node.
type ContentEntityRow struct {
	EntityID     string
	RepoID       string
	RelativePath string
	EntityType   string
	EntityName   string
	Metadata     map[string]string
}

// contentEntityColumns is the content_entities column list contentEntityCopyRow
// shapes, in order. The other columns are nullable.
var contentEntityColumns = []string{
	"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
	"start_line", "end_line", "source_cache", "metadata", "indexed_at",
}

// labelMetadata is the label-specific metadata key the infra read model copies
// into a dimension column (kind for manifests, resource_type for IaC
// resources), so the seeded rows populate more than provider/environment.
var labelMetadata = map[string][2]string{
	"K8sResource":            {"kind", "Deployment"},
	"TerraformResource":      {"resource_type", "aws_instance"},
	"CloudFormationResource": {"resource_type", "AWS::EC2::Instance"},
	"ArgoCDApplication":      {"kind", "Application"},
	"CrossplaneXRD":          {"kind", "CompositeResourceDefinition"},
	"HelmChart":              {"kind", "Chart"},
}

// infraContentEntityRows builds the content_entities rows mirroring the bulk
// graph nodes infraNodeRows creates for the inclusive index range r: the same
// id, and the same provider and environment.
func infraContentEntityRows(label string, r idRange) []ContentEntityRow {
	rows := make([]ContentEntityRow, 0, r.Last-r.First+1)
	for i := r.First; i <= r.Last; i++ {
		meta := map[string]string{"provider": seedProvider(i), "environment": seedEnvironment(i)}
		if lm, ok := labelMetadata[label]; ok {
			meta[lm[0]] = lm[1]
		}
		rows = append(rows, ContentEntityRow{
			EntityID:     seedInfraID(label, i),
			RepoID:       fmt.Sprintf("seed-infra-repo-%03d", i%infraSeedRepoCount),
			RelativePath: fmt.Sprintf("infra/%s/%04d.tf", label, i/1000),
			EntityType:   label,
			EntityName:   seedInfraID(label, i),
			Metadata:     meta,
		})
	}
	return rows
}

// iacContentEntityRows builds the content_entities rows mirroring the
// correlated graph nodes SeedIaCGraphNodes creates: the fact's entity id, type
// and name.
func iacContentEntityRows(facts []SeedIaCFact) []ContentEntityRow {
	rows := make([]ContentEntityRow, 0, len(facts))
	for _, f := range facts {
		rows = append(rows, ContentEntityRow{
			EntityID:     f.EntityID,
			RepoID:       f.ScopeID,
			RelativePath: f.RelativePath,
			EntityType:   f.EntityType,
			EntityName:   f.EntityName,
			Metadata:     map[string]string{"provider": f.Provider, "resource_type": f.ResourceType},
		})
	}
	return rows
}

// contentEntityCopyRow shapes row into a COPY row matching contentEntityColumns.
func contentEntityCopyRow(row ContentEntityRow, now time.Time) ([]any, error) {
	metadata, err := json.Marshal(row.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata for %s: %w", row.EntityID, err)
	}
	return []any{
		row.EntityID, row.RepoID, row.RelativePath, row.EntityType, row.EntityName,
		1, 10, "", metadata, now,
	}, nil
}

// expectedContentEntityCounts is the per-entity-type row count a correctly
// seeded content_entities table holds: nodesPerLabel rows for every
// content-derived infra label, plus one row per SeedIaCFact of its type. It
// equals expectedGraphNodeCounts for the same labels by construction.
func expectedContentEntityCounts(nodesPerLabel int, facts []SeedIaCFact) map[string]int {
	expected := make(map[string]int, len(infraLabels)+len(iacEntityTypes))
	for _, l := range contentDerivedInfraLabels() {
		expected[l] = nodesPerLabel
	}
	for _, f := range facts {
		expected[f.EntityType]++
	}
	return expected
}

// SeedInfraContentEntities bulk-inserts the content_entities rows mirroring the
// seeded graph, via COPY in bounded batches. The rows are written by a plain
// connection with no derive-aware session, exactly like a writer that predates
// the infra read model: where that read model exists its fence marks every
// repository dirty, and the API's startup backfill (which is why eshu-api is
// started after the seed) re-derives them and clears the marks.
func SeedInfraContentEntities(ctx context.Context, pool *pgxpool.Pool, nodesPerLabel int, facts []SeedIaCFact, now time.Time) error {
	for _, label := range contentDerivedInfraLabels() {
		for _, r := range bulkNodeRanges(nodesPerLabel, contentEntityBatchSize) {
			if err := copyContentEntities(ctx, pool, infraContentEntityRows(label, r), now); err != nil {
				return fmt.Errorf("seed content_entities for %s nodes %d..%d: %w", label, r.First, r.Last, err)
			}
		}
	}
	all := iacContentEntityRows(facts)
	for start := 0; start < len(all); start += contentEntityBatchSize {
		end := start + contentEntityBatchSize
		if end > len(all) {
			end = len(all)
		}
		if err := copyContentEntities(ctx, pool, all[start:end], now); err != nil {
			return fmt.Errorf("seed content_entities for IaC entities %d..%d: %w", start, end-1, err)
		}
	}
	return nil
}

func copyContentEntities(ctx context.Context, pool *pgxpool.Pool, rows []ContentEntityRow, now time.Time) error {
	copyRows := make([][]any, 0, len(rows))
	for _, r := range rows {
		copyRow, err := contentEntityCopyRow(r, now)
		if err != nil {
			return err
		}
		copyRows = append(copyRows, copyRow)
	}
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"content_entities"}, contentEntityColumns, pgx.CopyFromRows(copyRows)); err != nil {
		return fmt.Errorf("copy content_entities: %w", err)
	}
	return nil
}

// VerifyContentEntityCounts reads back the per-entity-type row counts and fails
// when any differs from expected, for the same reason VerifyGraphNodeCounts
// exists: a seed that silently under-creates is a vacuous corpus.
func VerifyContentEntityCounts(ctx context.Context, pool *pgxpool.Pool, expected map[string]int) error {
	types := make([]string, 0, len(expected))
	for t := range expected {
		types = append(types, t)
	}
	rows, err := pool.Query(ctx, "SELECT entity_type, count(*) FROM content_entities WHERE entity_type = ANY($1) GROUP BY entity_type", types)
	if err != nil {
		return fmt.Errorf("count content_entities: %w", err)
	}
	defer rows.Close()
	actual := make(map[string]int, len(expected))
	for rows.Next() {
		var entityType string
		var count int64
		if err := rows.Scan(&entityType, &count); err != nil {
			return fmt.Errorf("scan content_entities count: %w", err)
		}
		actual[entityType] = int(count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("count content_entities: %w", err)
	}
	if mismatches := graphCountMismatches(expected, actual); len(mismatches) > 0 {
		return fmt.Errorf("seeded content_entities does not hold the expected row counts: %s", strings.Join(mismatches, "; "))
	}
	return nil
}
