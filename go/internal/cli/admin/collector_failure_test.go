// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"errors"
	"strings"
	"testing"
)

// scriptedAckClient records the acknowledge POST.
type scriptedAckClient struct {
	path string
	body any
	err  error
}

func (c *scriptedAckClient) Get(_ string, _ any) error { return errors.New("unused") }

func (c *scriptedAckClient) Post(path string, body, _ any) error {
	c.path = path
	c.body = body
	return c.err
}

func fullAckInput() AcknowledgeCollectorFailureInput {
	return AcknowledgeCollectorFailureInput{
		CollectorKind: "security_alert", InstanceID: "alerts", ScopeID: "org:example",
		Reason: "org left the program", IdempotencyKey: "key-1",
	}
}

// TestAcknowledgeCollectorFailurePostsTheFailureReasonAndKey proves the #8830
// operator command shape.
func TestAcknowledgeCollectorFailurePostsTheFailureReasonAndKey(t *testing.T) {
	client := &scriptedAckClient{}
	if _, err := AcknowledgeCollectorFailure(client, fullAckInput()); err != nil {
		t.Fatalf("AcknowledgeCollectorFailure = %v, want nil", err)
	}
	if client.path != "/api/v0/admin/collector-failures/acknowledge" {
		t.Fatalf("path = %q, want the acknowledge route", client.path)
	}
	body, ok := client.body.(map[string]any)
	if !ok || body["collector_kind"] != "security_alert" || body["instance_id"] != "alerts" || body["scope_id"] != "org:example" ||
		body["reason"] != "org left the program" || body["idempotency_key"] != "key-1" {
		t.Fatalf("body = %#v, want kind, instance, scope, class, reason, and key", client.body)
	}
}

// TestAcknowledgeCollectorFailureRefusesMissingInputs proves the CLI rejects an
// incomplete command before any HTTP call, naming the flag to pass.
func TestAcknowledgeCollectorFailureRefusesMissingInputs(t *testing.T) {
	cases := map[string]func(*AcknowledgeCollectorFailureInput){
		"--collector-kind": func(in *AcknowledgeCollectorFailureInput) { in.CollectorKind = " " },
		"--instance":       func(in *AcknowledgeCollectorFailureInput) { in.InstanceID = " " },
		"--scope":          func(in *AcknowledgeCollectorFailureInput) { in.ScopeID = "" },
		"--reason":         func(in *AcknowledgeCollectorFailureInput) { in.Reason = "  " },
	}
	for flag, mutate := range cases {
		client := &scriptedAckClient{}
		in := fullAckInput()
		mutate(&in)
		_, err := AcknowledgeCollectorFailure(client, in)
		if err == nil || !strings.Contains(err.Error(), flag) {
			t.Fatalf("%s blank: err = %v, want a message naming the flag", flag, err)
		}
		if client.path != "" {
			t.Fatalf("%s blank: HTTP ran", flag)
		}
	}
}

func TestAcknowledgeCollectorFailureGeneratesKeyWhenBlank(t *testing.T) {
	client := &scriptedAckClient{}
	in := fullAckInput()
	in.IdempotencyKey = ""
	if _, err := AcknowledgeCollectorFailure(client, in); err != nil {
		t.Fatalf("AcknowledgeCollectorFailure = %v, want nil", err)
	}
	key, _ := client.body.(map[string]any)["idempotency_key"].(string)
	if !strings.HasPrefix(key, "cli-ack-failure-") {
		t.Fatalf("idempotency_key = %q, want a generated cli-ack-failure- key", key)
	}
}

// scriptedListClient records the list POST and replays a canned page or error.
type scriptedListClient struct {
	path string
	body any
	page CollectorFailuresPage
	err  error
}

func (c *scriptedListClient) Get(_ string, _ any) error { return errors.New("unused") }

func (c *scriptedListClient) Post(path string, body, out any) error {
	c.path = path
	c.body = body
	if c.err != nil {
		return c.err
	}
	if decoded, ok := out.(*CollectorFailuresPage); ok {
		*decoded = c.page
	}
	return nil
}

// TestListCollectorFailuresPostsTheInstanceAndPaging proves the #8830 discovery
// command shape: kind and instance always, limit and cursor only when set.
func TestListCollectorFailuresPostsTheInstanceAndPaging(t *testing.T) {
	client := &scriptedListClient{page: CollectorFailuresPage{Count: 1, Items: []CollectorFailureItem{{ScopeID: "org:example", Acknowledged: false}}}}
	page, err := ListCollectorFailures(client, ListCollectorFailuresInput{CollectorKind: "security_alert", InstanceID: "alerts"})
	if err != nil {
		t.Fatalf("ListCollectorFailures = %v, want nil", err)
	}
	if client.path != "/api/v0/admin/collector-failures/query" {
		t.Fatalf("path = %q, want the list route", client.path)
	}
	body, _ := client.body.(map[string]any)
	if body["collector_kind"] != "security_alert" || body["instance_id"] != "alerts" {
		t.Fatalf("body = %#v, want kind and instance", client.body)
	}
	if _, present := body["limit"]; present {
		t.Fatalf("body = %#v, want no limit when unset", client.body)
	}
	if _, present := body["cursor"]; present {
		t.Fatalf("body = %#v, want no cursor when unset", client.body)
	}
	if len(page.Items) != 1 || page.Items[0].ScopeID != "org:example" {
		t.Fatalf("page = %+v, want the decoded items", page)
	}

	if _, err := ListCollectorFailures(client, ListCollectorFailuresInput{CollectorKind: "security_alert", InstanceID: "alerts", Limit: 25, Cursor: "scope-c"}); err != nil {
		t.Fatalf("ListCollectorFailures(paged) = %v, want nil", err)
	}
	body, _ = client.body.(map[string]any)
	if body["limit"] != 25 || body["cursor"] != "scope-c" {
		t.Fatalf("body = %#v, want limit 25 and cursor scope-c", client.body)
	}
}

func TestListCollectorFailuresRefusesMissingInputsAndKeepsTheGateError(t *testing.T) {
	for flag, in := range map[string]ListCollectorFailuresInput{
		"--collector-kind": {InstanceID: "alerts"},
		"--instance":       {CollectorKind: "security_alert", InstanceID: " "},
	} {
		client := &scriptedListClient{}
		if _, err := ListCollectorFailures(client, in); err == nil || !strings.Contains(err.Error(), flag) {
			t.Fatalf("%s blank: err = %v, want a message naming the flag", flag, err)
		}
		if client.path != "" {
			t.Fatalf("%s blank: HTTP ran", flag)
		}
	}
	gate := errors.New("403 Forbidden: listing collector terminal failures requires an admin (all-scopes) token")
	if _, err := ListCollectorFailures(&scriptedListClient{err: gate}, ListCollectorFailuresInput{CollectorKind: "security_alert", InstanceID: "alerts"}); !errors.Is(err, gate) {
		t.Fatalf("err = %v, want the API's gate error returned unchanged", err)
	}
}
