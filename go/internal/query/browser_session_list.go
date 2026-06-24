// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// BrowserSessionListItem is the metadata-only view of one browser session that
// is safe to return to the owning subject. It never includes session_hash,
// csrf_token_hash, or external auth secrets. Current is set by the store via
// SQL boolean comparison — no raw hash value is ever selected into this type.
type BrowserSessionListItem struct {
	IssuedAt          time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	TenantID          string
	WorkspaceID       string
	// Current is true when this row matches the caller's active session.
	// Computed by the store: (session_hash = callerHash) AS current.
	Current   bool
	RevokedAt time.Time
}

// BrowserSessionLister is the read surface for listing a subject's own browser
// sessions. sessionHash is the SHA-256 hash of the caller's cookie value, used
// only to flag the matching row as current — it is never included in the result.
type BrowserSessionLister interface {
	// ListSessionsBySubject returns metadata-only rows for the subject. The
	// sessionHash parameter is used server-side to compute the Current boolean;
	// it is never selected or returned in the results. Pass "" when no session
	// cookie is available; no row will be marked current.
	ListSessionsBySubject(ctx context.Context, subjectIDHash string, asOf time.Time, sessionHash string) ([]BrowserSessionListItem, error)
}

// BrowserSessionListHandler serves GET /api/v0/auth/sessions, returning
// metadata-only rows for the caller's own browser sessions.
type BrowserSessionListHandler struct {
	Store BrowserSessionLister
	Now   func() time.Time
}

// Mount registers the list-sessions route.
func (h *BrowserSessionListHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/sessions", h.handleListSessions)
}

func (h *BrowserSessionListHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func (h *BrowserSessionListHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "session store unavailable")
		return
	}
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	auth = normalizeAuthContext(auth)
	if auth.Mode != AuthModeBrowserSession || auth.SubjectIDHash == "" {
		unauthorizedResponse(w, r)
		return
	}

	// Hash the caller's cookie so the store can mark the matching row as
	// current. The raw cookie value is never forwarded to the store.
	sessionHash, _ := browserSessionHashFromCookie(r)

	now := h.now()
	items, err := h.Store.ListSessionsBySubject(r.Context(), auth.SubjectIDHash, now, sessionHash)
	if err != nil {
		slog.ErrorContext(r.Context(), "list browser sessions failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list browser sessions")
		return
	}

	type sessionJSON struct {
		IssuedAt          time.Time  `json:"issued_at"`
		LastSeenAt        time.Time  `json:"last_seen_at"`
		IdleExpiresAt     time.Time  `json:"idle_expires_at"`
		AbsoluteExpiresAt time.Time  `json:"absolute_expires_at"`
		TenantID          string     `json:"tenant_id,omitempty"`
		WorkspaceID       string     `json:"workspace_id,omitempty"`
		Current           bool       `json:"current"`
		RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	}

	out := make([]sessionJSON, 0, len(items))
	for _, item := range items {
		s := sessionJSON{
			IssuedAt:          item.IssuedAt,
			LastSeenAt:        item.LastSeenAt,
			IdleExpiresAt:     item.IdleExpiresAt,
			AbsoluteExpiresAt: item.AbsoluteExpiresAt,
			TenantID:          item.TenantID,
			WorkspaceID:       item.WorkspaceID,
			Current:           item.Current,
		}
		if !item.RevokedAt.IsZero() {
			t := item.RevokedAt
			s.RevokedAt = &t
		}
		out = append(out, s)
	}

	WriteJSON(w, http.StatusOK, map[string]any{"sessions": out})
}
