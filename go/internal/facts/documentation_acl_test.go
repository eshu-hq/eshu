// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

func TestSourceACLStateConstantsReuseSemanticPolicyVocabulary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		factsValue  string
		policyValue string
	}{
		{SourceACLStateAllowed, semanticpolicy.ACLAllowed},
		{SourceACLStateDenied, semanticpolicy.ACLDenied},
		{SourceACLStatePartial, semanticpolicy.ACLPartial},
		{SourceACLStateMissing, semanticpolicy.ACLMissing},
		{SourceACLStateStale, semanticpolicy.ACLStale},
	}
	for _, tc := range cases {
		if tc.factsValue != tc.policyValue {
			t.Fatalf("source ACL state %q must equal semanticpolicy value %q", tc.factsValue, tc.policyValue)
		}
	}
}

func TestValidSourceACLState(t *testing.T) {
	t.Parallel()

	valid := []string{
		SourceACLStateAllowed,
		SourceACLStateDenied,
		SourceACLStatePartial,
		SourceACLStateMissing,
		SourceACLStateStale,
	}
	for _, value := range valid {
		if !ValidSourceACLState(value) {
			t.Fatalf("expected %q to be a valid source ACL state", value)
		}
	}

	invalid := []string{"", "unknown", "ALLOWED", "hidden", "permission_hidden"}
	for _, value := range invalid {
		if ValidSourceACLState(value) {
			t.Fatalf("expected %q to be an invalid source ACL state", value)
		}
	}
}

func TestDocumentationACLSummaryOmitsUnobservedSourceACLState(t *testing.T) {
	t.Parallel()

	// Absence means "no ACL claim": an unset state must not appear in the wire
	// payload. This guards the omit-when-unobserved posture.
	summary := DocumentationACLSummary{Visibility: "repository"}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(encoded), "source_acl_state") {
		t.Fatalf("unobserved source_acl_state must be omitted, got %s", encoded)
	}
}

func TestDocumentationACLSummaryEmitsObservedSourceACLState(t *testing.T) {
	t.Parallel()

	states := []string{
		SourceACLStateAllowed,
		SourceACLStateDenied,
		SourceACLStatePartial,
		SourceACLStateMissing,
		SourceACLStateStale,
	}
	for _, state := range states {
		summary := DocumentationACLSummary{Visibility: "credential_viewable", SourceACLState: state}
		encoded, err := json.Marshal(summary)
		if err != nil {
			t.Fatalf("marshal summary: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal summary: %v", err)
		}
		got, ok := decoded["source_acl_state"].(string)
		if !ok || got != state {
			t.Fatalf("expected source_acl_state %q, got %v", state, decoded["source_acl_state"])
		}
	}
}
