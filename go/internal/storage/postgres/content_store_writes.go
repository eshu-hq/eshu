package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// UpsertFileBatch persists file content records in batches.
func (s ContentStore) UpsertFileBatch(
	ctx context.Context,
	repoID string,
	files []content.Record,
) error {
	if s.db == nil {
		return fmt.Errorf("content store database is required")
	}
	if strings.TrimSpace(repoID) == "" {
		return fmt.Errorf("content store repo_id is required")
	}
	if len(files) == 0 {
		return nil
	}

	indexedAt := s.now()

	for _, record := range files {
		if strings.TrimSpace(record.Path) == "" {
			return fmt.Errorf("content record path is required")
		}

		if record.Deleted {
			if _, err := s.db.ExecContext(ctx, deleteContentEntityQuery, repoID, record.Path); err != nil {
				return fmt.Errorf("delete content_entities for %q: %w", record.Path, err)
			}
			if _, err := s.db.ExecContext(ctx, deleteContentFileQuery, repoID, record.Path); err != nil {
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

		if _, err := s.db.ExecContext(
			ctx,
			upsertContentFileQuery,
			repoID,
			record.Path,
			commitSHA,
			record.Body,
			contentHash,
			lineCount(record.Body),
			language,
			artifactType,
			templateDialect,
			iacRelevant,
			indexedAt,
		); err != nil {
			return fmt.Errorf("upsert content_files for %q: %w", record.Path, err)
		}
	}

	return nil
}

// UpsertEntityBatch persists entity content records in batches.
func (s ContentStore) UpsertEntityBatch(
	ctx context.Context,
	repoID string,
	entities []content.EntityRecord,
) error {
	if s.db == nil {
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
			if _, err := s.db.ExecContext(
				ctx,
				deleteContentEntityByIDQuery,
				repoID,
				entity.EntityID,
			); err != nil {
				return fmt.Errorf("delete content_entities by entity_id for %q: %w", entity.EntityID, err)
			}
			continue
		}

		mdJSON, err := metadataJSON(entity.Metadata)
		if err != nil {
			return fmt.Errorf("marshal content entity metadata for %q: %w", entity.EntityID, err)
		}

		if _, err := s.db.ExecContext(
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
