// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// updateSingleMetadataOnlyContentFileQuery is the single-row twin of the
// batched metadata-only UPDATE (#8950): it rewrites one row whose content and
// guarded columns equal the incoming record but whose commit_sha or
// generation_id differs, moving only the lineage so the update stays HOT.
// The parameter order mirrors the batch VALUES list (repo, path, lineage,
// content identity, content, size, guarded columns) to keep the two shapes
// reviewable side by side. Text parameters infer their type from the
// comparison; only the non-text comparisons carry casts. A row it does not
// touch (insert, content change, or an identical row) falls through to the
// guarded upsert, which keeps last-writer-wins.
const updateSingleMetadataOnlyContentFileQuery = `
UPDATE content_files AS c SET commit_sha = $3, generation_id = $4
WHERE c.repo_id = $1 AND c.relative_path = $2
  AND c.content_hash = $5
  AND octet_length(c.content) = $6::bigint
  AND c.content IS NOT DISTINCT FROM $7
  AND c.line_count IS NOT DISTINCT FROM $8::integer
  AND c.language IS NOT DISTINCT FROM $9
  AND c.artifact_type IS NOT DISTINCT FROM $10
  AND c.template_dialect IS NOT DISTINCT FROM $11
  AND c.iac_relevant IS NOT DISTINCT FROM $12::boolean
  AND (c.commit_sha IS DISTINCT FROM $3 OR c.generation_id IS DISTINCT FROM $4)
RETURNING c.repo_id, c.relative_path`

// UpsertFileBatch persists file content records in batches, tagging every
// row with the writing generation (#7760) so active-generation reads can
// tell current content from ahead writes.
func (s ContentStore) UpsertFileBatch(
	ctx context.Context,
	repoID string,
	generationID string,
	files []content.Record,
) error {
	if s.database == nil {
		return fmt.Errorf("content store database is required")
	}
	if strings.TrimSpace(repoID) == "" {
		return fmt.Errorf("content store repo_id is required")
	}
	if len(files) == 0 {
		return nil
	}

	indexedAt := s.now()

	// Blank writing generations bind NULL through the shared tag rule.
	tag := contentFileTagParam(generationID)

	for _, record := range files {
		if strings.TrimSpace(record.Path) == "" {
			return fmt.Errorf("content record path is required")
		}

		if record.Deleted {
			if _, err := s.database.ExecContext(ctx, deleteContentEntityQuery, repoID, record.Path); err != nil {
				return fmt.Errorf("delete content_entities for %q: %w", record.Path, err)
			}
			if _, err := s.database.ExecContext(ctx, deleteContentFileQuery, repoID, record.Path); err != nil {
				return fmt.Errorf("delete content_files for %q: %w", record.Path, err)
			}
			continue
		}

		contentHash, err := fileContentHash(record)
		if err != nil {
			return fmt.Errorf("derive content hash for %q: %w", record.Path, err)
		}

		commitSHA, err := optionalMetadataText(record.Metadata, "commit_sha")
		if err != nil {
			return fmt.Errorf("commit_sha metadata for %q: %w", record.Path, err)
		}
		language, err := optionalMetadataText(record.Metadata, "language")
		if err != nil {
			return fmt.Errorf("language metadata for %q: %w", record.Path, err)
		}
		artifactType, err := optionalMetadataText(record.Metadata, "artifact_type")
		if err != nil {
			return fmt.Errorf("artifact_type metadata for %q: %w", record.Path, err)
		}
		templateDialect, err := optionalMetadataText(record.Metadata, "template_dialect")
		if err != nil {
			return fmt.Errorf("template_dialect metadata for %q: %w", record.Path, err)
		}
		iacRelevant, err := optionalMetadataBool(record.Metadata, "iac_relevant")
		if err != nil {
			return fmt.Errorf("iac_relevant metadata for %q: %w", record.Path, err)
		}

		// #8950: a lineage-only rewrite stays HOT through the
		// metadata-only UPDATE, which leaves content and indexed_at alone.
		// Anything it misses (insert, content change, or an identical row
		// whose lineage already matches) falls through to the guarded
		// upsert, the last-writer-wins backstop.
		bodyLen := len(record.Body)
		bodyLineCount := lineCount(record.Body)
		updated, err := s.updateMetadataOnlyContentFile(
			ctx, repoID, record.Path, commitSHA, tag,
			contentHash, bodyLen, record.Body, bodyLineCount,
			language, artifactType, templateDialect, iacRelevant,
		)
		if err != nil {
			return err
		}
		if updated {
			continue
		}

		if _, err := s.database.ExecContext(
			ctx,
			upsertContentFileQuery,
			repoID,
			record.Path,
			commitSHA,
			record.Body,
			contentHash,
			bodyLineCount,
			language,
			artifactType,
			templateDialect,
			iacRelevant,
			indexedAt,
			tag,
		); err != nil {
			return fmt.Errorf("upsert content_files for %q: %w", record.Path, err)
		}
	}

	return nil
}

// updateMetadataOnlyContentFile runs the single-row metadata-only UPDATE and
// reports whether it rewrote the row. A concurrent content edit between a
// caller-side read and this UPDATE misses the content recheck and falls
// through to the guarded upsert, which keeps last-writer-wins.
func (s ContentStore) updateMetadataOnlyContentFile(
	ctx context.Context,
	repoID, path string,
	commitSHA, generation any,
	contentHash string,
	bodyLen int,
	body string,
	bodyLineCount int,
	language, artifactType, templateDialect, iacRelevant any,
) (bool, error) {
	rows, err := s.database.QueryContext(
		ctx,
		updateSingleMetadataOnlyContentFileQuery,
		repoID,
		path,
		commitSHA,
		generation,
		contentHash,
		bodyLen,
		body,
		bodyLineCount,
		language,
		artifactType,
		templateDialect,
		iacRelevant,
	)
	if err != nil {
		return false, fmt.Errorf("update metadata-only content_files for %q: %w", path, err)
	}
	defer func() { _ = rows.Close() }()
	updated := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read updated metadata-only content_files key for %q: %w", path, err)
	}
	return updated, nil
}

// UpsertEntityBatch persists entity content records in batches.
func (s ContentStore) UpsertEntityBatch(
	ctx context.Context,
	repoID string,
	entities []content.EntityRecord,
) error {
	if s.database == nil {
		return fmt.Errorf("content store database is required")
	}
	if strings.TrimSpace(repoID) == "" {
		return fmt.Errorf("content store repo_id is required")
	}
	if len(entities) == 0 {
		return nil
	}

	indexedAt := s.now()

	for _, entity := range entities {
		if strings.TrimSpace(entity.EntityID) == "" {
			return fmt.Errorf("content entity id is required")
		}
		if strings.TrimSpace(entity.Path) == "" {
			return fmt.Errorf("content entity path is required")
		}
		if strings.TrimSpace(entity.EntityType) == "" {
			return fmt.Errorf("content entity type is required for %q", entity.EntityID)
		}
		if strings.TrimSpace(entity.EntityName) == "" {
			return fmt.Errorf("content entity name is required for %q", entity.EntityID)
		}
		if entity.StartLine <= 0 {
			return fmt.Errorf("content entity start line is required for %q", entity.EntityID)
		}

		endLine := entity.EndLine
		if endLine < entity.StartLine {
			endLine = entity.StartLine
		}
		sourceCache := strings.TrimSpace(entity.SourceCache)

		if entity.Deleted {
			if _, err := s.database.ExecContext(
				ctx,
				deleteContentEntityByIDQuery,
				repoID,
				entity.EntityID,
			); err != nil {
				return fmt.Errorf("delete content_entities by entity_id for %q: %w", entity.EntityID, err)
			}
			continue
		}

		// Same #7172 write contract as ContentWriter.Write: fingerprint
		// keys never reach content_entities.metadata.
		mdJSON, err := metadataJSON(persistedEntityMetadata(entity.Metadata))
		if err != nil {
			return fmt.Errorf("marshal content entity metadata for %q: %w", entity.EntityID, err)
		}

		if _, err := s.database.ExecContext(
			ctx,
			upsertContentEntityQuery,
			entity.EntityID,
			repoID,
			entity.Path,
			entity.EntityType,
			entity.EntityName,
			entity.StartLine,
			endLine,
			optionalInt(entity.StartByte),
			optionalInt(entity.EndByte),
			optionalString(entity.Language),
			optionalString(entity.ArtifactType),
			optionalString(entity.TemplateDialect),
			optionalBool(entity.IACRelevant),
			sourceCache,
			mdJSON,
			indexedAt,
		); err != nil {
			return fmt.Errorf("upsert content_entities for %q: %w", entity.EntityID, err)
		}
	}

	return nil
}
