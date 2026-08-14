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

func TestFreshnessChangedSinceCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"freshness", "changed-since"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness changed-since) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "changed-since" {
		t.Fatalf("resolved command = %#v, want changed-since", cmd)
	}
	for _, name := range []string{"json", "scope-id", "repository", "since-generation-id", "since-observed-at", "sample-limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness changed-since flag %q missing", name)
		}
	}
}

func TestFreshnessChangedSinceOptionsCarryEveryFlag(t *testing.T) {
	cmd := newFreshnessChangedSinceCommand()
	for name, value := range map[string]string{
		"json": "true", "scope-id": "s", "repository": "r",
		"since-generation-id": "gen-prior", "since-observed-at": "2026-01-02T03:04:05Z",
		"sample-limit": "40",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	opts, err := freshnessChangedSinceOptionsFromCommand(cmd)
	if err != nil {
		t.Fatalf("freshnessChangedSinceOptionsFromCommand() error = %v", err)
	}
	want := freshness.ChangedSinceOptions{
		JSON: true, ScopeID: "s", Repository: "r",
		SinceGenerationID: "gen-prior", SinceObservedAt: "2026-01-02T03:04:05Z", SampleLimit: 40,
	}
	if opts != want {
		t.Fatalf("options = %#v, want %#v", opts, want)
	}
}

func TestRunFreshnessChangedSinceRendersSummary(t *testing.T) {
	server := freshnessTestServer(t, http.StatusOK,
		`{"data":{"scope_id":"git-repository-scope:acme/app","since_generation_id":"gen-prior",`+
			`"current_active_generation_id":"gen-current","unavailable":false,"categories":[`+
			`{"category":"files","unavailable":false,"counts":{"added":2,"updated":1,"unchanged":5,`+
			`"retired":1,"superseded":1}}]},"truth":{"freshness":{"state":"fresh"}},"error":null}`)

	out := &bytes.Buffer{}
	cmd := newFreshnessChangedSinceCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	if err := runFreshnessChangedSince(cmd, nil); err != nil {
		t.Fatalf("runFreshnessChangedSince() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "gen-prior -> gen-current") {
		t.Fatalf("summary missing baseline line: %q", output)
	}
	if !strings.Contains(output, "retired=1 superseded=1") {
		t.Fatalf("summary missing retired/superseded counts: %q", output)
	}
	if !strings.Contains(output, "Truth freshness: fresh") {
		t.Fatalf("summary missing freshness: %q", output)
	}
}
