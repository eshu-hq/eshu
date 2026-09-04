// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const (
	entityMapCapability   = "platform_impact.entity_map"
	entityMapDefaultLimit = 25
	entityMapMaxLimit     = 100
	entityMapDefaultDepth = 1
	entityMapMaxDepth     = 4
)

type entityMapRequest struct {
	From         string `json:"from"`
	FromType     string `json:"from_type"`
	RepoID       string `json:"repo_id"`
	Environment  string `json:"environment"`
	Relationship string `json:"relationship"`
	Depth        int    `json:"depth"`
	Limit        int    `json:"limit"`
	OriginalFrom string `json:"-"`
}

type entityMapCandidate struct {
	ID             string
	Name           string
	Labels         []string
	RepoID         string
	Environment    string
	AnchorLabel    string
	AnchorProperty string
	AnchorValue    string
}

type entityMapResolverQuery struct {
	cypher string
	params map[string]any
}

func (h *ImpactHandler) entityMap(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryEntityMap,
		"POST /api/v0/impact/entity-map",
		entityMapCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), entityMapCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"entity map requires authoritative platform graph truth",
			ErrorCodeUnsupportedCapability,
			entityMapCapability,
			h.profile(),
			requiredProfile(entityMapCapability),
		)
		return
	}

	var req entityMapRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.normalize(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	selected, resolution, err := h.resolveEntityMapStart(r.Context(), req)
	if err != nil {
		if WriteGraphReadError(w, r, err, entityMapCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if selected == nil {
		resp := entityMapResponse(req, resolution, nil, false)
		WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
			h.profile(),
			entityMapCapability,
			TruthBasisHybrid,
			"resolved entity map ambiguity before graph traversal",
		))
		return
	}

	span.SetAttributes(
		attribute.Int("eshu.entity_map.depth", req.Depth),
		attribute.Int("eshu.entity_map.limit", req.Limit),
		attribute.Bool("eshu.entity_map.relationship_filter", req.Relationship != ""),
	)
	traversalStart := time.Now()
	rows, truncated, err := h.entityMapNeighborhoodRows(r.Context(), req, *selected)
	span.SetAttributes(attribute.Float64("eshu.entity_map.traversal_seconds", time.Since(traversalStart).Seconds()))
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("eshu.entity_map.traversal_error", true))
		if WriteGraphReadError(w, r, err, entityMapCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	span.SetAttributes(
		attribute.Int("eshu.entity_map.result_count", len(rows)),
		attribute.Bool("eshu.entity_map.truncated", truncated),
	)
	resp := entityMapResponse(req, resolution, rows, truncated)
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
		h.profile(),
		entityMapCapability,
		TruthBasisHybrid,
		"resolved from typed entity resolution and bounded graph neighborhood traversal",
	))
}

func (r *entityMapRequest) normalize() error {
	r.From = strings.TrimSpace(r.From)
	r.OriginalFrom = r.From
	r.FromType = normalizeEntityMapType(r.FromType)
	r.RepoID = strings.TrimSpace(r.RepoID)
	r.Environment = strings.TrimSpace(r.Environment)
	r.Relationship = strings.ToUpper(strings.TrimSpace(r.Relationship))
	if strings.HasPrefix(r.From, "terraform/") && r.FromType == "" {
		r.FromType = "terraform_resource"
		r.From = strings.TrimPrefix(r.From, "terraform/")
	}
	if strings.HasPrefix(r.From, "k8s/") && r.FromType == "" {
		r.FromType = "k8s_resource"
		r.From = strings.TrimPrefix(r.From, "k8s/")
	}
	if r.Depth <= 0 {
		r.Depth = entityMapDefaultDepth
	}
	if r.Depth > entityMapMaxDepth {
		r.Depth = entityMapMaxDepth
	}
	if r.Limit <= 0 {
		r.Limit = entityMapDefaultLimit
	}
	if r.Limit > entityMapMaxLimit {
		r.Limit = entityMapMaxLimit
	}
	if r.From == "" {
		return fmt.Errorf("from is required")
	}
	if r.Relationship != "" && !validEntityMapRelationshipType(r.Relationship) {
		return fmt.Errorf("relationship must be an uppercase graph relationship type")
	}
	return nil
}

func (r entityMapRequest) responseFrom() string {
	if r.OriginalFrom != "" {
		return r.OriginalFrom
	}
	return r.From
}

func normalizeEntityMapType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "service", "workload", "workload_instance", "repository", "repo", "resource", "cloud", "cloud_resource", "terraform", "tf", "terraform_resource", "terraform_datasource", "k8s", "kubernetes", "k8s_resource", "terraform_module", "module", "file":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func validEntityMapRelationshipType(value string) bool {
	if value == "" {
		return true
	}
	for index, char := range value {
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if index > 0 && char >= '0' && char <= '9' {
			continue
		}
		if index > 0 && char == '_' {
			continue
		}
		return false
	}
	return true
}
