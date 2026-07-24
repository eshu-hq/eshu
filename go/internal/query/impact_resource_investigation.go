// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	resourceInvestigationCapability   = "platform_impact.resource_investigation"
	resourceInvestigationDefaultLimit = 25
	resourceInvestigationMaxLimit     = 100
	resourceInvestigationDefaultDepth = 4
	resourceInvestigationMaxDepth     = 8
)

type resourceInvestigationRequest struct {
	Query        string `json:"query"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	Environment  string `json:"environment"`
	MaxDepth     int    `json:"max_depth"`
	Limit        int    `json:"limit"`
}

type resourceInvestigationCandidate struct {
	ID            string
	Name          string
	Labels        []string
	ResourceType  string
	Provider      string
	Environment   string
	RepoID        string
	ConfigPath    string
	Source        string
	ResourceID    string
	Arn           string
	ResourceKind  string
	ResourceClass string
}

func (h *ImpactHandler) investigateResource(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryResourceInvestigation,
		"POST /api/v0/impact/resource-investigation",
		resourceInvestigationCapability,
	)
	defer span.End()

	if capabilityUnsupported(h.profile(), resourceInvestigationCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"resource investigation requires authoritative platform truth",
			ErrorCodeUnsupportedCapability,
			resourceInvestigationCapability,
			h.profile(),
			requiredProfile(resourceInvestigationCapability),
		)
		return
	}

	var req resourceInvestigationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.normalize(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// #5167 W3: resourceInvestigationResolverCypher matches by free-text/exact
	// selector across every supported infra label with no repo scoping in the
	// Cypher itself, so an empty grant short-circuits to "no match" without
	// running the resolver query, and resolveResourceInvestigationTarget filters
	// the resolved candidates by the caller's grant below.
	if access := repositoryAccessFilterFromContext(r.Context()); access.empty() {
		resp := resourceInvestigationResponse(req, resourceInvestigationEmptyGrantResolution(req), nil, nil, nil, nil, false)
		WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
			h.profile(),
			resourceInvestigationCapability,
			TruthBasisHybrid,
			"resolved resource ambiguity before graph traversal",
		))
		return
	}

	selected, resolution, err := h.resolveResourceInvestigationTarget(r.Context(), req)
	if err != nil {
		if WriteGraphReadError(w, r, err, resourceInvestigationCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if selected == nil {
		resp := resourceInvestigationResponse(req, resolution, nil, nil, nil, nil, false)
		WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
			h.profile(),
			resourceInvestigationCapability,
			TruthBasisHybrid,
			"resolved resource ambiguity before graph traversal",
		))
		return
	}

	workloads, workloadsTruncated, incomingPaths, incomingTruncated, outgoingPaths, outgoingTruncated, err := h.loadResourceInvestigationSections(
		r.Context(), req, selected, repositoryAccessFilterFromContext(r.Context()),
	)
	if err != nil {
		if WriteGraphReadError(w, r, err, resourceInvestigationCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := workloadsTruncated || incomingTruncated || outgoingTruncated
	resp := resourceInvestigationResponse(req, resolution, selected, workloads, incomingPaths, outgoingPaths, truncated)
	WriteSuccess(w, r, http.StatusOK, resp, BuildTruthEnvelope(
		h.profile(),
		resourceInvestigationCapability,
		TruthBasisHybrid,
		"resolved from bounded resource resolution, workload usage, and repository provenance paths",
	))
}

func (h *ImpactHandler) loadResourceInvestigationSections(
	ctx context.Context,
	req resourceInvestigationRequest,
	selected *resourceInvestigationCandidate,
	access repositoryAccessFilter,
) (
	[]map[string]any,
	bool,
	[]map[string]any,
	bool,
	[]map[string]any,
	bool,
	error,
) {
	if resourceInvestigationAnchorLabel(selected) == "" {
		return nil, false, nil, false, nil, false, fmt.Errorf(
			"resolved resource %q has no supported infrastructure label",
			selected.ID,
		)
	}

	var (
		workloads          []map[string]any
		workloadsTruncated bool
		incomingPaths      []map[string]any
		incomingTruncated  bool
		outgoingPaths      []map[string]any
		outgoingTruncated  bool
		wg                 sync.WaitGroup
	)
	errCh := make(chan error, 3)
	wg.Add(3)
	go func() {
		defer wg.Done()
		var err error
		workloads, workloadsTruncated, err = h.resourceInvestigationWorkloads(ctx, req, selected, access)
		if err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		incomingPaths, incomingTruncated, err = h.resourceInvestigationRepoPaths(ctx, req, selected, "incoming", access)
		if err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		outgoingPaths, outgoingTruncated, err = h.resourceInvestigationRepoPaths(ctx, req, selected, "outgoing", access)
		if err != nil {
			errCh <- err
		}
	}()
	wg.Wait()
	close(errCh)
	var sectionErrs []error
	for err := range errCh {
		sectionErrs = append(sectionErrs, err)
	}
	if len(sectionErrs) > 0 {
		return nil, false, nil, false, nil, false, errors.Join(sectionErrs...)
	}
	return workloads, workloadsTruncated, incomingPaths, incomingTruncated, outgoingPaths, outgoingTruncated, nil
}

func (r *resourceInvestigationRequest) normalize() error {
	r.Query = strings.TrimSpace(r.Query)
	r.ResourceID = strings.TrimSpace(r.ResourceID)
	r.ResourceType = normalizeResourceInvestigationType(r.ResourceType)
	r.Environment = strings.TrimSpace(r.Environment)
	if r.Limit <= 0 {
		r.Limit = resourceInvestigationDefaultLimit
	}
	if r.Limit > resourceInvestigationMaxLimit {
		r.Limit = resourceInvestigationMaxLimit
	}
	if r.MaxDepth <= 0 {
		r.MaxDepth = resourceInvestigationDefaultDepth
	}
	if r.MaxDepth > resourceInvestigationMaxDepth {
		r.MaxDepth = resourceInvestigationMaxDepth
	}
	if r.selector() == "" {
		return fmt.Errorf("query or resource_id is required")
	}
	return nil
}

func (r resourceInvestigationRequest) selector() string {
	if r.ResourceID != "" {
		return r.ResourceID
	}
	return r.Query
}

func normalizeResourceInvestigationType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queue", "database", "db", "cloud_resource", "cloud", "k8s", "k8s_resource", "kubernetes", "terraform", "terraform_resource", "module", "terraform_module":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

// resourceInvestigationEmptyGrantResolution builds the "no match" resolution
// envelope for a scoped caller with an empty grant, mirroring the shape
// resolveResourceInvestigationTarget returns for a zero-candidate resolution
// so an empty-grant caller cannot distinguish "no such resource" from "no
// granted repositories".
func resourceInvestigationEmptyGrantResolution(req resourceInvestigationRequest) map[string]any {
	return map[string]any{
		"input":         req.selector(),
		"resource_type": req.ResourceType,
		"status":        "no_match",
		"candidates":    []map[string]any{},
		"truncated":     false,
	}
}
