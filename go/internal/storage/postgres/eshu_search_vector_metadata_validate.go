// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"fmt"
	"strings"
)

func normalizeEshuSearchVectorMetadata(row EshuSearchVectorMetadata) EshuSearchVectorMetadata {
	row.ScopeID = strings.TrimSpace(row.ScopeID)
	row.GenerationID = strings.TrimSpace(row.GenerationID)
	row.DocumentID = strings.TrimSpace(row.DocumentID)
	row.ProviderProfileID = strings.TrimSpace(row.ProviderProfileID)
	row.SourceClass = strings.TrimSpace(row.SourceClass)
	row.EmbeddingModelID = strings.TrimSpace(row.EmbeddingModelID)
	row.EmbeddingContentHash = strings.TrimSpace(row.EmbeddingContentHash)
	row.VectorIndexVersion = strings.TrimSpace(row.VectorIndexVersion)
	row.FailureClass = strings.TrimSpace(row.FailureClass)
	row.BuildState = EshuSearchVectorBuildState(strings.TrimSpace(string(row.BuildState)))
	return row
}

func validateEshuSearchVectorMetadata(row EshuSearchVectorMetadata) error {
	var problems []error
	if row.ScopeID == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires scope id"))
	}
	if row.GenerationID == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires generation id"))
	}
	if row.DocumentID == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires document id"))
	}
	if row.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires provider profile id"))
	}
	if row.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires source class"))
	}
	if row.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires embedding model id"))
	}
	if row.EmbeddingDimensions <= 0 {
		problems = append(problems, errors.New("eshu search vector metadata requires positive embedding dimensions"))
	}
	if row.EmbeddingContentHash == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires embedding content hash"))
	}
	if row.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector metadata requires vector index version"))
	}
	if !validEshuSearchVectorBuildState(row.BuildState) {
		problems = append(problems, fmt.Errorf("invalid eshu search vector build state %q", row.BuildState))
	}
	if row.CreatedAt.IsZero() {
		problems = append(problems, errors.New("eshu search vector metadata requires created_at"))
	}
	if row.UpdatedAt.IsZero() {
		problems = append(problems, errors.New("eshu search vector metadata requires updated_at"))
	}
	return errors.Join(problems...)
}

func normalizeEshuSearchVectorMetadataFilter(filter EshuSearchVectorMetadataFilter) EshuSearchVectorMetadataFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.ProviderProfileID = strings.TrimSpace(filter.ProviderProfileID)
	filter.SourceClass = strings.TrimSpace(filter.SourceClass)
	filter.EmbeddingModelID = strings.TrimSpace(filter.EmbeddingModelID)
	filter.VectorIndexVersion = strings.TrimSpace(filter.VectorIndexVersion)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	return filter
}

func validateEshuSearchVectorMetadataFilter(filter EshuSearchVectorMetadataFilter) error {
	var problems []error
	if filter.ScopeID == "" {
		problems = append(problems, errors.New("eshu search vector metadata filter requires scope id"))
	}
	if filter.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector metadata filter requires provider profile id"))
	}
	if filter.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector metadata filter requires source class"))
	}
	if filter.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector metadata filter requires embedding model id"))
	}
	if filter.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector metadata filter requires vector index version"))
	}
	return errors.Join(problems...)
}

func normalizeEshuSearchVectorStatusRequest(req EshuSearchVectorStatusRequest) EshuSearchVectorStatusRequest {
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.ProviderProfileID = strings.TrimSpace(req.ProviderProfileID)
	req.SourceClass = strings.TrimSpace(req.SourceClass)
	req.EmbeddingModelID = strings.TrimSpace(req.EmbeddingModelID)
	req.VectorIndexVersion = strings.TrimSpace(req.VectorIndexVersion)
	return req
}

func validateEshuSearchVectorStatusRequest(req EshuSearchVectorStatusRequest) error {
	var problems []error
	if req.ScopeID == "" {
		problems = append(problems, errors.New("eshu search vector status requires scope id"))
	}
	if req.ProviderProfileID == "" {
		problems = append(problems, errors.New("eshu search vector status requires provider profile id"))
	}
	if req.SourceClass == "" {
		problems = append(problems, errors.New("eshu search vector status requires source class"))
	}
	if req.EmbeddingModelID == "" {
		problems = append(problems, errors.New("eshu search vector status requires embedding model id"))
	}
	if req.VectorIndexVersion == "" {
		problems = append(problems, errors.New("eshu search vector status requires vector index version"))
	}
	return errors.Join(problems...)
}

func validEshuSearchVectorBuildState(state EshuSearchVectorBuildState) bool {
	switch state {
	case EshuSearchVectorBuildStateDisabled,
		EshuSearchVectorBuildStateQueued,
		EshuSearchVectorBuildStateBuilding,
		EshuSearchVectorBuildStateReady,
		EshuSearchVectorBuildStateFailed,
		EshuSearchVectorBuildStateStale:
		return true
	default:
		return false
	}
}
