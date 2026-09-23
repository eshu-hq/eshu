// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// TestServicePrincipalLabelIsNeverRewritten: a single label directly under
// amazonaws.com is AWS-owned -- a customer cannot register one -- so a
// service principal such as monitoring.amazonaws.com or
// ecs-tasks.amazonaws.com (both documented AWS service principals) is kept
// verbatim even when a customer resource carries the same word as its name.
// Before the fix the name was learned and free-text substitution rewrote
// the principal to n<hex>.amazonaws.com, which is wrong graph truth.
func TestServicePrincipalLabelIsNeverRewritten(t *testing.T) {
	key := mustKey(t, keyA)
	gen := generation("aws:"+acct+":us-east-1:iam", nil,
		map[string]any{"name": "monitoring"},
		map[string]any{"name": "ecs-tasks"},
		map[string]any{
			"principal_service": "monitoring.amazonaws.com",
			"principal_value":   "ecs-tasks.amazonaws.com",
			"assume_principals": []any{"monitoring.amazonaws.com", "ecs-tasks.amazonaws.com"},
		},
	)
	_, envs, _ := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{gen}}, key, recordpolicy.Policy())
	// The names are customer data and are still pseudonymized.
	for i := 0; i < 2; i++ {
		mustMatch(t, "customer name", fmt.Sprint(envs[0][i].Payload["name"]), `^`+hexName+`$`)
	}
	payload := envs[0][2].Payload
	if got := fmt.Sprint(payload["principal_service"]); got != "monitoring.amazonaws.com" {
		t.Errorf("principal_service rewritten to shape %q", shapeOf(got))
	}
	if got := fmt.Sprint(payload["principal_value"]); got != "ecs-tasks.amazonaws.com" {
		t.Errorf("principal_value rewritten to shape %q", shapeOf(got))
	}
	if got := fmt.Sprint(payload["assume_principals"]); got != "[monitoring.amazonaws.com ecs-tasks.amazonaws.com]" {
		t.Errorf("assume_principals rewritten to shape %q", shapeOf(got))
	}
}

// TestVerifyAdmitsServicePrincipals: <label>.amazonaws.com with exactly one
// label is an AWS service principal and passes Verify by shape.
func TestVerifyAdmitsServicePrincipals(t *testing.T) {
	doc := []byte(`{"a":"states.amazonaws.com","b":"ecs-tasks.amazonaws.com","c":"Service: monitoring.amazonaws.com"}`)
	if err := recordpseudo.Verify(doc, recordpseudo.Set{}); err != nil {
		t.Fatalf("service principals refused: %v", err)
	}
	// Two raw customer labels are not a service principal.
	err := recordpseudo.Verify([]byte(`{"a":"orders-api.team-b.amazonaws.com"}`), recordpseudo.Set{})
	if err == nil || !strings.Contains(err.Error(), "alternative hostname") {
		t.Fatalf("a two-label raw host under amazonaws.com passed: %v", err)
	}
}
