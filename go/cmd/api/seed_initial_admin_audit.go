// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Bootstrap identity seeding durable audit events (issue #4963 acceptance
// criterion: "Bootstrap mode choice, generation, retrieval, and reset are
// durable audit events (values excluded)"). Every event below carries only
// bounded metadata (event kind via ReasonCode, tenant/workspace, timestamp,
// and key_id via CorrelationID) — never the generated plaintext password,
// recovery code, or sealed ciphertext. Reason codes are the governanceaudit
// package's own bounded, lowercase snake_case, <=64-char format
// (governanceaudit.NormalizeEvent's validReasonCode); this file's constants
// must stay within that contract.
const (
	bootstrapAuditReasonModeGenerated  = "bootstrap_mode_generated"
	bootstrapAuditReasonModeSeededEnv  = "bootstrap_mode_seeded_env"
	bootstrapAuditReasonModeSSOOnly    = "bootstrap_mode_sso_only"
	bootstrapAuditReasonModeDisabled   = "bootstrap_mode_disabled"
	bootstrapAuditReasonModeSealed     = "bootstrap_mode_sealed_existing"
	bootstrapAuditReasonModeError      = "bootstrap_mode_error"
	bootstrapAuditReasonGenerated      = "bootstrap_credential_generated"
	bootstrapAuditReasonGenerateFailed = "bootstrap_credential_generation_failed"
)

// auditBootstrapModeChoice records the effective boot-decision outcome
// exactly once per seedInitialAdmin call, regardless of which branch was
// taken. appender may be nil (audit store unavailable), matching every other
// call site in this codebase (see query.LocalIdentityHandler.auditLocalIdentity).
func auditBootstrapModeChoice(ctx context.Context, appender query.GovernanceAuditAppender, outcome string) {
	if appender == nil {
		return
	}
	reason, decision := bootstrapModeAuditReason(outcome)
	// Best-effort, fire-and-forget: matches every other governance-audit call
	// site in this codebase (query.LocalIdentityHandler.auditLocalIdentity),
	// which never fails the primary operation on an audit-append error.
	_ = appender.Append(ctx, []governanceaudit.Event{{
		Type:        governanceaudit.EventTypeBootstrap,
		ActorClass:  governanceaudit.ActorClassSystem,
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reason,
		TenantID:    pgstorage.BootstrapAdminTenantID,
		WorkspaceID: pgstorage.BootstrapAdminWorkspaceID,
	}})
}

// auditBootstrapCredentialGenerated records the credential-generation
// attempt inside ESHU_AUTH_BOOTSTRAP_MODE=generated, distinct from the
// broader mode-choice event, so an auditor can filter specifically for
// "was a credential generated" without decoding outcome reason codes.
// keyID is safe to record (epic #4962: "key_id OK on spans/logs"); it is
// never the plaintext credential.
func auditBootstrapCredentialGenerated(ctx context.Context, appender query.GovernanceAuditAppender, keyID string, genErr error) {
	if appender == nil {
		return
	}
	reason := bootstrapAuditReasonGenerated
	decision := governanceaudit.DecisionAllowed
	correlationID := ""
	if genErr != nil {
		reason = bootstrapAuditReasonGenerateFailed
		decision = governanceaudit.DecisionDenied
	} else if keyID != "" {
		correlationID = "key:" + strings.ToLower(strings.TrimSpace(keyID))
	}
	_ = appender.Append(ctx, []governanceaudit.Event{{
		Type:          governanceaudit.EventTypeBootstrap,
		ActorClass:    governanceaudit.ActorClassSystem,
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      decision,
		ReasonCode:    reason,
		CorrelationID: correlationID,
		TenantID:      pgstorage.BootstrapAdminTenantID,
		WorkspaceID:   pgstorage.BootstrapAdminWorkspaceID,
	}})
}

func bootstrapModeAuditReason(outcome string) (reason string, decision governanceaudit.Decision) {
	switch outcome {
	case "generated":
		return bootstrapAuditReasonModeGenerated, governanceaudit.DecisionAllowed
	case "seeded_env":
		return bootstrapAuditReasonModeSeededEnv, governanceaudit.DecisionAllowed
	case "skipped_" + authBootstrapModeSSOOnly:
		return bootstrapAuditReasonModeSSOOnly, governanceaudit.DecisionAllowed
	case "skipped_" + authBootstrapModeDisabled:
		return bootstrapAuditReasonModeDisabled, governanceaudit.DecisionAllowed
	case "sealed_existing":
		return bootstrapAuditReasonModeSealed, governanceaudit.DecisionAllowed
	default:
		return bootstrapAuditReasonModeError, governanceaudit.DecisionUnavailable
	}
}
