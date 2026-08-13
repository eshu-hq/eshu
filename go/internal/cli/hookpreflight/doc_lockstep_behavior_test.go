// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// Behavioral half of the doc-lockstep guard (see doc_lockstep_test.go for the
// full rationale and conventions). These tests pin the claims the package docs
// and the preflight.go constant comments make about Evaluate's behavior:
// which reason code wins when several conditions are ineligible at once, that
// no skip publishes a scope, and which trigger classes are accepted.

// TestDocLockstepReasonPrecedence pins the check order AGENTS.md calls the one
// thing deciding which single reason code comes back, and the per-constant
// comments in preflight.go that describe it. Every case is ineligible for
// several reasons at once, so reordering Evaluate's switch changes an
// assertion here instead of silently changing what a caller sees.
func TestDocLockstepReasonPrecedence(t *testing.T) {
	t.Parallel()

	const narrowScope = "services/api/handler.go"
	cases := []struct {
		name       string
		input      Input
		wantReason string
		why        string
	}{
		{
			name: "timeout_beats_every_later_check",
			input: Input{
				Host: "codex", Enabled: false, Trigger: "edit",
				Permission: permissionDenied, Budget: DefaultBudget,
				Elapsed: DefaultBudget + time.Millisecond,
			},
			wantReason: reasonTimeout,
			why:        "DefaultBudget's comment: the timeout check runs first and wins over every other ineligible condition",
		},
		{
			name: "unsupported_host_beats_disabled_and_later",
			input: Input{
				Host: "codex", Enabled: false, Trigger: "edit",
				Permission: permissionDenied, Budget: DefaultBudget,
			},
			wantReason: reasonUnsupportedHost,
			why:        "supportedHostClaude's comment: any other host skips when no earlier check decided the reason",
		},
		{
			name: "disabled_beats_trigger_and_later",
			input: Input{
				Host: supportedHostClaude, Enabled: false, Trigger: "edit",
				Permission: permissionDenied, Budget: DefaultBudget,
			},
			wantReason: reasonDisabled,
			why:        "Evaluate's switch: enabled is checked before trigger and permission",
		},
		{
			name: "disallowed_trigger_beats_permission",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "edit",
				Permission: permissionDenied, Budget: DefaultBudget,
			},
			wantReason: reasonDisallowedTrigger,
			why:        "Evaluate's switch: trigger is checked before permission",
		},
		{
			name: "permission_denied_beats_broad_scope",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				Permission: permissionDenied, Budget: DefaultBudget,
			},
			wantReason: reasonPermissionDenied,
			why:        "permissionDenied's comment: the permission check runs before scope resolution",
		},
		{
			name: "permission_denied_beats_unavailable_freshness",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				RepoPath: narrowScope, Permission: permissionDenied,
				Freshness: freshnessUnavailable, Budget: DefaultBudget,
			},
			wantReason: reasonPermissionDenied,
			why:        "freshness is read last, after both permission and scope",
		},
		{
			name: "broad_scope_beats_unavailable_freshness",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				Freshness: freshnessUnavailable, Budget: DefaultBudget,
			},
			wantReason: reasonBroadScope,
			why:        "Freshness constants' comment: freshness is read last, after scope resolution",
		},
		{
			name: "unavailable_freshness_wins_once_scope_resolves",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				RepoPath: narrowScope, Freshness: freshnessUnavailable,
				Budget: DefaultBudget,
			},
			wantReason: reasonUnavailableContext,
			why:        "the last skip in Evaluate, reached only after a safe scope resolves",
		},
	}
	if len(cases) < 8 {
		t.Fatalf("precedence cases = %d, want one per documented ordering claim", len(cases))
	}

	checked := 0
	for _, tc := range cases {
		out := Evaluate(tc.input)
		if out.Reason != tc.wantReason {
			t.Errorf("%s: reason = %q, want %q (%s)", tc.name, out.Reason, tc.wantReason, tc.why)
		}
		checked++
	}
	if checked != len(cases) {
		t.Fatalf("evaluated %d of %d precedence cases", checked, len(cases))
	}
}

// TestDocLockstepSkipCarriesNoScope pins the publish-safety claim AGENTS.md
// makes about scopeSafe and permissionDenied's comment makes about denials: no
// skip Output carries a Scope or PlannedCall. The unavailable-freshness case
// is the one that needs skip's own clearing -- it is the only path where
// Evaluate has already assigned Output.Scope before deciding to skip.
func TestDocLockstepSkipCarriesNoScope(t *testing.T) {
	t.Parallel()

	const narrowScope = "services/api/handler.go"
	cases := []struct {
		name       string
		input      Input
		wantReason string
	}{
		{
			name: "permission_denied",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				RepoPath: narrowScope, Permission: permissionDenied,
				Budget: DefaultBudget,
			},
			wantReason: reasonPermissionDenied,
		},
		{
			name: "unavailable_freshness_after_scope_resolved",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				RepoPath: narrowScope, Freshness: freshnessUnavailable,
				Budget: DefaultBudget,
			},
			wantReason: reasonUnavailableContext,
		},
		{
			name: "timeout_after_scope_would_resolve",
			input: Input{
				Host: supportedHostClaude, Enabled: true, Trigger: "read",
				RepoPath: narrowScope, Budget: DefaultBudget,
				Elapsed: DefaultBudget + time.Millisecond,
			},
			wantReason: reasonTimeout,
		},
	}
	if len(cases) == 0 {
		t.Fatal("no skip paths to check; the assertion would be vacuous")
	}

	for _, tc := range cases {
		out := Evaluate(tc.input)
		if out.Decision != decisionSkip || out.Reason != tc.wantReason {
			t.Errorf("%s: decision=%q reason=%q, want skip/%s", tc.name, out.Decision, out.Reason, tc.wantReason)
			continue
		}
		if out.Scope != nil {
			t.Errorf("%s: Output.Scope = %+v, want nil; a skip must not publish the resolved scope", tc.name, out.Scope)
		}
		if out.PlannedCall != nil {
			t.Errorf("%s: Output.PlannedCall = %+v, want nil for a skip", tc.name, out.PlannedCall)
		}
	}
}

// TestDocLockstepAllowedTriggersMatchDoc pins the trigger classes
// triggerAllowed accepts against the set the contract doc's "Trigger Classes"
// section permits: source file read, text or symbol search (which this package
// splits into read/search/grep), glob discovery, editor symbol lookup, and
// prompt-visible requests. Adding a class without updating that doc fails
// here. Sorted for a stable failure message.
func TestDocLockstepAllowedTriggersMatchDoc(t *testing.T) {
	t.Parallel()

	documented := []string{"glob", "grep", "prompt", "read", "search", "symbol"}
	candidates := append([]string{}, documented...)
	candidates = append(candidates, "edit", "write", "bash", "")

	var allowed []string
	for _, trigger := range candidates {
		if triggerAllowed(trigger) {
			allowed = append(allowed, trigger)
		}
	}
	sort.Strings(allowed)
	if len(allowed) == 0 {
		t.Fatal("no trigger was allowed; the assertion would be vacuous")
	}
	if strings.Join(allowed, ",") != strings.Join(documented, ",") {
		t.Errorf("triggerAllowed accepts %v, docs name %v; update docs/public/reference/assistant-fast-path-hooks.md's Trigger Classes section in the same change", allowed, documented)
	}
}
