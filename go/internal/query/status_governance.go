// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/status"
)

const hostedGovernanceStatusCapability = "hosted_governance.status"

// GovernanceStatusConfig carries redacted operator-visible governance readback
// settings.
type GovernanceStatusConfig struct {
	Mode               string
	State              string
	SourceKind         string
	PolicyRevisionHash string
	AuthMode           string
	TokenConfigured    bool
	TenantMode         string
	WorkspaceMode      string
	EgressMode         string
	RetentionMode      string
	RedactionState     string
	AuditState         string
	ExtensionMode      string
	DeniedDecisions    int
	PolicySectionCount int
	StaleSectionCount  int
	Reasons            []string
	AuditSummary       governanceaudit.Summary
}

// GovernanceAuditSummaryReader reads aggregate-only governance audit counts.
type GovernanceAuditSummaryReader interface {
	Summary(context.Context) (governanceaudit.Summary, error)
}

func (h *StatusHandler) getGovernanceStatus(w http.ResponseWriter, r *http.Request) {
	report := status.BuildReport(status.RawSnapshot{}, status.DefaultOptions())
	if h != nil && h.StatusReader != nil {
		loaded, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
			return
		}
		report = loaded
	}
	config := h.governanceConfig()
	if h != nil && h.GovernanceAudit != nil {
		summary, err := h.GovernanceAudit.Summary(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load governance audit summary: %v", err))
			return
		}
		if governanceAuditSummaryHasValues(summary) {
			config.AuditSummary = summary
			if strings.TrimSpace(config.AuditState) == "" || strings.TrimSpace(config.AuditState) == "not_configured" {
				config.AuditState = "observed"
			}
		}
	}
	payload := buildGovernanceStatus(config, report.SemanticExtraction)
	WriteSuccess(w, r, http.StatusOK, payload, governanceStatusTruth(h.profile(), payload))
}

func (h *StatusHandler) governanceConfig() GovernanceStatusConfig {
	if h == nil {
		return GovernanceStatusConfig{}
	}
	return h.Governance
}

// GovernanceStatusConfigFromEnv loads safe governance readback settings from
// environment-style key/value sources. It does not read or expose policy bodies.
func GovernanceStatusConfigFromEnv(
	getenv func(string) string,
	tokenConfigured bool,
) GovernanceStatusConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return GovernanceStatusConfig{
		Mode:               strings.TrimSpace(getenv("ESHU_GOVERNANCE_MODE")),
		State:              strings.TrimSpace(getenv("ESHU_GOVERNANCE_STATE")),
		SourceKind:         strings.TrimSpace(getenv("ESHU_GOVERNANCE_SOURCE_KIND")),
		PolicyRevisionHash: strings.TrimSpace(getenv("ESHU_GOVERNANCE_POLICY_REVISION_HASH")),
		AuthMode:           strings.TrimSpace(getenv("ESHU_GOVERNANCE_AUTH_MODE")),
		TokenConfigured:    tokenConfigured,
		TenantMode:         strings.TrimSpace(getenv("ESHU_GOVERNANCE_TENANT_MODE")),
		WorkspaceMode:      strings.TrimSpace(getenv("ESHU_GOVERNANCE_WORKSPACE_MODE")),
		EgressMode:         strings.TrimSpace(getenv("ESHU_GOVERNANCE_EGRESS_MODE")),
		RetentionMode:      strings.TrimSpace(getenv("ESHU_GOVERNANCE_RETENTION_MODE")),
		RedactionState:     strings.TrimSpace(getenv("ESHU_GOVERNANCE_REDACTION_STATE")),
		AuditState:         strings.TrimSpace(getenv("ESHU_GOVERNANCE_AUDIT_STATE")),
		ExtensionMode:      strings.TrimSpace(getenv("ESHU_GOVERNANCE_EXTENSION_MODE")),
		DeniedDecisions:    intFromEnv(getenv("ESHU_GOVERNANCE_DENIED_DECISION_COUNT")),
		PolicySectionCount: intFromEnv(getenv("ESHU_GOVERNANCE_POLICY_SECTION_COUNT")),
		StaleSectionCount:  intFromEnv(getenv("ESHU_GOVERNANCE_STALE_SECTION_COUNT")),
		Reasons:            envList(getenv("ESHU_GOVERNANCE_REASONS")),
		AuditSummary: governanceaudit.Summary{
			Total:       intFromEnv(getenv("ESHU_GOVERNANCE_AUDIT_EVENT_COUNT")),
			Denied:      intFromEnv(getenv("ESHU_GOVERNANCE_AUDIT_DENIED_DECISION_COUNT")),
			Unavailable: intFromEnv(getenv("ESHU_GOVERNANCE_AUDIT_UNAVAILABLE_DECISION_COUNT")),
		},
	}
}

func buildGovernanceStatus(
	config GovernanceStatusConfig,
	semantic status.SemanticExtractionStatus,
) map[string]any {
	normalized := normalizeGovernanceConfig(config)
	reasons := governanceReasons(normalized)
	return map[string]any{
		"mode":                 normalized.Mode,
		"state":                normalized.State,
		"source_kind":          normalized.SourceKind,
		"policy_revision_hash": normalized.PolicyRevisionHash,
		"readiness":            governanceReadiness(normalized, semantic),
		"identity":             governanceIdentity(normalized),
		"tenancy":              governanceTenancy(normalized),
		"egress":               map[string]any{"mode": normalized.EgressMode},
		"semantic":             governanceSemantic(semantic),
		"extensions":           map[string]any{"mode": normalized.ExtensionMode},
		"redaction":            map[string]any{"state": normalized.RedactionState},
		"retention":            map[string]any{"mode": normalized.RetentionMode},
		"audit":                governanceAudit(normalized, semantic),
		"aggregates":           governanceAggregates(normalized, semantic),
		"reasons":              reasons,
		"supported_modes":      []string{"local_no_policy", "hosted_single_tenant", "hosted_multi_tenant"},
		"supported_states":     []string{"disabled", "partial", "enforcing", "stale", "invalid"},
	}
}

func normalizeGovernanceConfig(config GovernanceStatusConfig) GovernanceStatusConfig {
	config.Mode = allowedOrDefault(config.Mode, "local_no_policy",
		"local_no_policy", "hosted_single_tenant", "hosted_multi_tenant")
	config.State = allowedOrDefault(config.State, "disabled",
		"disabled", "partial", "enforcing", "stale", "invalid")
	config.SourceKind = allowedOrDefault(config.SourceKind, "unknown",
		"environment", "kubernetes_secret", "config_map", "postgres_revision", "unknown")
	config.PolicyRevisionHash = safeRevisionHash(config.PolicyRevisionHash)
	config.AuthMode = allowedOrDefault(config.AuthMode, "",
		"none", "shared_token", "scoped_token", "workload_identity")
	if config.AuthMode == "" {
		if config.TokenConfigured {
			config.AuthMode = "shared_token"
		} else {
			config.AuthMode = "none"
		}
	}
	if config.TenantMode == "" {
		config.TenantMode = modeDefault(config.Mode, "local_dev", "not_configured")
	}
	if config.WorkspaceMode == "" {
		config.WorkspaceMode = modeDefault(config.Mode, "single_workspace_dev", "not_configured")
	}
	config.TenantMode = allowedOrDefault(config.TenantMode, modeDefault(config.Mode, "local_dev", "not_configured"),
		"local_dev", "single_tenant", "multi_tenant", "not_configured")
	config.WorkspaceMode = allowedOrDefault(config.WorkspaceMode, modeDefault(config.Mode, "single_workspace_dev", "not_configured"),
		"single_workspace_dev", "single_workspace", "multi_workspace", "not_configured")
	config.EgressMode = allowedOrDefault(config.EgressMode, "not_configured",
		"restricted", "broad", "not_configured")
	config.RetentionMode = allowedOrDefault(config.RetentionMode, "not_configured",
		"metadata_only", "configured", "disabled", "not_configured", "stale", "invalid")
	config.RedactionState = allowedOrDefault(config.RedactionState, "not_configured",
		"configured", "disabled", "not_configured", "stale", "invalid")
	config.AuditState = allowedOrDefault(config.AuditState, "not_configured",
		"observed", "configured", "disabled", "not_configured", "stale", "unavailable")
	config.ExtensionMode = allowedOrDefault(config.ExtensionMode, "not_configured",
		"strict", "allowlist", "disabled", "not_configured", "stale", "untrusted")
	return config
}

func governanceReasons(config GovernanceStatusConfig) []any {
	reasons := append([]string{}, config.Reasons...)
	switch config.State {
	case "disabled":
		reasons = append(reasons, "policy_not_configured")
	case "invalid":
		reasons = append(reasons, "policy_invalid")
	case "stale":
		reasons = append(reasons, "policy_stale")
	case "partial":
		reasons = append(reasons, "policy_partial")
	}
	if config.AuthMode == "shared_token" {
		reasons = append(reasons, "shared_token_mode")
	}
	if isHostedMode(config.Mode) && !safeConfigured(config.TenantMode) {
		reasons = append(reasons, "tenant_scope_missing")
	}
	return stringsToAny(uniqueAllowedReasons(reasons))
}

func governanceReadiness(config GovernanceStatusConfig, semantic status.SemanticExtractionStatus) map[string]any {
	return map[string]any{
		"identity":   config.AuthMode != "none",
		"tenant":     safeConfigured(config.TenantMode) && safeConfigured(config.WorkspaceMode),
		"egress":     safeConfigured(config.EgressMode) && config.EgressMode != "broad",
		"semantic":   semantic.ProviderConfigured && semantic.State == status.SemanticExtractionAvailable,
		"extensions": safeConfigured(config.ExtensionMode),
		"redaction":  safeConfigured(config.RedactionState),
		"retention":  safeConfigured(config.RetentionMode),
		"audit":      safeConfigured(config.AuditState) || semanticExtractionAuditHasValues(semantic.Audit),
	}
}

func governanceIdentity(config GovernanceStatusConfig) map[string]any {
	return map[string]any{
		"auth_mode":               config.AuthMode,
		"configured":              config.AuthMode != "none",
		"shared_token_limitation": config.AuthMode == "shared_token",
	}
}

func governanceTenancy(config GovernanceStatusConfig) map[string]any {
	return map[string]any{
		"tenant_mode":    config.TenantMode,
		"workspace_mode": config.WorkspaceMode,
	}
}

func governanceSemantic(semantic status.SemanticExtractionStatus) map[string]any {
	view := statusJSONFromSemanticExtraction(semantic)
	return map[string]any{
		"state":                   view.State,
		"reason":                  view.Reason,
		"provider_configured":     view.ProviderConfigured,
		"provider_profile_count":  len(view.ProviderProfiles),
		"policy_denied_count":     semantic.Queue.PolicyDenied,
		"audit_actor_class_count": len(semantic.Audit.ActorClassCounts),
	}
}

func governanceAudit(config GovernanceStatusConfig, semantic status.SemanticExtractionStatus) map[string]any {
	summary := normalizeGovernanceAuditSummary(config.AuditSummary)
	return map[string]any{
		"state":                      config.AuditState,
		"event_count":                summary.Total,
		"denied_decision_count":      summary.Denied,
		"unavailable_decision_count": summary.Unavailable,
		"event_type_count":           len(summary.EventTypeCounts),
		"actor_class_count":          governanceAuditActorClassCount(summary, semantic.Audit),
		"scope_class_count":          len(summary.ScopeClassCounts),
		"reason_count":               len(summary.ReasonCounts),
		"acl_state_count":            len(semantic.Audit.ACLStateCounts),
	}
}

func governanceAggregates(config GovernanceStatusConfig, semantic status.SemanticExtractionStatus) map[string]any {
	denied := config.DeniedDecisions
	if denied == 0 {
		denied = normalizeGovernanceAuditSummary(config.AuditSummary).Denied
	}
	if denied == 0 {
		denied = semantic.Queue.PolicyDenied
	}
	return map[string]any{
		"policy_section_count":  config.PolicySectionCount,
		"denied_decision_count": denied,
		"stale_section_count":   config.StaleSectionCount,
	}
}

func governanceStatusTruth(profile QueryProfile, payload map[string]any) *TruthEnvelope {
	truth := BuildTruthEnvelope(
		profile,
		hostedGovernanceStatusCapability,
		TruthBasisRuntimeState,
		"resolved from redacted runtime governance status",
	)
	state, _ := payload["state"].(string)
	if state != "enforcing" {
		truth.Level = TruthLevelFallback
		truth.Freshness = TruthFreshness{State: FreshnessUnavailable, Detail: state}
	}
	return truth
}

func isHostedMode(mode string) bool {
	return mode == "hosted_single_tenant" || mode == "hosted_multi_tenant"
}

func safeConfigured(value string) bool {
	return value != "" && value != "not_configured" && value != "unknown"
}

func modeDefault(mode, local, hosted string) string {
	if mode == "local_no_policy" {
		return local
	}
	return hosted
}

func allowedOrDefault(value, fallback string, allowed ...string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}

func safeRevisionHash(value string) string {
	value = strings.TrimSpace(value)
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	hash := strings.TrimPrefix(value, prefix)
	if len(hash) < 8 || len(hash) > 64 {
		return ""
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return value
}

func normalizeGovernanceAuditSummary(summary governanceaudit.Summary) governanceaudit.Summary {
	summary.Total = nonNegative(summary.Total)
	summary.Allowed = nonNegative(summary.Allowed)
	summary.Denied = nonNegative(summary.Denied)
	summary.Unavailable = nonNegative(summary.Unavailable)
	summary.EventTypeCounts = normalizeGovernanceAuditCounts(summary.EventTypeCounts)
	summary.DecisionCounts = normalizeGovernanceAuditCounts(summary.DecisionCounts)
	summary.ReasonCounts = normalizeGovernanceAuditCounts(summary.ReasonCounts)
	summary.ActorClassCounts = normalizeGovernanceAuditCounts(summary.ActorClassCounts)
	summary.ScopeClassCounts = normalizeGovernanceAuditCounts(summary.ScopeClassCounts)
	return summary
}

func normalizeGovernanceAuditCounts(rows []governanceaudit.Count) []governanceaudit.Count {
	out := make([]governanceaudit.Count, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" || row.Count <= 0 {
			continue
		}
		out = append(out, governanceaudit.Count{Name: strings.TrimSpace(row.Name), Count: row.Count})
	}
	return out
}

func governanceAuditActorClassCount(
	summary governanceaudit.Summary,
	semanticAudit status.SemanticExtractionAuditSnapshot,
) int {
	names := map[string]struct{}{}
	for _, row := range summary.ActorClassCounts {
		if name := strings.TrimSpace(row.Name); name != "" && row.Count > 0 {
			names[name] = struct{}{}
		}
	}
	for _, row := range semanticAudit.ActorClassCounts {
		if name := strings.TrimSpace(row.Name); name != "" && row.Count > 0 {
			names[name] = struct{}{}
		}
	}
	return len(names)
}

func governanceAuditSummaryHasValues(summary governanceaudit.Summary) bool {
	return summary.Total > 0 || summary.Allowed > 0 || summary.Denied > 0 ||
		summary.Unavailable > 0 || len(summary.EventTypeCounts) > 0 ||
		len(summary.DecisionCounts) > 0 || len(summary.ReasonCounts) > 0 ||
		len(summary.ActorClassCounts) > 0 || len(summary.ScopeClassCounts) > 0
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func uniqueAllowedReasons(reasons []string) []string {
	allowed := map[string]struct{}{
		"policy_not_configured":    {},
		"policy_invalid":           {},
		"policy_stale":             {},
		"policy_partial":           {},
		"shared_token_mode":        {},
		"tenant_scope_missing":     {},
		"subject_scope_missing":    {},
		"egress_policy_missing":    {},
		"redaction_policy_missing": {},
		"retention_policy_missing": {},
		"audit_sink_unavailable":   {},
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if _, ok := allowed[reason]; !ok {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	return out
}

func stringsToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func intFromEnv(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func envList(raw string) []string {
	fields := strings.Split(raw, ",")
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if value := strings.TrimSpace(field); value != "" {
			out = append(out, value)
		}
	}
	return out
}
