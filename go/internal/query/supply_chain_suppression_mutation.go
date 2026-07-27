// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerabilitysuppression/v1"
	"go.opentelemetry.io/otel/attribute"
)

const (
	vulnerabilitySuppressionRequestMaxBytes    = 64 << 10
	vulnerabilitySuppressionMutationCapability = "supply_chain.vulnerability_suppression_mutation"
)

// VulnerabilitySuppressionMutationStore persists operator-authored
// vulnerability suppression facts.
type VulnerabilitySuppressionMutationStore interface {
	UpsertVulnerabilitySuppression(
		context.Context,
		vulnerabilitysuppressionv1.Suppression,
	) (VulnerabilitySuppressionMutationResult, error)
}

// VulnerabilitySuppressionMutationResult identifies the durable generation
// containing an operator suppression.
type VulnerabilitySuppressionMutationResult struct {
	SuppressionID string
	GenerationID  string
	Changed       bool
}

// VulnerabilitySuppressionMutationRequest is the operator-facing POST body.
// Source and author are deliberately absent because the server derives them
// from the authenticated mutation path.
type VulnerabilitySuppressionMutationRequest struct {
	SuppressionID string                           `json:"suppression_id"`
	Justification string                           `json:"justification"`
	AuthoredAt    string                           `json:"authored_at"`
	ExpiresAt     string                           `json:"expires_at,omitempty"`
	Reason        string                           `json:"reason"`
	EvidenceRef   string                           `json:"evidence_ref,omitempty"`
	Scope         vulnerabilitysuppressionv1.Scope `json:"scope"`
}

// VulnerabilitySuppressionMutationResponse is returned after a suppression
// mutation is durably committed.
type VulnerabilitySuppressionMutationResponse struct {
	SuppressionID string `json:"suppression_id"`
	GenerationID  string `json:"generation_id"`
	Status        string `json:"status"`
}

func (h *SupplyChainHandler) createVulnerabilitySuppression(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryVulnerabilitySuppressionMutation,
		"POST /api/v0/supply-chain/impact/suppressions",
		vulnerabilitySuppressionMutationCapability,
	)
	outcome := "rejected"
	defer func() {
		span.SetAttributes(attribute.String("eshu.mutation.outcome", outcome))
		span.End()
	}()

	auth, ok := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !ok || !auth.AllScopes {
		WriteError(w, http.StatusForbidden, "all-scopes operator authorization is required")
		return
	}
	if h.SuppressionMutations == nil {
		WriteError(w, http.StatusServiceUnavailable, "vulnerability suppression mutation store is unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, vulnerabilitySuppressionRequestMaxBytes)
	var request VulnerabilitySuppressionMutationRequest
	if err := ReadJSON(r, &request); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	value, err := buildOperatorVulnerabilitySuppression(request, auth)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.SuppressionMutations.UpsertVulnerabilitySuppression(r.Context(), value)
	if err != nil {
		outcome = "store_error"
		span.RecordError(err)
		WriteError(w, http.StatusInternalServerError, "persist vulnerability suppression")
		return
	}

	status := http.StatusCreated
	state := "created"
	if !result.Changed {
		status = http.StatusOK
		state = "unchanged"
	}
	outcome = state
	WriteJSON(w, status, VulnerabilitySuppressionMutationResponse{
		SuppressionID: result.SuppressionID,
		GenerationID:  result.GenerationID,
		Status:        state,
	})
}

func buildOperatorVulnerabilitySuppression(
	request VulnerabilitySuppressionMutationRequest,
	auth AuthContext,
) (vulnerabilitysuppressionv1.Suppression, error) {
	request.SuppressionID = strings.TrimSpace(request.SuppressionID)
	request.Justification = strings.TrimSpace(request.Justification)
	request.AuthoredAt = strings.TrimSpace(request.AuthoredAt)
	request.ExpiresAt = strings.TrimSpace(request.ExpiresAt)
	request.Reason = strings.TrimSpace(request.Reason)
	request.EvidenceRef = strings.TrimSpace(request.EvidenceRef)
	request.Scope = normalizeVulnerabilitySuppressionScope(request.Scope)

	if request.SuppressionID == "" {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("suppression_id is required")
	}
	if len(request.SuppressionID) > 256 {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("suppression_id must be 256 characters or fewer")
	}
	if !operatorSuppressionJustificationAllowed(request.Justification) {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf(
			"justification must be one of not_affected, accepted_risk, false_positive, ignored",
		)
	}
	authoredAt, err := time.Parse(time.RFC3339, request.AuthoredAt)
	if err != nil {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("authored_at must be an RFC3339 timestamp")
	}
	authoredAtText := authoredAt.UTC().Format(time.RFC3339)
	if request.Reason == "" {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("reason is required")
	}
	if vulnerabilitySuppressionScopeEmpty(request.Scope) {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("scope must include at least one vulnerability or deployment anchor")
	}

	var expiresAt *string
	if request.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, request.ExpiresAt)
		if err != nil {
			return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("expires_at must be an RFC3339 timestamp")
		}
		normalized := parsed.UTC().Format(time.RFC3339)
		expiresAt = &normalized
	} else if request.Justification == facts.VulnerabilitySuppressionJustificationIgnored {
		return vulnerabilitysuppressionv1.Suppression{}, fmt.Errorf("expires_at is required when justification is ignored")
	}

	reason := request.Reason
	value := vulnerabilitysuppressionv1.Suppression{
		SuppressionID: request.SuppressionID,
		Source:        facts.VulnerabilitySuppressionSourcePolicy,
		Justification: request.Justification,
		Author:        vulnerabilitySuppressionAuthor(auth),
		AuthoredAt:    authoredAtText,
		ExpiresAt:     expiresAt,
		Reason:        &reason,
		Scope:         request.Scope,
	}
	if request.EvidenceRef != "" {
		evidenceRef := request.EvidenceRef
		value.EvidenceRef = &evidenceRef
	}
	return value, nil
}

func operatorSuppressionJustificationAllowed(value string) bool {
	switch value {
	case facts.VulnerabilitySuppressionJustificationNotAffected,
		facts.VulnerabilitySuppressionJustificationAcceptedRisk,
		facts.VulnerabilitySuppressionJustificationFalsePositive,
		facts.VulnerabilitySuppressionJustificationIgnored:
		return true
	default:
		return false
	}
}

func vulnerabilitySuppressionAuthor(auth AuthContext) string {
	subjectClass := strings.TrimSpace(auth.SubjectClass)
	if subjectClass == "" {
		subjectClass = "authenticated_operator"
	}
	subjectIDHash := strings.TrimSpace(auth.SubjectIDHash)
	if subjectIDHash == "" {
		return subjectClass
	}
	return subjectClass + ":" + subjectIDHash
}

func normalizeVulnerabilitySuppressionScope(
	value vulnerabilitysuppressionv1.Scope,
) vulnerabilitysuppressionv1.Scope {
	value.CVEID = trimmedStringPointer(value.CVEID)
	value.AdvisoryID = trimmedStringPointer(value.AdvisoryID)
	value.PackageID = trimmedStringPointer(value.PackageID)
	value.PURL = trimmedStringPointer(value.PURL)
	value.RepositoryID = trimmedStringPointer(value.RepositoryID)
	value.SubjectDigest = trimmedStringPointer(value.SubjectDigest)
	cleanedPath := make([]string, 0, len(value.EvidencePath))
	for _, step := range value.EvidencePath {
		if step = strings.TrimSpace(step); step != "" {
			cleanedPath = append(cleanedPath, step)
		}
	}
	value.EvidencePath = cleanedPath
	return value
}

func trimmedStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func vulnerabilitySuppressionScopeEmpty(value vulnerabilitysuppressionv1.Scope) bool {
	return value.CVEID == nil &&
		value.AdvisoryID == nil &&
		value.PackageID == nil &&
		value.PURL == nil &&
		value.RepositoryID == nil &&
		value.SubjectDigest == nil &&
		len(value.EvidencePath) == 0
}
