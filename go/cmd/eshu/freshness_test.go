// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

// The command logic these tests used to cover now lives in
// internal/cli/freshness and is tested there. What is left here is the wrapper
// contract: the commands are registered with the flags operators type, the
// flag values reach the options struct unchanged, and a freshness.Failure
// becomes the CLI's exit-code error.

func newTestFreshnessGenerationsCommand() *cobra.Command {
	cmd := &cobra.Command{}
	addFreshnessGenerationsFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

// freshnessTestServer serves one canned response for every freshness route, so
// a wrapper test can drive the real RunE path end to end through --service-url.
func freshnessTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFreshnessGenerationsCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"freshness", "generations"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness generations) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "generations" {
		t.Fatalf("resolved command = %#v, want generations", cmd)
	}
	for _, name := range []string{"json", "scope-id", "repository", "collector-kind", "source-system", "generation-id", "status", "limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness generations flag %q missing", name)
		}
	}
}

// TestFreshnessGenerationsOptionsCarryEveryFlag proves the wrapper reads every
// flag it declares. A flag added to addFreshnessGenerationsFlags but never read
// would leave its field at the zero value here.
func TestFreshnessGenerationsOptionsCarryEveryFlag(t *testing.T) {
	cmd := newTestFreshnessGenerationsCommand()
	for name, value := range map[string]string{
		"json": "true", "scope-id": "s", "repository": "r", "collector-kind": "git",
		"source-system": "github", "generation-id": "g", "status": "active", "limit": "7",
	} {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	opts, err := freshnessGenerationsOptionsFromCommand(cmd)
	if err != nil {
		t.Fatalf("freshnessGenerationsOptionsFromCommand() error = %v", err)
	}
	want := freshness.GenerationsOptions{
		JSON: true, ScopeID: "s", Repository: "r", CollectorKind: "git",
		SourceSystem: "github", GenerationID: "g", Status: "active", Limit: 7,
	}
	if opts != want {
		t.Fatalf("options = %#v, want %#v", opts, want)
	}
}

func TestRunFreshnessGenerationsRendersSummary(t *testing.T) {
	server := freshnessTestServer(t, http.StatusOK,
		`{"data":{"count":1,"truncated":false,"generations":[`+
			`{"generation_id":"gen-active","status":"active","scope_id":"git-repository-scope:acme/app",`+
			`"trigger_kind":"snapshot","is_active":true,`+
			`"queue_status":{"outstanding":0,"failed":0,"dead_letter":0}}]},`+
			`"truth":{"freshness":{"state":"fresh"}},"error":null}`)

	out := &bytes.Buffer{}
	cmd := newTestFreshnessGenerationsCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	if err := runFreshnessGenerations(cmd, nil); err != nil {
		t.Fatalf("runFreshnessGenerations() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "gen-active") || !strings.Contains(output, "status=active") {
		t.Fatalf("summary missing generation row: %q", output)
	}
	if !strings.Contains(output, "Truth freshness: fresh") {
		t.Fatalf("summary missing freshness: %q", output)
	}
}

// TestRunFreshnessGenerationsMapsFailureToTheExitContract is the wrapper's own
// job: internal/cli/freshness picks the number, and this proves the number
// survives the conversion into the type main's exit path reads.
func TestRunFreshnessGenerationsMapsFailureToTheExitContract(t *testing.T) {
	server := freshnessTestServer(t, http.StatusOK,
		`{"data":null,"truth":null,"error":{"code":"scope_not_found","message":"no records for scope"}}`)

	out := &bytes.Buffer{}
	cmd := newTestFreshnessGenerationsCommand()
	cmd.SetOut(out)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	err := runFreshnessGenerations(cmd, nil)
	if err == nil {
		t.Fatal("runFreshnessGenerations() error = nil, want not-found exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 2; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
	if got, want := exitErr.Error(), "no records for scope"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestFreshnessExitErrorPassesOtherErrorsThrough proves the conversion is
// narrow: only a *freshness.Failure becomes a commandExitError, so an unrelated
// error keeps the default exit 1 rather than picking up a borrowed code.
func TestFreshnessExitErrorPassesOtherErrorsThrough(t *testing.T) {
	if got := freshnessExitError(nil); got != nil {
		t.Fatalf("freshnessExitError(nil) = %v, want nil", got)
	}
	plain := errors.New("disk full")
	if got := freshnessExitError(plain); !errors.Is(got, plain) {
		t.Fatalf("freshnessExitError(plain) = %v, want the same error back", got)
	}
	converted := freshnessExitError(&freshness.Failure{Message: "capability off", Code: 6})
	var exitErr commandExitError
	if !errors.As(converted, &exitErr) {
		t.Fatalf("converted = %T, want commandExitError", converted)
	}
	if got := exitErr.ExitCode(); got != 6 {
		t.Fatalf("ExitCode() = %d, want 6", got)
	}
}
