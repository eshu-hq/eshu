// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/admin"
)

// TestAdminCollectorFailureCommandTree pins the #8830 operator commands: both
// subcommands exist under `eshu admin collector-failure` with the flags the
// docs name, and the acknowledge help points at the list command rather than the
// reducer work-items query.
func TestAdminCollectorFailureCommandTree(t *testing.T) {
	lockCommandTree(t)
	group, _, err := adminCmd.Find([]string{"collector-failure"})
	if err != nil || group == nil || group.Name() != "collector-failure" {
		t.Fatalf("admin command has no collector-failure group: %v", err)
	}
	flags := map[string][]string{
		"list":        {"collector-kind", "instance", "limit", "cursor"},
		"acknowledge": {"collector-kind", "instance", "scope", "reason", "idempotency-key"},
	}
	for name, want := range flags {
		sub, _, err := group.Find([]string{name})
		if err != nil || sub == nil || sub.Name() != name {
			t.Fatalf("collector-failure has no %s subcommand: %v", name, err)
		}
		for _, flag := range want {
			if sub.Flags().Lookup(flag) == nil {
				t.Fatalf("%s has no --%s flag", name, flag)
			}
		}
	}
	ack, _, _ := group.Find([]string{"acknowledge"})
	if ack.Flags().Lookup("failure-class") != nil {
		t.Fatal("acknowledge still has --failure-class; the acknowledgement records the observed class")
	}
	if strings.Contains(ack.Long, "work-items/query") || !strings.Contains(ack.Long, "collector-failure list") {
		t.Fatalf("acknowledge help = %q, want it to point at `collector-failure list`", ack.Long)
	}
}

func TestWriteCollectorFailureListPrintsOneLinePerItemAndTheNextCursor(t *testing.T) {
	var out, errOut bytes.Buffer
	writeCollectorFailureList(&out, &errOut, admin.CollectorFailuresPage{
		Truncated: true, NextCursor: "scope-c",
		Items: []admin.CollectorFailureItem{
			{ScopeID: "scope-b", FailureClass: "rate_limited", FailureOperation: "list_org_alerts", FailedAt: "2026-10-24T10:00:00Z", WorkItemID: "work-b"},
			{ScopeID: "scope-c", FailureClass: "auth_denied", FailureOperation: "list_org_alerts", FailedAt: "2026-10-24T10:01:00Z", Acknowledged: true, WorkItemID: "work-c"},
		},
	})
	want := "scope-b rate_limited list_org_alerts 2026-10-24T10:00:00Z false work-b\n" +
		"scope-c auth_denied list_org_alerts 2026-10-24T10:01:00Z true work-c\n"
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
	if errOut.String() != "next cursor: scope-c\n" {
		t.Fatalf("stderr = %q, want the next cursor", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	writeCollectorFailureList(&out, &errOut, admin.CollectorFailuresPage{})
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("an empty last page printed %q / %q, want nothing", out.String(), errOut.String())
	}
}
