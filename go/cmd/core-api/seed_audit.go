// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Bootstrap audit reason codes (bounded, lowercase snake_case, <=64 chars).
const (
	coreBootstrapAuditReasonModeGenerated  = "bootstrap_mode_generated"
	coreBootstrapAuditReasonModeSeededEnv  = "bootstrap_mode_seeded_env"
	coreBootstrapAuditReasonModeSSOOnly    = "bootstrap_mode_sso_only"
	coreBootstrapAuditReasonModeDisabled   = "bootstrap_mode_disabled"
	coreBootstrapAuditReasonModeSealed     = "bootstrap_mode_sealed_existing"
	coreBootstrapAuditReasonModeError      = "bootstrap_mode_error"
	coreBootstrapAuditReasonGenerated      = "bootstrap_credential_generated"
	coreBootstrapAuditReasonGenerateFailed = "bootstrap_credential_generation_failed"
)

// auditCoreBootstrapModeChoice records the effective boot-decision outcome
// exactly once per seed call. Best-effort: never fails the primary
// operation on an audit-append error.
func auditCoreBootstrapModeChoice(
	ctx context.Context,
	appender query.GovernanceAuditAppender,
	outcome string,
) {
	if appender == nil {
		return
	}
	reason, decision := coreBootstrapModeAuditReason(outcome)
	_ = appender.Append(ctx, []governanceaudit.Event{{
		Type:        governanceaudit.EventTypeBootstrap,
		ActorClass:  governanceaudit.ActorClassSystem,
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reason,
		OccurredAt:  time.Now().UTC(),
		TenantID:    pgstatus.BootstrapAdminTenantID,
		WorkspaceID: pgstatus.BootstrapAdminWorkspaceID,
	}})
}

// auditCoreBootstrapCredentialGenerated records the credential-generation
// attempt inside ESHU_AUTH_BOOTSTRAP_MODE=generated. keyID is safe to
// record; never the plaintext credential.
func auditCoreBootstrapCredentialGenerated(
	ctx context.Context,
	appender query.GovernanceAuditAppender,
	keyID string,
	genErr error,
) {
	if appender == nil {
		return
	}
	reason := coreBootstrapAuditReasonGenerated
	decision := governanceaudit.DecisionAllowed
	correlationID := ""
	if genErr != nil {
		reason = coreBootstrapAuditReasonGenerateFailed
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
		OccurredAt:    time.Now().UTC(),
		TenantID:      pgstatus.BootstrapAdminTenantID,
		WorkspaceID:   pgstatus.BootstrapAdminWorkspaceID,
	}})
}

func coreBootstrapModeAuditReason(outcome string) (reason string, decision governanceaudit.Decision) {
	switch outcome {
	case "generated":
		return coreBootstrapAuditReasonGenerated, governanceaudit.DecisionAllowed
	case "seeded_env":
		return coreBootstrapAuditReasonModeSeededEnv, governanceaudit.DecisionAllowed
	case "skipped_" + coreAuthBootstrapModeSSOOnly:
		return coreBootstrapAuditReasonModeSSOOnly, governanceaudit.DecisionAllowed
	case "skipped_" + coreAuthBootstrapModeDisabled:
		return coreBootstrapAuditReasonModeDisabled, governanceaudit.DecisionAllowed
	case "sealed_existing":
		return coreBootstrapAuditReasonModeSealed, governanceaudit.DecisionAllowed
	default:
		return coreBootstrapAuditReasonModeError, governanceaudit.DecisionUnavailable
	}
}
