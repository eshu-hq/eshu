// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config_test

// D0 termination-transport shim: core-side versioned port server (test-only). See termination_transport_shim_test.go for the theory.

import (
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/admin/provider/config"
)

// ---------------------------------------------------------------------------
// Core side: versioned port server (test-only, never mounted).
// ---------------------------------------------------------------------------

// shim port request headers carry the destination-asserted caller bounds.
// The port server re-validates every field before touching the store, so
// authz and the read are a single atomic core-side step.
const (
	shimHeaderAllScopes = "X-Shim-All-Scopes"
	shimHeaderTenant    = "X-Shim-Tenant"
	shimHeaderEnforced  = "X-Shim-Catalog-Enforced"
	shimHeaderFeatures  = "X-Shim-Features"
)

// shimPortServer is the core-side end of the D0 transport: a versioned
// (v0) JSON exposure of the frozen ReadStore port. It enforces the same
// three gates the in-tree handler enforces — all-scopes, tenant, and the
// permission feature — reconstructed from the wire headers, before any
// store call. A denied call returns 403 and records zero store calls.
type shimPortServer struct {
	store config.ReadStore
}

func (s *shimPortServer) gate(r *http.Request) (tenantID string, ok bool) {
	if r.Header.Get(shimHeaderAllScopes) != "true" {
		return "", false
	}
	tenantID = strings.TrimSpace(r.Header.Get(shimHeaderTenant))
	if tenantID == "" {
		return "", false
	}
	if r.Header.Get(shimHeaderEnforced) == "true" {
		allowed := false
		for _, f := range strings.Split(r.Header.Get(shimHeaderFeatures), ",") {
			f = strings.TrimSpace(f)
			if f == shimPermissionFeature || f == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", false
		}
	}
	return tenantID, true
}

func (s *shimPortServer) deny(w http.ResponseWriter) {
	shimWriteError(w, http.StatusForbidden, shimScopeRequiredMessage)
}

func (s *shimPortServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/port/provider-configs", s.serveList)
	mux.HandleFunc("GET /v0/port/provider-configs/detail", s.serveGet)
	mux.HandleFunc("GET /v0/port/provider-configs/revisions", s.serveRevisions)
	return mux
}

func (s *shimPortServer) serveList(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.gate(r)
	if !ok || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != tenantID {
		s.deny(w)
		return
	}
	items, err := s.store.ListProviderConfigDetails(r.Context(), tenantID)
	if err != nil {
		shimWriteError(w, http.StatusInternalServerError, shimListFailedMessage)
		return
	}
	out := make([]shimDetailDTO, 0, len(items))
	for _, item := range items {
		out = append(out, shimDetailFromConfig(item))
	}
	shimWriteJSON(w, http.StatusOK, map[string]any{"details": out})
}

func (s *shimPortServer) serveGet(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.gate(r)
	if !ok || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != tenantID {
		s.deny(w)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("provider_config_id"))
	detail, found, err := s.store.GetProviderConfigDetail(r.Context(), id, tenantID)
	if err != nil {
		shimWriteError(w, http.StatusInternalServerError, shimReadFailedMessage)
		return
	}
	if !found {
		shimWriteError(w, http.StatusNotFound, shimNotFoundMessage)
		return
	}
	shimWriteJSON(w, http.StatusOK, shimDetailFromConfig(detail))
}

func (s *shimPortServer) serveRevisions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.gate(r)
	if !ok || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != tenantID {
		s.deny(w)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("provider_config_id"))
	items, err := s.store.ListProviderConfigRevisions(r.Context(), id, tenantID)
	if err != nil {
		shimWriteError(w, http.StatusInternalServerError, shimRevisionsFailedMessage)
		return
	}
	out := make([]shimRevisionDTO, 0, len(items))
	for _, item := range items {
		out = append(out, shimRevisionFromConfig(item))
	}
	shimWriteJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

func shimDetailFromConfig(d config.Detail) shimDetailDTO {
	return shimDetailDTO{
		ProviderConfigID:      d.ProviderConfigID,
		ProviderKind:          d.ProviderKind,
		Status:                d.Status,
		ActiveRevisionID:      d.ActiveRevisionID,
		Configuration:         d.Configuration,
		HasSecret:             d.HasSecret,
		SecretFingerprint:     d.SecretFingerprint,
		SecretKeyID:           d.SecretKeyID,
		ShadowedByEnvironment: d.ShadowedByEnvironment,
		ManagedBy:             d.ManagedBy,
		CreatedAt:             d.CreatedAt,
		UpdatedAt:             d.UpdatedAt,
	}
}

func shimRevisionFromConfig(item config.RevisionItem) shimRevisionDTO {
	out := shimRevisionDTO{
		RevisionID: item.RevisionID,
		Status:     item.Status,
		HasSecret:  item.HasSecret,
		CreatedAt:  item.CreatedAt,
	}
	// Normalize to UTC like the reference: audit.AddOptionalTime renders
	// activated_at/superseded_at UTC-normalized, so a non-UTC store
	// instant must render identically here or the bodies differ.
	if !item.ActivatedAt.IsZero() {
		at := item.ActivatedAt.UTC()
		out.ActivatedAt = &at
	}
	if !item.SupersededAt.IsZero() {
		st := item.SupersededAt.UTC()
		out.SupersededAt = &st
	}
	return out
}
