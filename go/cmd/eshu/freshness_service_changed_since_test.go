// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

func TestFreshnessServiceChangedSinceCommandIsRegistered(t *testing.T) {
	lockCommandTree(t)

	cmd, _, err := rootCmd.Find([]string{"freshness", "service-changed-since"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness service-changed-since) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "service-changed-since" {
		t.Fatalf("resolved command = %#v, want service-changed-since", cmd)
	}
	for _, name := range []string{"json", "service-id", "since-generation-id", "sample-limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness service-changed-since flag %q missing", name)
		}
	}
}

func TestFreshnessServiceChangedSinceOptionsCarryEveryFlag(t *testing.T) {
	cmd := newFreshnessServiceChangedSinceCommand()
	for name, value := range map[string]string{
		"json": "true", "service-id": "svc-a", "since-generation-id": "gen-prior", "sample-limit": "40",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	opts, err := freshnessServiceChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		t.Fatalf("freshnessServiceChangedSinceOptionsFromCommand() error = %v", err)
	}
	want := freshness.ServiceChangedSinceOptions{
		JSON: true, ServiceID: "svc-a", SinceGenerationID: "gen-prior", SampleLimit: 40,
	}
	if opts != want {
		t.Fatalf("options = %#v, want %#v", opts, want)
	}
}

func TestRunFreshnessServiceChangedSinceRendersSummary(t *testing.T) {
	server := freshnessTestServer(t, http.StatusOK,
		`{"data":{"service_id":"svc-a","since_generation_id":"gen-prior",`+
			`"current_active_generation_id":"gen-current","unavailable":false,"categories":[`+
			`{"category":"evidence","unavailable":false,"counts":{"added":4,"updated":0,"unchanged":9,`+
			`"retired":2,"superseded":3}}]},"truth":{"freshness":{"state":"stale"}},"error":null}`)

	out := &bytes.Buffer{}
	cmd := newFreshnessServiceChangedSinceCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	if err := runFreshnessServiceChangedSince(cmd, nil); err != nil {
		t.Fatalf("runFreshnessServiceChangedSince() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Service changed since gen-prior -> gen-current (service=svc-a)") {
		t.Fatalf("summary missing baseline line: %q", output)
	}
	if !strings.Contains(output, "retired=2 superseded=3") {
		t.Fatalf("summary missing counts: %q", output)
	}
	if !strings.Contains(output, "Truth freshness: stale") {
		t.Fatalf("summary missing freshness: %q", output)
	}
}
