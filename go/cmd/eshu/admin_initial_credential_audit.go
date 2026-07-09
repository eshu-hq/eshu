// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Bootstrap admin credential CLI durable audit events (issue #4963
// acceptance criterion: "Bootstrap mode choice, generation, retrieval, and
// reset are durable audit events (values excluded)"). Retrieval and reset
// only ever happen through this CLI (`eshu admin initial-credential` /
// `reset-initial-credential`), never through the API process, so their
// audit events live here rather than in
// go/cmd/api/seed_initial_admin_audit.go (which covers the two events the
// API process itself observes: mode choice and credential generation at
// startup) — cmd/api and cmd/eshu are separate main packages and cannot
// share unexported code. Every event below carries only bounded metadata
// (event kind via ReasonCode, tenant/workspace, timestamp, and key_id via
// CorrelationID) — never the retrieved or regenerated plaintext password,
// recovery code, or sealed ciphertext. Reason codes follow the
// governanceaudit package's own bounded, lowercase snake_case, <=64-char
// format (governanceaudit.NormalizeEvent's validReasonCode).
const (
	bootstrapCredentialAuditReasonRetrieved = "bootstrap_credential_retrieved"
	bootstrapCredentialAuditReasonReset     = "bootstrap_credential_reset"
)

// governanceAuditAppender is the minimal Append contract this CLI needs. It
// is declared locally, rather than importing internal/query (an API-focused
// package this binary otherwise has no dependency on), so
// pgstorage.GovernanceAuditStore satisfies it structurally.
type governanceAuditAppender interface {
	Append(context.Context, []governanceaudit.Event) error
}

// newAdminCredentialAuditAppender builds the durable governance-audit
// appender from the CLI's own Postgres handle. appender is nil only when db
// is nil (defensive; every real call site always has an open connection),
// matching every other governance-audit call site in this codebase (see
// go/cmd/api/seed_initial_admin_audit.go's auditBootstrapModeChoice), which
// never fails the primary operation because audit wiring is unavailable.
func newAdminCredentialAuditAppender(db pgstorage.ExecQueryer) governanceAuditAppender {
	if db == nil {
		return nil
	}
	store := pgstorage.NewGovernanceAuditStore(db)
	return store
}

// auditBootstrapCredentialRetrieved records a retrieval of the one-time
// bootstrap admin credential via `eshu admin initial-credential`. Retrieval
// is repeatable until the credential's first login consumes it, so that it
// happened — and when — must be durably recorded (epic #4962 acceptance
// criterion). This CLI has no login/session of its own (it authenticates
// directly with ESHU_POSTGRES_DSN + the DEK, the same trust boundary as the
// API process itself), so there is no per-operator identity to attribute the
// event to; ActorClassSystem below reflects that honestly rather than
// fabricating an ActorIDHash NormalizeEvent would otherwise require for
// ActorClassOperator.
func auditBootstrapCredentialRetrieved(ctx context.Context, appender governanceAuditAppender, keyID string) {
	auditBootstrapCredentialCLIEvent(ctx, appender, bootstrapCredentialAuditReasonRetrieved, keyID)
}

// auditBootstrapCredentialReset records a reset/regeneration of the
// bootstrap admin credential via `eshu admin reset-initial-credential`.
func auditBootstrapCredentialReset(ctx context.Context, appender governanceAuditAppender, keyID string) {
	auditBootstrapCredentialCLIEvent(ctx, appender, bootstrapCredentialAuditReasonReset, keyID)
}

// auditBootstrapCredentialCLIEvent appends one bounded, values-excluded
// audit event. Best-effort, fire-and-forget: matches every other
// governance-audit call site in this codebase, which never fails the
// primary CLI operation on an audit-append error — the retrieved/reset
// credential has already been printed or persisted by the time this runs,
// so failing here would only hide a successful operation behind an
// unrelated audit-store error.
func auditBootstrapCredentialCLIEvent(ctx context.Context, appender governanceAuditAppender, reason, keyID string) {
	if appender == nil {
		return
	}
	correlationID := ""
	if keyID != "" {
		correlationID = "key:" + strings.ToLower(strings.TrimSpace(keyID))
	}
	_ = appender.Append(ctx, []governanceaudit.Event{{
		Type:          governanceaudit.EventTypeBootstrap,
		ActorClass:    governanceaudit.ActorClassSystem,
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      governanceaudit.DecisionAllowed,
		ReasonCode:    reason,
		CorrelationID: correlationID,
		OccurredAt:    time.Now().UTC(),
		TenantID:      pgstorage.BootstrapAdminTenantID,
		WorkspaceID:   pgstorage.BootstrapAdminWorkspaceID,
	}})
}
