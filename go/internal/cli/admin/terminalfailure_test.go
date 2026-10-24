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

// TestAcknowledgeTerminalFailurePostsTheFailureAndReason proves the #8830
// operator command shape: the CLI posts the instance, scope, class, and
// reason to the acknowledgement route.
func TestAcknowledgeTerminalFailurePostsTheFailureAndReason(t *testing.T) {
	client := &scriptedAckClient{}
	if _, err := AcknowledgeTerminalFailure(client, AcknowledgeTerminalFailureInput{
		InstanceID: "alerts", ScopeID: "org:example", FailureClass: "auth_denied", Reason: "org left the program",
	}); err != nil {
		t.Fatalf("AcknowledgeTerminalFailure = %v, want nil", err)
	}
	if client.path != "/api/v0/admin/collector-terminal-failure-acknowledgements" {
		t.Fatalf("path = %q, want the acknowledgement route", client.path)
	}
	body, ok := client.body.(map[string]any)
	if !ok || body["instance_id"] != "alerts" || body["scope_id"] != "org:example" ||
		body["failure_class"] != "auth_denied" || body["reason"] != "org left the program" {
		t.Fatalf("body = %#v, want instance, scope, class, and reason", client.body)
	}
}

// TestAcknowledgeTerminalFailureRefusesMissingInputs proves the CLI rejects an
// incomplete command before any HTTP call, naming the flag to pass.
func TestAcknowledgeTerminalFailureRefusesMissingInputs(t *testing.T) {
	full := AcknowledgeTerminalFailureInput{InstanceID: "alerts", ScopeID: "org:example", FailureClass: "auth_denied", Reason: "r"}
	cases := map[string]func(*AcknowledgeTerminalFailureInput){
		"--instance":      func(in *AcknowledgeTerminalFailureInput) { in.InstanceID = " " },
		"--scope":         func(in *AcknowledgeTerminalFailureInput) { in.ScopeID = "" },
		"--failure-class": func(in *AcknowledgeTerminalFailureInput) { in.FailureClass = "" },
		"--reason":        func(in *AcknowledgeTerminalFailureInput) { in.Reason = "  " },
	}
	for flag, mutate := range cases {
		client := &scriptedAckClient{}
		in := full
		mutate(&in)
		err := func() error { _, err := AcknowledgeTerminalFailure(client, in); return err }()
		if err == nil || !strings.Contains(err.Error(), flag) {
			t.Fatalf("%s blank: err = %v, want a message naming the flag", flag, err)
		}
		if client.path != "" {
			t.Fatalf("%s blank: HTTP ran", flag)
		}
	}
}
