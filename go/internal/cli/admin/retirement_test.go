// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"reflect"
	"testing"
)

// scriptedRetireClient records the retire POST body.
type scriptedRetireClient struct {
	path string
	body any
}

func (c *scriptedRetireClient) Get(_ string, _ any) error { return errors.New("unused") }

func (c *scriptedRetireClient) Post(path string, body, _ any) error {
	c.path = path
	c.body = body
	return nil
}

// TestRetirePostsSelectorsReasonAndKey proves the #8602 operator command
// shape: the CLI posts selectors, reason, and key to the retire route.
func TestRetirePostsSelectorsReasonAndKey(t *testing.T) {
	client := &scriptedRetireClient{}
	if _, err := Retire(client, RetireInput{
		Repositories:   []string{"trigger-repo"},
		Reason:         "deselected",
		IdempotencyKey: "key-01",
	}); err != nil {
		t.Fatalf("Retire = %v, want nil", err)
	}
	if client.path != "/api/v0/admin/repository-retirements" {
		t.Fatalf("path = %q, want the retire route", client.path)
	}
	body, ok := client.body.(map[string]any)
	if !ok {
		t.Fatalf("body = %#v, want a JSON object", client.body)
	}
	if !reflect.DeepEqual(body["repositories"], []string{"trigger-repo"}) {
		t.Fatalf("repositories = %#v, want the selectors", body["repositories"])
	}
	if body["reason"] != "deselected" || body["idempotency_key"] != "key-01" {
		t.Fatalf("body = %#v, want reason and key", body)
	}
}

// TestRetireGeneratesKeyWhenBlank proves a retried CLI invocation stays
// safe: an omitted key is generated, never sent blank.
func TestRetireGeneratesKeyWhenBlank(t *testing.T) {
	client := &scriptedRetireClient{}
	if _, err := Retire(client, RetireInput{
		Repositories: []string{"trigger-repo"},
		Reason:       "deselected",
	}); err != nil {
		t.Fatalf("Retire = %v, want nil", err)
	}
	body := client.body.(map[string]any)
	key, ok := body["idempotency_key"].(string)
	if !ok || key == "" {
		t.Fatalf("idempotency_key = %#v, want a generated key", body["idempotency_key"])
	}
}

// TestRetireRefusesBlankReason proves the CLI rejects a missing --reason
// before any HTTP call, naming the flag the operator must pass.
func TestRetireRefusesBlankReason(t *testing.T) {
	client := &scriptedRetireClient{}
	if _, err := Retire(client, RetireInput{Repositories: []string{"trigger-repo"}}); err == nil {
		t.Fatal("err = nil, want the --reason refusal")
	} else if got := err.Error(); got != "--reason is required and must explain why the retirement is safe" {
		t.Fatalf("err = %q, want the --reason message", got)
	}
	if client.path != "" {
		t.Fatal("HTTP ran for a blank reason")
	}
}
