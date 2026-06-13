package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// content_entities and content_files hold the current indexed snapshot for a
// repository, keyed by repo_id (rows are overwritten on re-index, not kept per
// generation). The scope's repository is resolved from
// ingestion_scopes.payload->>'repo_id'; content indexes shortly after a
// generation's ingested_at, so there is no per-generation content history to
// bound by. The projection reads the current snapshot and the writer tags the
// resulting facts with the intent's generation; the active-generation reader and
// generation-scoped retirement keep the read model converged.
const loadEshuSearchDocumentEntitiesQuery = `
SELECT entity_id,
       repo_id,
       COALESCE(relative_path, ''),
       COALESCE(entity_type, ''),
       COALESCE(entity_name, ''),
       COALESCE(start_line, 0),
       COALESCE(end_line, 0),
       COALESCE(language, ''),
       COALESCE(artifact_type, ''),
       COALESCE(source_cache, ''),
       COALESCE(metadata, '{}'::jsonb),
       indexed_at
FROM content_entities
WHERE repo_id = (SELECT payload->>'repo_id' FROM ingestion_scopes WHERE scope_id = $1)
ORDER BY entity_id
`

const loadEshuSearchDocumentFilesQuery = `
SELECT repo_id,
       COALESCE(relative_path, ''),
       COALESCE(language, ''),
       COALESCE(artifact_type, ''),
       COALESCE(content, ''),
       indexed_at
FROM content_files
WHERE repo_id = (SELECT payload->>'repo_id' FROM ingestion_scopes WHERE scope_id = $1)
ORDER BY relative_path
`

// EshuSearchDocumentSourceLoader loads the current indexed content for a scope's
// repository as curated-search projection inputs. It implements
// reducer.SearchDocumentSourceLoader.
type EshuSearchDocumentSourceLoader struct {
	db ExecQueryer
}

// NewEshuSearchDocumentSourceLoader builds a content source loader over db.
func NewEshuSearchDocumentSourceLoader(db ExecQueryer) EshuSearchDocumentSourceLoader {
	return EshuSearchDocumentSourceLoader{db: db}
}

// LoadSearchDocumentSources returns the bounded content set for the scope. The
// generationID is part of the contract and tags the written facts, but the
// content snapshot is the repository's current indexed state, so the load is
// keyed by scope alone. Runtime summaries are not yet a content-store source.
func (l EshuSearchDocumentSourceLoader) LoadSearchDocumentSources(
	ctx context.Context,
	scopeID string,
	generationID string,
) (reducer.SearchDocumentProjectionInput, error) {
	if l.db == nil {
		return reducer.SearchDocumentProjectionInput{}, fmt.Errorf("eshu search document source loader requires a database")
	}
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" || strings.TrimSpace(generationID) == "" {
		return reducer.SearchDocumentProjectionInput{}, fmt.Errorf("eshu search document source loader requires scope and generation")
	}

	entities, err := l.loadEntities(ctx, scopeID)
	if err != nil {
		return reducer.SearchDocumentProjectionInput{}, err
	}
	files, err := l.loadFiles(ctx, scopeID)
	if err != nil {
		return reducer.SearchDocumentProjectionInput{}, err
	}
	return reducer.SearchDocumentProjectionInput{ContentEntities: entities, ContentFiles: files}, nil
}

func (l EshuSearchDocumentSourceLoader) loadEntities(ctx context.Context, scopeID string) ([]searchdocs.ContentEntity, error) {
	rows, err := l.db.QueryContext(ctx, loadEshuSearchDocumentEntitiesQuery, scopeID)
	if err != nil {
		return nil, fmt.Errorf("load search document content entities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entities []searchdocs.ContentEntity
	for rows.Next() {
		var (
			entity      searchdocs.ContentEntity
			metadataRaw []byte
			indexedAt   time.Time
		)
		if err := rows.Scan(
			&entity.EntityID,
			&entity.RepoID,
			&entity.RelativePath,
			&entity.EntityType,
			&entity.EntityName,
			&entity.StartLine,
			&entity.EndLine,
			&entity.Language,
			&entity.ArtifactType,
			&entity.SourceCache,
			&metadataRaw,
			&indexedAt,
		); err != nil {
			return nil, fmt.Errorf("scan search document content entity: %w", err)
		}
		entity.Metadata = decodeEshuSearchDocumentMetadata(metadataRaw)
		entity.IndexedAt = indexedAt
		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search document content entities: %w", err)
	}
	return entities, nil
}

func (l EshuSearchDocumentSourceLoader) loadFiles(ctx context.Context, scopeID string) ([]searchdocs.ContentFile, error) {
	rows, err := l.db.QueryContext(ctx, loadEshuSearchDocumentFilesQuery, scopeID)
	if err != nil {
		return nil, fmt.Errorf("load search document content files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var files []searchdocs.ContentFile
	for rows.Next() {
		var (
			file      searchdocs.ContentFile
			indexedAt time.Time
		)
		if err := rows.Scan(
			&file.RepoID,
			&file.RelativePath,
			&file.Language,
			&file.ArtifactType,
			&file.Content,
			&indexedAt,
		); err != nil {
			return nil, fmt.Errorf("scan search document content file: %w", err)
		}
		file.IndexedAt = indexedAt
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search document content files: %w", err)
	}
	return files, nil
}

// decodeEshuSearchDocumentMetadata best-effort decodes content entity metadata
// JSON into a string map, returning nil on any malformed payload so projection
// proceeds without the metadata-based exclusion signal rather than failing.
func decodeEshuSearchDocumentMetadata(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil
	}
	if len(generic) == 0 {
		return nil
	}
	metadata := make(map[string]string, len(generic))
	for key, value := range generic {
		if str, ok := value.(string); ok {
			metadata[key] = str
		}
	}
	return metadata
}
