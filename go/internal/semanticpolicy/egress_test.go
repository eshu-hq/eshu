// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticpolicy_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestEvaluateRequiresSemanticProviderEgressAllow(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.Evaluate(docsOnlyPolicy(), semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
		SourcePath:        "docs/runbook.md",
		ACLState:          semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false without egress allow")
	}
	if got, want := decision.State, status.SemanticExtractionDisabledByPolicy; got != want {
		t.Fatalf("Evaluate() State = %q, want %q", got, want)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonEgressPolicyMissing; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateAllowsDocumentationWithBroadEgressOptIn(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicy()
	policy.Egress.Mode = semanticpolicy.EgressModeBroad
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
		SourcePath:        "docs/runbook.md",
		ACLState:          semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if !decision.Allowed {
		t.Fatalf("Evaluate() Allowed = false, want true with broad egress opt-in: %#v", decision)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonAllowed; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateDeniesDocumentationWhenProviderEgressDenied(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicy()
	policy.Egress = semanticpolicy.EgressPolicy{
		Mode: semanticpolicy.EgressModeRestricted,
		SemanticProviders: []semanticpolicy.EgressProviderRule{
			{
				ProviderProfileID: "semantic-docs-default",
				SourceClasses:     []string{semanticprofile.SourceDocumentation},
				Decision:          semanticpolicy.EgressDecisionDeny,
			},
		},
	}
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
		SourcePath:        "docs/runbook.md",
		ACLState:          semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false for denied provider egress")
	}
	if got, want := decision.Reason, semanticpolicy.ReasonEgressProviderDenied; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateDeniesWhenEgressAllowAndDenyOverlap(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicyWithEgress()
	policy.Egress.SemanticProviders = append(policy.Egress.SemanticProviders, semanticpolicy.EgressProviderRule{
		ProviderProfileID: "semantic-docs-default",
		SourceClasses:     []string{semanticprofile.SourceCodeHints, semanticprofile.SourceDocumentation},
		Decision:          semanticpolicy.EgressDecisionDeny,
	})
	decision := semanticpolicy.Evaluate(policy, semanticpolicy.Request{
		ProviderProfileID: "semantic-docs-default",
		SourceClass:       semanticprofile.SourceDocumentation,
		Scope:             semanticpolicy.Scope{Kind: semanticpolicy.ScopeRepository, ID: "repo-1"},
		SourcePath:        "docs/runbook.md",
		ACLState:          semanticpolicy.ACLAllowed,
	}, providerStatuses())

	if decision.Allowed {
		t.Fatal("Evaluate() Allowed = true, want false when deny overlaps allow")
	}
	if got, want := decision.Reason, semanticpolicy.ReasonEgressProviderDenied; got != want {
		t.Fatalf("Evaluate() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateEgressFailsClosedWithoutPolicy(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.EvaluateEgress(docsOnlyPolicy(), "semantic-docs-default", semanticprofile.SourceDocumentation)
	if decision.Allowed {
		t.Fatal("EvaluateEgress() Allowed = true, want false without egress policy")
	}
	if got, want := decision.Reason, semanticpolicy.ReasonEgressPolicyMissing; got != want {
		t.Fatalf("EvaluateEgress() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateEgressAllowsExplicitRestrictedRule(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.EvaluateEgress(docsOnlyPolicyWithEgress(), "semantic-docs-default", semanticprofile.SourceDocumentation)
	if !decision.Allowed {
		t.Fatalf("EvaluateEgress() Allowed = false, want true with restricted allow rule: %#v", decision)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonAllowed; got != want {
		t.Fatalf("EvaluateEgress() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateEgressDeniesProfileOutsideAllowlist(t *testing.T) {
	t.Parallel()

	decision := semanticpolicy.EvaluateEgress(docsOnlyPolicyWithEgress(), "semantic-other-profile", semanticprofile.SourceDocumentation)
	if decision.Allowed {
		t.Fatal("EvaluateEgress() Allowed = true, want false for profile outside allowlist")
	}
	if got, want := decision.Reason, semanticpolicy.ReasonEgressProviderDenied; got != want {
		t.Fatalf("EvaluateEgress() Reason = %q, want %q", got, want)
	}
}

func TestEvaluateEgressAllowsBroadOptIn(t *testing.T) {
	t.Parallel()

	policy := docsOnlyPolicy()
	policy.Egress.Mode = semanticpolicy.EgressModeBroad
	decision := semanticpolicy.EvaluateEgress(policy, "semantic-docs-default", semanticprofile.SourceDocumentation)
	if !decision.Allowed {
		t.Fatalf("EvaluateEgress() Allowed = false, want true with broad opt-in: %#v", decision)
	}
	if got, want := decision.Reason, semanticpolicy.ReasonAllowed; got != want {
		t.Fatalf("EvaluateEgress() Reason = %q, want %q", got, want)
	}
}

func TestApplyToProviderStatusesRequiresSemanticProviderEgress(t *testing.T) {
	t.Parallel()

	applied := semanticpolicy.ApplyToProviderStatuses(providerStatuses(), docsOnlyPolicy())
	if len(applied) != 1 {
		t.Fatalf("len(applied) = %d, want 1", len(applied))
	}
	if applied[0].SourcePolicyConfigured {
		t.Fatal("SourcePolicyConfigured = true, want false without semantic provider egress allow")
	}
	if len(applied[0].SourceClasses) != 0 {
		t.Fatalf("SourceClasses = %#v, want empty without semantic provider egress allow", applied[0].SourceClasses)
	}
}

func TestParsePolicyJSONRejectsBroadEgressWithProviderRules(t *testing.T) {
	t.Parallel()

	_, err := semanticpolicy.ParsePolicyJSON(`{
		"policy_id": "semantic-hosted-policy",
		"enabled": true,
		"egress": {
			"mode": "broad",
			"semantic_providers": [
				{
					"provider_profile_id": "semantic-docs-default",
					"source_classes": ["documentation"],
					"decision": "deny"
				}
			]
		},
		"rules": [
			{
				"rule_id": "docs-repo-1",
				"provider_profile_id": "semantic-docs-default",
				"source_classes": ["documentation"],
				"scopes": [{"kind": "repository", "id": "repo-1"}],
				"source_allowlist": [{"kind": "path_prefix", "value": "docs/"}],
				"settings": {
					"limits": {
						"max_chunk_bytes": 8192,
						"max_tokens_per_chunk": 2048,
						"max_daily_tokens": 100000
					},
					"redaction": {"mode": "strict"},
					"retention": {
						"posture": "metadata_only",
						"prompt": "none",
						"response": "hash_only"
					}
				}
			}
		]
	}`)
	if err == nil {
		t.Fatal("ParsePolicyJSON() error = nil, want broad egress with provider rules rejection")
	}
}

func docsOnlyPolicyWithEgress() semanticpolicy.Policy {
	policy := docsOnlyPolicy()
	policy.Egress = semanticpolicy.EgressPolicy{
		Mode: semanticpolicy.EgressModeRestricted,
		SemanticProviders: []semanticpolicy.EgressProviderRule{
			{
				ProviderProfileID: "semantic-docs-default",
				SourceClasses:     []string{semanticprofile.SourceDocumentation},
				Decision:          semanticpolicy.EgressDecisionAllow,
			},
		},
	}
	return policy
}
