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
		FailureClass: "auth_denied", Reason: "org left the program", IdempotencyKey: "key-1",
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
		body["failure_class"] != "auth_denied" || body["reason"] != "org left the program" || body["idempotency_key"] != "key-1" {
		t.Fatalf("body = %#v, want kind, instance, scope, class, reason, and key", client.body)
	}
}

// TestAcknowledgeCollectorFailureOmitsAnUnsetClass keeps the optional guard out
// of the body unless the operator set it.
func TestAcknowledgeCollectorFailureOmitsAnUnsetClass(t *testing.T) {
	client := &scriptedAckClient{}
	in := fullAckInput()
	in.FailureClass = " "
	if _, err := AcknowledgeCollectorFailure(client, in); err != nil {
		t.Fatalf("AcknowledgeCollectorFailure = %v, want nil", err)
	}
	if _, present := client.body.(map[string]any)["failure_class"]; present {
		t.Fatalf("body = %#v, want no failure_class when unset", client.body)
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
