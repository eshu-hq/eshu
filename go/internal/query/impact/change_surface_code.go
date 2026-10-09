// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// change_surface_code.go holds the content-store (Postgres) side of
// change-surface resolution -- code-topic evidence and changed-path symbol
// lookup -- split out of change_surface_response.go (which owns the
// graph-traversal impact rows and final response shaping) to keep both files
// under the repo's file-length cap.

// writeCodeSurfaceUnavailable answers a failed change-surface code-evidence
// read with a fixed-text 503. Call it after querycontract.WriteGraphReadError
// has declined err. The 503 marks the request span the way
// tracing.WriteServerFailure does (err recorded, status Error with message);
// tracing.WriteServerFailure itself cannot answer 503. A client cancel answers
// 499 through tracing.WriteServerFailure like every other server failure.
func writeCodeSurfaceUnavailable(w http.ResponseWriter, r *http.Request, err error, message string) {
	if errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled) {
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, message)
		return
	}
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)
	span.SetStatus(codes.Error, message)
	querycontract.WriteError(w, http.StatusServiceUnavailable, message)
}

// writePreChangeImpactFailure answers a failed pre-change or developer-plan
// read that querycontract.WriteGraphReadError declined: 404 for a repository
// outside the caller's grant, the fixed codeEvidenceMessage 503 for a
// code-evidence failure, and the fixed queryMessage 500 for anything else.
func writePreChangeImpactFailure(w http.ResponseWriter, r *http.Request, err error, queryMessage, codeEvidenceMessage string) {
	switch preChangeImpactErrorStatus(err) {
	case http.StatusNotFound:
		querycontract.WriteError(w, http.StatusNotFound, ErrChangeSurfaceRepoNotGranted.Error())
	case http.StatusServiceUnavailable:
		writeCodeSurfaceUnavailable(w, r, err, codeEvidenceMessage)
	default:
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, queryMessage)
	}
}

func (h *Handler) changeSurfacePathSymbols(
	ctx context.Context,
	req ChangeSurfaceInvestigationRequest,
) ([]map[string]any, bool, error) {
	entities, err := h.Content.ListRepoEntitiesByPaths(ctx, req.RepoID, req.ChangedPaths, req.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list repo entities by changed paths: %w", err)
	}
	symbols := make([]map[string]any, 0)
	for _, entity := range entities {
		symbols = append(symbols, map[string]any{
			"entity_id":     entity.EntityID,
			"entity_name":   entity.EntityName,
			"entity_type":   entity.EntityType,
			"repo_id":       entity.RepoID,
			"relative_path": entity.RelativePath,
			"language":      entity.Language,
			"start_line":    entity.StartLine,
			"end_line":      entity.EndLine,
			"source_handle": map[string]any{
				"repo_id":       entity.RepoID,
				"relative_path": entity.RelativePath,
				"start_line":    entity.StartLine,
				"end_line":      entity.EndLine,
			},
		})
	}
	truncated := len(symbols) > req.Limit
	if truncated {
		symbols = symbols[:req.Limit]
	}
	return symbols, truncated, nil
}
