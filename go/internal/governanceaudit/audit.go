// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceaudit

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// EventType names one hosted governance decision family.
type EventType string

const (
	// EventTypeAPIMCPAuthentication covers API or MCP authentication decisions.
	EventTypeAPIMCPAuthentication EventType = "api_mcp_authentication"
	// EventTypeIdentityAuthentication covers human local, OIDC, and SAML login/logout decisions.
	EventTypeIdentityAuthentication EventType = "identity_authentication"
	// EventTypeMFALifecycle covers MFA challenge, enrollment, reset, and recovery-code decisions.
	EventTypeMFALifecycle EventType = "mfa_lifecycle"
	// EventTypeSessionLifecycle covers server-managed dashboard session decisions.
	EventTypeSessionLifecycle EventType = "session_lifecycle"
	// EventTypeReadAuthorization covers API, MCP, or admin read authorization.
	EventTypeReadAuthorization EventType = "read_authorization"
	// EventTypeTokenLifecycle covers scoped token creation, rotation, or revoke decisions.
	EventTypeTokenLifecycle EventType = "token_lifecycle"
	// EventTypeIDPConfigChange covers external identity-provider configuration changes.
	EventTypeIDPConfigChange EventType = "idp_config_change"
	// EventTypeRoleGrantChange covers role, grant, and data-class permission changes.
	EventTypeRoleGrantChange EventType = "role_grant_change"
	// EventTypeTenantSwitch covers active tenant or workspace switch decisions.
	EventTypeTenantSwitch EventType = "tenant_switch"
	// EventTypeSensitiveDataAccess covers sensitive data-class reads.
	EventTypeSensitiveDataAccess EventType = "sensitive_data_access"
	// EventTypeAskSearchRun covers governed Ask Eshu and semantic/search runs.
	EventTypeAskSearchRun EventType = "ask_search_run"
	// EventTypeExport covers export, report, and portable bundle decisions.
	EventTypeExport EventType = "export"
	// EventTypeBootstrap covers bootstrap-owner and first-setup decisions.
	EventTypeBootstrap EventType = "bootstrap"
	// EventTypeBreakGlass covers time-boxed break-glass access decisions.
	EventTypeBreakGlass EventType = "break_glass"
	// EventTypeAuditRead covers authorized audit-read and incident-report decisions.
	EventTypeAuditRead EventType = "audit_read"
	// EventTypeCollectorActivation covers hosted collector enablement or claim decisions.
	EventTypeCollectorActivation EventType = "collector_activation"
	// EventTypeSemanticPolicyDecision covers source, egress, redaction, and retention decisions.
	EventTypeSemanticPolicyDecision EventType = "semantic_policy_decision"
	// EventTypeProviderBudgetDecision covers provider health, budget, or quota decisions.
	EventTypeProviderBudgetDecision EventType = "provider_budget_decision"
	// EventTypeExtensionActivation covers hosted extension trust and activation decisions.
	EventTypeExtensionActivation EventType = "extension_activation"
	// EventTypeAdminRecoveryAction covers refinalize, replay, repair, and recovery decisions.
	EventTypeAdminRecoveryAction EventType = "admin_recovery_action"
)

// ActorClass names the bounded class of the actor that caused an event.
type ActorClass string

const (
	// ActorClassAnonymous marks unauthenticated or unauthored attempts.
	ActorClassAnonymous ActorClass = "anonymous"
	// ActorClassSharedToken marks the legacy shared bearer-token actor class.
	ActorClassSharedToken ActorClass = "shared_token"
	// ActorClassScopedToken marks a future scoped token actor class.
	ActorClassScopedToken ActorClass = "scoped_token"
	// ActorClassServicePrincipal marks an internal service principal.
	ActorClassServicePrincipal ActorClass = "service_principal"
	// ActorClassOperator marks a human operator class without a direct identifier.
	ActorClassOperator ActorClass = "operator"
	// ActorClassSystem marks internal system maintenance work.
	ActorClassSystem ActorClass = "system"
)

// ScopeClass names the bounded governance scope class attached to an event.
type ScopeClass string

const (
	// ScopeClassTenant covers tenant-level policy decisions.
	ScopeClassTenant ScopeClass = "tenant"
	// ScopeClassWorkspace covers workspace-level policy decisions.
	ScopeClassWorkspace ScopeClass = "workspace"
	// ScopeClassRepository covers repository-scoped decisions.
	ScopeClassRepository ScopeClass = "repository"
	// ScopeClassSourceClass covers low-cardinality source classes.
	ScopeClassSourceClass ScopeClass = "source_class"
	// ScopeClassProviderProfile covers provider profile class or safe hash decisions.
	ScopeClassProviderProfile ScopeClass = "provider_profile"
	// ScopeClassCollectorKind covers hosted collector kind decisions.
	ScopeClassCollectorKind ScopeClass = "collector_kind"
	// ScopeClassExtensionComponent covers hosted extension component decisions.
	ScopeClassExtensionComponent ScopeClass = "extension_component"
	// ScopeClassAdmin covers global admin or recovery decisions.
	ScopeClassAdmin ScopeClass = "admin"
)

// Decision names the bounded result of a governance event.
type Decision string

const (
	// DecisionAllowed marks a permitted decision.
	DecisionAllowed Decision = "allowed"
	// DecisionDenied marks a denied decision.
	DecisionDenied Decision = "denied"
	// DecisionUnavailable marks a decision blocked by missing dependency or policy.
	DecisionUnavailable Decision = "unavailable"
)

// Event is the audit-safe hosted governance decision envelope.
type Event struct {
	Type               EventType
	ActorClass         ActorClass
	ActorIDHash        string
	ServicePrincipalID string
	ScopeClass         ScopeClass
	ScopeIDHash        string
	Decision           Decision
	ReasonCode         string
	CorrelationID      string
	PolicyRevisionHash string
	OccurredAt         time.Time
	// TenantID identifies the tenant this event belongs to. Empty/NULL for
	// genuine system-wide (global) events such as bootstrap or platform-level
	// egress decisions. MUST NOT be fabricated: only populate from the caller's
	// AuthContext when a real tenant is present.
	TenantID string
	// WorkspaceID identifies the workspace this event belongs to, when the
	// event is further scoped below the tenant level. Empty when not applicable.
	WorkspaceID string
}

// Count is one low-cardinality aggregate count.
type Count struct {
	Name  string
	Count int
}

// Summary is an aggregate audit readback safe for status and MCP surfaces.
type Summary struct {
	Total            int
	Allowed          int
	Denied           int
	Unavailable      int
	LastOccurredAt   time.Time
	EventTypeCounts  []Count
	DecisionCounts   []Count
	ReasonCounts     []Count
	ActorClassCounts []Count
	ScopeClassCounts []Count
}

// NormalizeEvent trims and validates an event without returning raw unsafe values.
func NormalizeEvent(event Event) (Event, error) {
	event.Type = EventType(strings.TrimSpace(string(event.Type)))
	event.ActorClass = ActorClass(strings.TrimSpace(string(event.ActorClass)))
	event.ActorIDHash = strings.TrimSpace(event.ActorIDHash)
	event.ServicePrincipalID = strings.TrimSpace(event.ServicePrincipalID)
	event.ScopeClass = ScopeClass(strings.TrimSpace(string(event.ScopeClass)))
	event.ScopeIDHash = strings.TrimSpace(event.ScopeIDHash)
	event.Decision = Decision(strings.TrimSpace(string(event.Decision)))
	event.ReasonCode = strings.TrimSpace(event.ReasonCode)
	event.CorrelationID = strings.TrimSpace(event.CorrelationID)
	event.PolicyRevisionHash = strings.TrimSpace(event.PolicyRevisionHash)
	event.OccurredAt = event.OccurredAt.UTC()
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.WorkspaceID = strings.TrimSpace(event.WorkspaceID)

	if !validEventType(event.Type) {
		return Event{}, fieldError("type")
	}
	if !validActorClass(event.ActorClass) {
		return Event{}, fieldError("actor_class")
	}
	if event.ActorIDHash == "" && event.ServicePrincipalID == "" &&
		event.ActorClass != ActorClassAnonymous && event.ActorClass != ActorClassSystem {
		return Event{}, fieldError("actor_identity")
	}
	if !validOptionalHash(event.ActorIDHash) {
		return Event{}, fieldError("actor_id_hash")
	}
	if !validOptionalToken(event.ServicePrincipalID) {
		return Event{}, fieldError("service_principal_id")
	}
	if !validScopeClass(event.ScopeClass) {
		return Event{}, fieldError("scope_class")
	}
	if !validOptionalHash(event.ScopeIDHash) {
		return Event{}, fieldError("scope_id_hash")
	}
	if !validDecision(event.Decision) {
		return Event{}, fieldError("decision")
	}
	if !validReasonCode(event.ReasonCode) {
		return Event{}, fieldError("reason_code")
	}
	if !validOptionalToken(event.CorrelationID) {
		return Event{}, fieldError("correlation_id")
	}
	if !validOptionalHash(event.PolicyRevisionHash) {
		return Event{}, fieldError("policy_revision_hash")
	}
	if event.OccurredAt.IsZero() {
		return Event{}, fieldError("occurred_at")
	}
	return event, nil
}

// Aggregate validates events and returns low-cardinality counts.
func Aggregate(events []Event) (Summary, error) {
	var summary Summary
	eventTypes := map[string]int{}
	decisions := map[string]int{}
	reasons := map[string]int{}
	actors := map[string]int{}
	scopes := map[string]int{}

	for _, event := range events {
		normalized, err := NormalizeEvent(event)
		if err != nil {
			return Summary{}, err
		}
		summary.Total++
		switch normalized.Decision {
		case DecisionAllowed:
			summary.Allowed++
		case DecisionDenied:
			summary.Denied++
		case DecisionUnavailable:
			summary.Unavailable++
		}
		if normalized.OccurredAt.After(summary.LastOccurredAt) {
			summary.LastOccurredAt = normalized.OccurredAt
		}
		eventTypes[string(normalized.Type)]++
		decisions[string(normalized.Decision)]++
		reasons[normalized.ReasonCode]++
		actors[string(normalized.ActorClass)]++
		scopes[string(normalized.ScopeClass)]++
	}

	summary.EventTypeCounts = countsFromMap(eventTypes)
	summary.DecisionCounts = countsFromMap(decisions)
	summary.ReasonCounts = countsFromMap(reasons)
	summary.ActorClassCounts = countsFromMap(actors)
	summary.ScopeClassCounts = countsFromMap(scopes)
	return summary, nil
}

func fieldError(field string) error {
	return fmt.Errorf("governance audit field %q is invalid", field)
}

func validEventType(value EventType) bool {
	switch value {
	case EventTypeAPIMCPAuthentication, EventTypeIdentityAuthentication,
		EventTypeMFALifecycle, EventTypeSessionLifecycle,
		EventTypeReadAuthorization, EventTypeTokenLifecycle,
		EventTypeIDPConfigChange, EventTypeRoleGrantChange,
		EventTypeTenantSwitch, EventTypeSensitiveDataAccess,
		EventTypeAskSearchRun, EventTypeExport, EventTypeBootstrap,
		EventTypeBreakGlass, EventTypeAuditRead, EventTypeCollectorActivation,
		EventTypeSemanticPolicyDecision, EventTypeProviderBudgetDecision,
		EventTypeExtensionActivation, EventTypeAdminRecoveryAction:
		return true
	default:
		return false
	}
}

func validActorClass(value ActorClass) bool {
	switch value {
	case ActorClassAnonymous, ActorClassSharedToken, ActorClassScopedToken,
		ActorClassServicePrincipal, ActorClassOperator, ActorClassSystem:
		return true
	default:
		return false
	}
}

func validScopeClass(value ScopeClass) bool {
	switch value {
	case ScopeClassTenant, ScopeClassWorkspace, ScopeClassRepository,
		ScopeClassSourceClass, ScopeClassProviderProfile, ScopeClassCollectorKind,
		ScopeClassExtensionComponent, ScopeClassAdmin:
		return true
	default:
		return false
	}
}

func validDecision(value Decision) bool {
	switch value {
	case DecisionAllowed, DecisionDenied, DecisionUnavailable:
		return true
	default:
		return false
	}
}

func validOptionalHash(value string) bool {
	if value == "" {
		return true
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	hash := strings.TrimPrefix(value, prefix)
	if len(hash) < 8 || len(hash) > 64 {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validReasonCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func validOptionalToken(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 96 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '_' && r != '-' && r != ':' {
			return false
		}
	}
	return true
}

func countsFromMap(values map[string]int) []Count {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for name, count := range values {
		if name != "" && count > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	counts := make([]Count, 0, len(names))
	for _, name := range names {
		counts = append(counts, Count{Name: name, Count: values[name]})
	}
	return counts
}
