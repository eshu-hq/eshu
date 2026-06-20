package searchvector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const defaultBuildLimit = 500

const (
	// FailureClassEmbedder records a bounded embedder failure.
	FailureClassEmbedder = "embedder_error"
	// FailureClassInvalidVector records a malformed vector returned by an embedder.
	FailureClassInvalidVector = "invalid_vector"
)

// DocumentStore reads active curated search documents for a bounded scope.
type DocumentStore interface {
	ListActiveDocuments(context.Context, postgres.EshuSearchDocumentFilter) ([]postgres.EshuSearchDocumentRow, error)
}

// MetadataStore persists vector build metadata rows.
type MetadataStore interface {
	Upsert(context.Context, postgres.EshuSearchVectorMetadata) error
}

// ValueStore persists vector payload rows.
type ValueStore interface {
	Upsert(context.Context, postgres.EshuSearchVectorValue) error
}

// Builder builds vector rows from active curated search documents.
type Builder struct {
	Documents DocumentStore
	Metadata  MetadataStore
	Values    ValueStore
	Embedder  searchhybrid.Embedder
	Clock     func() time.Time
}

// BuildRequest identifies the active search-document slice and vector identity
// to build.
type BuildRequest struct {
	ScopeID            string
	RepoID             string
	SourceKinds        []searchdocs.SourceKind
	ProviderProfileID  string
	SourceClass        string
	EmbeddingModelID   string
	VectorIndexVersion string
	Limit              int
}

// BuildResult summarizes a vector build attempt.
type BuildResult struct {
	DocumentCount int
	VectorCount   int
	FailedCount   int
}

// Build embeds active search documents and upserts derived vector state.
func (b Builder) Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	req = normalizeBuildRequest(req)
	if err := b.validate(req); err != nil {
		return BuildResult{}, err
	}
	now := b.now()

	var result BuildResult
	var failures []error
	generationID := ""
	for offset := 0; ; {
		rows, err := b.Documents.ListActiveDocuments(ctx, postgres.EshuSearchDocumentFilter{
			ScopeID:      req.ScopeID,
			GenerationID: generationID,
			RepoID:       req.RepoID,
			SourceKinds:  req.SourceKinds,
			Limit:        req.Limit,
			Offset:       offset,
		})
		if err != nil {
			return result, fmt.Errorf("list active search documents for vector build: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		if generationID == "" {
			generationID = rows[0].GenerationID
		}
		for _, row := range rows {
			if row.GenerationID != generationID {
				return result, fmt.Errorf("active search document generation changed from %q to %q", generationID, row.GenerationID)
			}
			result.DocumentCount++
			vector, failureClass, err := b.embed(row.Document)
			if err != nil {
				if upsertErr := b.upsertMetadata(ctx, req, row, now, failureClass, nil); upsertErr != nil {
					return result, upsertErr
				}
				result.FailedCount++
				failures = append(failures, fmt.Errorf("%s: %w", failureClass, err))
				continue
			}

			if err := b.Values.Upsert(ctx, postgres.EshuSearchVectorValue{
				ScopeID:              row.ScopeID,
				GenerationID:         row.GenerationID,
				DocumentID:           row.Document.ID,
				ProviderProfileID:    req.ProviderProfileID,
				SourceClass:          req.SourceClass,
				EmbeddingModelID:     req.EmbeddingModelID,
				EmbeddingDimensions:  b.Embedder.Dimensions(),
				EmbeddingContentHash: searchhybrid.DocumentContentHash(row.Document),
				VectorIndexVersion:   req.VectorIndexVersion,
				VectorValues:         vector,
				CreatedAt:            now,
				UpdatedAt:            now,
			}); err != nil {
				return result, fmt.Errorf("upsert vector value for document %q: %w", row.Document.ID, err)
			}
			if err := b.upsertMetadata(ctx, req, row, now, "", &now); err != nil {
				return result, err
			}
			result.VectorCount++
		}
		offset += len(rows)
		if len(rows) < req.Limit {
			break
		}
	}
	return result, errors.Join(failures...)
}

func (b Builder) embed(doc searchdocs.Document) ([]float64, string, error) {
	vector, err := b.Embedder.Embed(searchhybrid.DocumentText(doc))
	if err != nil {
		return nil, FailureClassEmbedder, err
	}
	if err := validateVector(vector, b.Embedder.Dimensions()); err != nil {
		return nil, FailureClassInvalidVector, err
	}
	return vector, "", nil
}

func (b Builder) upsertMetadata(
	ctx context.Context,
	req BuildRequest,
	row postgres.EshuSearchDocumentRow,
	now time.Time,
	failureClass string,
	lastSuccessAt *time.Time,
) error {
	state := postgres.EshuSearchVectorBuildStateReady
	if failureClass != "" {
		state = postgres.EshuSearchVectorBuildStateFailed
	}
	if err := b.Metadata.Upsert(ctx, postgres.EshuSearchVectorMetadata{
		ScopeID:              row.ScopeID,
		GenerationID:         row.GenerationID,
		DocumentID:           row.Document.ID,
		ProviderProfileID:    req.ProviderProfileID,
		SourceClass:          req.SourceClass,
		EmbeddingModelID:     req.EmbeddingModelID,
		EmbeddingDimensions:  b.Embedder.Dimensions(),
		EmbeddingContentHash: searchhybrid.DocumentContentHash(row.Document),
		VectorIndexVersion:   req.VectorIndexVersion,
		BuildState:           state,
		FailureClass:         failureClass,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastSuccessAt:        lastSuccessAt,
	}); err != nil {
		return fmt.Errorf("upsert vector metadata for document %q: %w", row.Document.ID, err)
	}
	return nil
}

func (b Builder) validate(req BuildRequest) error {
	var problems []error
	if b.Documents == nil {
		problems = append(problems, errors.New("document store is required"))
	}
	if b.Metadata == nil {
		problems = append(problems, errors.New("metadata store is required"))
	}
	if b.Values == nil {
		problems = append(problems, errors.New("value store is required"))
	}
	if b.Embedder == nil {
		problems = append(problems, errors.New("embedder is required"))
	} else if b.Embedder.Dimensions() <= 0 {
		problems = append(problems, errors.New("embedder dimensions must be positive"))
	}
	if req.ScopeID == "" {
		problems = append(problems, errors.New("scope id is required"))
	}
	if req.ProviderProfileID == "" {
		problems = append(problems, errors.New("provider profile id is required"))
	}
	if req.SourceClass == "" {
		problems = append(problems, errors.New("source class is required"))
	}
	if req.EmbeddingModelID == "" {
		problems = append(problems, errors.New("embedding model id is required"))
	}
	if req.VectorIndexVersion == "" {
		problems = append(problems, errors.New("vector index version is required"))
	}
	return errors.Join(problems...)
}

func (b Builder) now() time.Time {
	if b.Clock != nil {
		return b.Clock().UTC()
	}
	return time.Now().UTC()
}

func normalizeBuildRequest(req BuildRequest) BuildRequest {
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.ProviderProfileID = strings.TrimSpace(req.ProviderProfileID)
	req.SourceClass = strings.TrimSpace(req.SourceClass)
	req.EmbeddingModelID = strings.TrimSpace(req.EmbeddingModelID)
	req.VectorIndexVersion = strings.TrimSpace(req.VectorIndexVersion)
	if req.Limit <= 0 || req.Limit > defaultBuildLimit {
		req.Limit = defaultBuildLimit
	}
	return req
}

func validateVector(vector []float64, dimensions int) error {
	if len(vector) != dimensions {
		return fmt.Errorf("vector length %d does not match dimensions %d", len(vector), dimensions)
	}
	for i, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("vector value at index %d is not finite", i)
		}
	}
	return nil
}
