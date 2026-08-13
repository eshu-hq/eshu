// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// schema is the assistant_fast_path_hook.v1 identifier stamped into
// Output.Schema; see Output for what is and is not marshalled.
const schema = "assistant_fast_path_hook.v1"

// DefaultBudget is the wall-time budget `eshu assistant hook preflight`
// uses when the caller does not override it with --budget, and the floor
// normalizeInput applies to any non-positive Input.Budget. Evaluate skips
// with reasonTimeout once Input.Elapsed exceeds Input.Budget; that is the
// first check in its switch, so it wins the reason code over every other
// ineligible condition.
const DefaultBudget = 200 * time.Millisecond

// defaultLimit is the result-count limit stamped onto every PlannedCall.
const defaultLimit = 5

// supportedHostClaude is the only Input.Host value Evaluate advises for.
// Any other host is a skip with reasonUnsupportedHost when no earlier check
// in Evaluate's switch already decided the reason.
const supportedHostClaude = "claude"

// Freshness states an Input.Freshness (and Output.Truth.FreshnessState) may
// carry. FreshnessFresh is the default; freshnessUnavailable is a skip with
// reasonUnavailableContext when no earlier check in Evaluate already decided
// the reason -- freshness is read last, after scope resolution.
// freshnessStale and freshnessBuilding still advise, with reasonStaleIndex
// instead of reasonBoundedPreflight.
const (
	FreshnessFresh       = "fresh"
	freshnessStale       = "stale"
	freshnessBuilding    = "building"
	freshnessUnavailable = "unavailable"
)

// Permission states an Input.Permission may carry. PermissionAllowed is the
// default; permissionDenied is a skip with reasonPermissionDenied when no
// earlier check in Evaluate's switch already decided the reason. That check
// runs before scope resolution, so a denial never reaches Output.Scope.
const (
	PermissionAllowed = "allowed"
	permissionDenied  = "denied"
)

// Decision values Output.Decision may carry. DecisionAdvise is the only
// decision the CLI's --json mode emits Claude hook JSON for.
const (
	DecisionAdvise = "advise"
	decisionSkip   = "skip"
)

// Reason values Output.Reason may carry, naming why Evaluate advised or
// skipped.
const (
	reasonBoundedPreflight   = "bounded_preflight"
	reasonBroadScope         = "broad_scope"
	reasonDisabled           = "hook_disabled"
	reasonDisallowedTrigger  = "disallowed_trigger"
	reasonPermissionDenied   = "eshu_permission_denied"
	reasonStaleIndex         = "stale_index"
	reasonTimeout            = "eshu_hook_timeout"
	reasonUnsupportedHost    = "unsupported_host"
	reasonUnavailableContext = "eshu_mcp_unavailable"
)

// Input is the resolved preflight request: the flags and (when present)
// merged Claude PreToolUse fields that Evaluate classifies. The caller
// (go/cmd/eshu's wrapper) fills Elapsed from its own start time; Evaluate
// never measures wall time itself.
type Input struct {
	Host        string
	Enabled     bool
	Trigger     string
	Tool        string
	RepoPath    string
	EntityID    string
	Service     string
	Workload    string
	Environment string
	Resource    string
	Freshness   string
	Permission  string
	Budget      time.Duration
	Elapsed     time.Duration
}

// Output is the full preflight decision. Its json tags name the
// assistant_fast_path_hook.v1 fields, but no caller marshals Output today:
// without --json the CLI renders it as text through RenderPreflightText,
// and with --json it emits the narrower Claude PreToolUse hook JSON built
// by ClaudePreToolUseOutputForPreflight.
type Output struct {
	Schema      string       `json:"schema"`
	Decision    string       `json:"decision"`
	Reason      string       `json:"reason"`
	Message     string       `json:"message"`
	Host        string       `json:"host"`
	Trigger     string       `json:"trigger"`
	Tool        string       `json:"tool,omitempty"`
	Scope       *Scope       `json:"scope,omitempty"`
	PlannedCall *PlannedCall `json:"planned_call,omitempty"`
	Truth       Truth        `json:"truth"`
	BudgetMS    int64        `json:"budget_ms"`
	ElapsedMS   int64        `json:"elapsed_ms"`
}

// Scope is the single narrow handle Evaluate resolved the advisory around:
// one of repo_path, entity_id, service, workload, environment, or resource.
type Scope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PlannedCall is the bounded MCP tool call Evaluate recommends for Scope,
// carrying the same timeout the hook itself was budgeted.
type PlannedCall struct {
	Tool      string            `json:"tool"`
	Arguments map[string]string `json:"arguments"`
	Limit     int               `json:"limit"`
	TimeoutMS int64             `json:"timeout_ms"`
}

// Truth labels Output's evidence tier. Every Evaluate result is advisory,
// local-preflight, and untruncated; FreshnessState carries Input.Freshness
// after normalizeInput trims and lowercases it (and defaults an empty value
// to FreshnessFresh).
type Truth struct {
	Level          string `json:"level"`
	Profile        string `json:"profile"`
	FreshnessState string `json:"freshness_state"`
	Truncated      bool   `json:"truncated"`
}

// Evaluate classifies a preflight request into an advise-or-skip Output.
// It fails open (skip, never an error) on every ineligible condition: an
// expired budget, an unsupported host, a disabled hook, a disallowed
// trigger, a denied permission, a missing or unsafe scope, and unavailable
// index freshness. It advises only once a single narrow, share-safe scope
// resolves; a stale or building index still advises, with a degraded-
// freshness message.
func Evaluate(input Input) Output {
	normalized := normalizeInput(input)
	out := baseOutput(normalized)
	switch {
	case normalized.Elapsed > normalized.Budget:
		return skip(out, reasonTimeout, "hook budget expired; original host action should continue")
	case normalized.Host != supportedHostClaude:
		return skip(out, reasonUnsupportedHost, "host is guidance-only; no hook output")
	case !normalized.Enabled:
		return skip(out, reasonDisabled, "hook is not explicitly enabled")
	case !triggerAllowed(normalized.Trigger):
		return skip(out, reasonDisallowedTrigger, "trigger class is not eligible for hook output")
	case normalized.Permission == permissionDenied:
		return skip(out, reasonPermissionDenied, "permission denied; no scoped context emitted")
	}
	scope, ok := scopeFromInput(normalized)
	if !ok {
		return skip(out, reasonBroadScope, "provide a narrower repo, file, symbol, service, workload, environment, or resource scope")
	}
	out.Scope = &scope
	if normalized.Freshness == freshnessUnavailable {
		return skip(out, reasonUnavailableContext, "Eshu context is unavailable; original host action should continue")
	}
	out.Decision = DecisionAdvise
	out.Reason = reasonBoundedPreflight
	out.Message = "bounded Eshu preflight available"
	if normalized.Freshness == freshnessStale || normalized.Freshness == freshnessBuilding {
		out.Reason = reasonStaleIndex
		out.Message = "bounded Eshu preflight available with degraded freshness"
	}
	out.PlannedCall = plannedCallForScope(scope, normalized.Budget)
	return out
}

func normalizeInput(input Input) Input {
	input.Host = strings.ToLower(strings.TrimSpace(input.Host))
	input.Trigger = strings.ToLower(strings.TrimSpace(input.Trigger))
	input.Tool = strings.TrimSpace(input.Tool)
	input.Freshness = strings.ToLower(strings.TrimSpace(input.Freshness))
	if input.Freshness == "" {
		input.Freshness = FreshnessFresh
	}
	input.Permission = strings.ToLower(strings.TrimSpace(input.Permission))
	if input.Permission == "" {
		input.Permission = PermissionAllowed
	}
	if input.Budget <= 0 {
		input.Budget = DefaultBudget
	}
	input.RepoPath = strings.TrimSpace(input.RepoPath)
	input.EntityID = strings.TrimSpace(input.EntityID)
	input.Service = strings.TrimSpace(input.Service)
	input.Workload = strings.TrimSpace(input.Workload)
	input.Environment = strings.TrimSpace(input.Environment)
	input.Resource = strings.TrimSpace(input.Resource)
	return input
}

func baseOutput(input Input) Output {
	return Output{
		Schema:    schema,
		Decision:  decisionSkip,
		Reason:    reasonBroadScope,
		Host:      input.Host,
		Trigger:   input.Trigger,
		Tool:      input.Tool,
		BudgetMS:  input.Budget.Milliseconds(),
		ElapsedMS: input.Elapsed.Milliseconds(),
		Truth: Truth{
			Level:          "advisory",
			Profile:        "local_preflight",
			FreshnessState: input.Freshness,
			Truncated:      false,
		},
	}
}

func skip(out Output, reason, message string) Output {
	out.Decision = decisionSkip
	out.Reason = reason
	out.Message = message
	out.Scope = nil
	out.PlannedCall = nil
	return out
}

func triggerAllowed(trigger string) bool {
	switch trigger {
	case "read", "search", "grep", "glob", "symbol", "prompt":
		return true
	default:
		return false
	}
}

func scopeFromInput(input Input) (Scope, bool) {
	candidates := []Scope{
		{Kind: "repo_path", ID: input.RepoPath},
		{Kind: "entity_id", ID: input.EntityID},
		{Kind: "service", ID: input.Service},
		{Kind: "workload", ID: input.Workload},
		{Kind: "environment", ID: input.Environment},
		{Kind: "resource", ID: input.Resource},
	}
	for _, scope := range candidates {
		if scope.ID == "" {
			continue
		}
		if scopeSafe(scope) {
			return scope, true
		}
		return Scope{}, false
	}
	return Scope{}, false
}

func scopeSafe(scope Scope) bool {
	if strings.Contains(scope.ID, "://") || strings.HasPrefix(scope.ID, "/") ||
		strings.HasPrefix(scope.ID, "~") || strings.Contains(scope.ID, "\\") ||
		strings.Contains(scope.ID, "..") || strings.TrimSpace(scope.ID) == "" {
		return false
	}
	for _, r := range scope.ID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '_', '-', '/', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func plannedCallForScope(scope Scope, budget time.Duration) *PlannedCall {
	tool := "get_code_relationship_story"
	argKey := scope.Kind
	switch scope.Kind {
	case "service":
		tool = "get_service_story"
	case "workload", "environment":
		tool = "trace_deployment_chain"
	case "resource":
		tool = "investigate_resource"
		argKey = "resource_handle"
	}
	return &PlannedCall{
		Tool:      tool,
		Arguments: map[string]string{argKey: scope.ID},
		Limit:     defaultLimit,
		TimeoutMS: budget.Milliseconds(),
	}
}

// RenderPreflightText writes the plain-text form of out to w: the decision
// and reason, the message, the resolved scope and planned call when
// present, and the truth summary. This is what the CLI prints when --json
// is not set.
func RenderPreflightText(w io.Writer, out Output) {
	_, _ = fmt.Fprintf(w, "Assistant hook preflight: %s (%s)\n", out.Decision, out.Reason)
	if out.Message != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", out.Message)
	}
	if out.Scope != nil {
		_, _ = fmt.Fprintf(w, "  scope: %s=%s\n", out.Scope.Kind, out.Scope.ID)
	}
	if out.PlannedCall != nil {
		_, _ = fmt.Fprintf(w, "  next: %s limit=%d timeout_ms=%d\n", out.PlannedCall.Tool, out.PlannedCall.Limit, out.PlannedCall.TimeoutMS)
	}
	_, _ = fmt.Fprintf(w, "  truth: level=%s freshness=%s truncated=%v\n", out.Truth.Level, out.Truth.FreshnessState, out.Truth.Truncated)
}
