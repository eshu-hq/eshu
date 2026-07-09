// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/crypto/bcrypt"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const setupHandlerSecretBytes = 32

func (h *SetupHandler) ready(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "setup store is unavailable")
		return false
	}
	return true
}

func (h *SetupHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *SetupHandler) idleTimeout() time.Duration {
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return DefaultBrowserSessionIdleTimeout
}

func (h *SetupHandler) absoluteTimeout() time.Duration {
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return DefaultBrowserSessionAbsoluteTimeout
}

// cookieSecureMode normalizes h.CookieSecure, defaulting to CookieSecureAuto.
func (h *SetupHandler) cookieSecureMode() CookieSecureMode {
	return ParseCookieSecureMode(string(h.CookieSecure))
}

func (h *SetupHandler) hashPassword(password string) (string, error) {
	cost := h.PasswordCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// newID returns a fresh random identifier or the underlying crypto/rand (or
// injected NewSecret) error. Callers MUST check the error and fail the
// request rather than proceed with an empty id: an empty CredentialID or
// MFAFactorID would still satisfy Go's zero-value defaults and silently
// collide across requests instead of failing loudly (#4990 P2).
func (h *SetupHandler) newID() (string, error) {
	return h.newSecret()
}

func (h *SetupHandler) newSecret() (string, error) {
	if h.NewSecret != nil {
		secret, err := h.NewSecret()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(secret), nil
	}
	var bytes [setupHandlerSecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

// auditSetup records one governance audit event for a setup-wizard step.
// There is no authenticated actor yet (every setup route is pre-auth, see
// SetupHandler.Mount's doc comment), so the actor class is always anonymous
// and the actor hash is a bounded reason marker, never a raw request value.
func (h *SetupHandler) auditSetup(r *http.Request, decision governanceaudit.Decision, reasonCode string) {
	if h.Audit == nil {
		return
	}
	event := governanceaudit.Event{
		Type:        governanceaudit.EventTypeBootstrap,
		ActorClass:  governanceaudit.ActorClassAnonymous,
		ActorIDHash: localIdentityHash("setup_wizard"),
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reasonCode,
		OccurredAt:  h.now(),
	}
	_ = h.Audit.Append(r.Context(), []governanceaudit.Event{event})
}

// recordOutcome records one eshu_dp_auth_setup_wizard_total observation for
// the bounded step_result value (e.g. "claim_allowed", "admin_denied").
func (h *SetupHandler) recordOutcome(r *http.Request, stepResult string) {
	if h.Instruments == nil || h.Instruments.AuthSetupWizardTotal == nil {
		return
	}
	h.Instruments.AuthSetupWizardTotal.Add(r.Context(), 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionResult, stepResult),
	))
}
