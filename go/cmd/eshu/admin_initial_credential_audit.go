// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query"
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
	bootstrapCredentialAuditReasonRetrieved      = "bootstrap_credential_retrieved"       // #nosec G101 -- audit reason-code label, not a credential value
	bootstrapCredentialAuditReasonRetrieveFailed = "bootstrap_credential_retrieve_failed" // #nosec G101 -- audit reason-code label, not a credential value
	bootstrapCredentialAuditReasonReset          = "bootstrap_credential_reset"           // #nosec G101 -- audit reason-code label, not a credential value
	bootstrapCredentialAuditReasonResetFailed    = "bootstrap_credential_reset_failed"    // #nosec G101 -- audit reason-code label, not a credential value
)

// newAdminCredentialAuditAppender builds the durable governance-audit
// appender from the CLI's own Postgres handle. appender is nil only when db
// is nil (defensive; every real call site always has an open connection),
// matching every other governance-audit call site in this codebase (see
// go/cmd/api/seed_initial_admin_audit.go's auditBootstrapModeChoice), which
// never fails the primary operation because audit wiring is unavailable.
// Returns query.GovernanceAuditAppender (this binary already depends on
// internal/query elsewhere — see local_host_config.go and friends — so
// reusing its interface here, rather than declaring a structurally-identical
// local one, is not a new dependency edge).
func newAdminCredentialAuditAppender(db pgstorage.ExecQueryer) query.GovernanceAuditAppender {
	if db == nil {
		return nil
	}
	store := pgstorage.NewGovernanceAuditStore(db)
	return store
}

// auditBootstrapCredentialRetrieved records a retrieval attempt of the
// one-time bootstrap admin credential via `eshu admin initial-credential`,
// success or failure — mirroring go/cmd/api/seed_initial_admin_audit.go's
// auditBootstrapCredentialGenerated, which audits both outcomes of its own
// operation, not only success. Retrieval is repeatable until the
// credential's first login consumes it, so that an attempt happened, when,
// and whether it succeeded must all be durably recorded (epic #4962
// acceptance criterion) — a failed attempt (already consumed, wrong DEK) is
// as security-relevant as a successful one. This CLI has no login/session of
// its own (it authenticates directly with ESHU_POSTGRES_DSN + the DEK, the
// same trust boundary as the API process itself), so there is no
// per-operator identity to attribute the event to; ActorClassSystem below
// reflects that honestly rather than fabricating an ActorIDHash
// NormalizeEvent would otherwise require for ActorClassOperator. keyID is
// "" on a failure that never resolved an envelope (not found, select error);
// a decrypt failure still carries the envelope's own keyID, since that much
// was read from the row before Open failed.
func auditBootstrapCredentialRetrieved(ctx context.Context, appender query.GovernanceAuditAppender, keyID string, retrieveErr error) {
	reason := bootstrapCredentialAuditReasonRetrieved
	decision := governanceaudit.DecisionAllowed
	if retrieveErr != nil {
		reason = bootstrapCredentialAuditReasonRetrieveFailed
		decision = governanceaudit.DecisionDenied
	}
	auditBootstrapCredentialCLIEvent(ctx, appender, reason, decision, keyID)
}

// auditBootstrapCredentialReset records a reset/regeneration attempt of the
// bootstrap admin credential via `eshu admin reset-initial-credential`,
// success or failure (see auditBootstrapCredentialRetrieved's doc comment
// for why both outcomes are audited). keyID is always the newly-sealed
// replacement envelope's key id: Seal (and EnvelopeKeyID) already succeeded
// before ResetBootstrapCredential's persistence call is attempted, so a
// persistence failure still has a real key id to correlate against.
func auditBootstrapCredentialReset(ctx context.Context, appender query.GovernanceAuditAppender, keyID string, resetErr error) {
	reason := bootstrapCredentialAuditReasonReset
	decision := governanceaudit.DecisionAllowed
	if resetErr != nil {
		reason = bootstrapCredentialAuditReasonResetFailed
		decision = governanceaudit.DecisionDenied
	}
	auditBootstrapCredentialCLIEvent(ctx, appender, reason, decision, keyID)
}

// auditBootstrapCredentialCLIEvent appends one bounded, values-excluded
// audit event. Best-effort, fire-and-forget: matches every other
// governance-audit call site in this codebase, which never fails the
// primary CLI operation on an audit-append error.
func auditBootstrapCredentialCLIEvent(ctx context.Context, appender query.GovernanceAuditAppender, reason string, decision governanceaudit.Decision, keyID string) {
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
		Decision:      decision,
		ReasonCode:    reason,
		CorrelationID: correlationID,
		OccurredAt:    time.Now().UTC(),
		TenantID:      pgstorage.BootstrapAdminTenantID,
		WorkspaceID:   pgstorage.BootstrapAdminWorkspaceID,
	}})
}
