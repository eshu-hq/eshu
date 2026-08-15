// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// These tests cover what this file owns: cobra flags reaching
// internal/cli/report, the package's results reaching the right stream or file,
// and errors reaching the right exit code. The bundle's content and its
// redaction rules are proven in internal/cli/report's own tests, against the
// same production functions these commands call.

// envelopeServer returns a canned query.ResponseEnvelope carrying a truth
// envelope and a citation with an inline excerpt, so the wiring assertions run
// against realistic bytes.
func envelopeServer(t *testing.T, wantPath string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantPath != "" && r.URL.Path != wantPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
		}
		w.Header().Set("Content-Type", "application/eshu.envelope+json")
		_, _ = w.Write([]byte(`{
			"data": {
				"owner": "platform-team",
				"truncated": true,
				"citations": [{"repo_id": "demo/service", "relative_path": "main.go", "excerpt": "func Handler() { return nil }"}]
			},
			"truth": {"level": "exact", "profile": "local_authoritative", "backend": "nornicdb"},
			"error": null
		}`))
	}))
}

// newCaptureCmd builds a capture command wired to buffers, matching how the
// real command resolves its streams.
func newCaptureCmd(t *testing.T, serverURL string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	addReportCaptureFlags(cmd)
	addRemoteFlags(cmd)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	mustSetFlag(t, cmd, "service-url", serverURL)
	return cmd, out, errOut
}

// TestReportCapture_WritesBundleToStdout proves the flags reach the capture
// path through the production APIClient and the bundle lands on stdout.
func TestReportCapture_WritesBundleToStdout(t *testing.T) {
	t.Parallel()

	server := envelopeServer(t, "/api/v0/services/checkout/story")
	defer server.Close()

	cmd, out, _ := newCaptureCmd(t, server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "params", `{"repo":"demo/service","api_key":"sk-live-should-not-leak"}`)
	mustSetFlag(t, cmd, "note", "expected the owning team, got an empty list")

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}

	var bundle reportbundle.Bundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("decode captured bundle: %v\noutput: %s", err, out.String())
	}
	if bundle.Response.Truth == nil || bundle.Response.Truth.Backend != "nornicdb" {
		t.Errorf("Response.Truth = %+v, want the server's verbatim truth envelope", bundle.Response.Truth)
	}
	if !bundle.Response.Truncated {
		t.Errorf("Response.Truncated = false, want the observed read-model flag")
	}
	if strings.Contains(out.String(), "sk-live-should-not-leak") {
		t.Errorf("captured bundle leaks the api_key sentinel value:\n%s", out.String())
	}
}

// TestReportCapture_WritesBundleToOutPath proves --out diverts the bytes to a
// file, owner-only, and leaves stdout empty.
func TestReportCapture_WritesBundleToOutPath(t *testing.T) {
	t.Parallel()

	server := envelopeServer(t, "")
	defer server.Close()

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd, out, _ := newCaptureCmd(t, server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "out", outPath)

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing written when --out is set", out.String())
	}
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat --out bundle: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("--out bundle mode = %04o, want 0600", got)
	}
	raw, err := os.ReadFile(outPath) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("read --out bundle: %v", err)
	}
	var bundle reportbundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode --out bundle: %v\ncontent: %s", err, raw)
	}
}

// TestReportCapture_RequiresEndpoint pins the usage exit code for the one flag
// check this wrapper still owns.
func TestReportCapture_RequiresEndpoint(t *testing.T) {
	t.Parallel()

	cmd, _, _ := newCaptureCmd(t, "http://127.0.0.1:1")
	mustSetFlag(t, cmd, "endpoint", "   ")

	err := runReportCapture(cmd, nil)
	var exitErr commandExitError
	if !asCommandExitError(err, &exitErr) {
		t.Fatalf("runReportCapture() error = %#v, want commandExitError", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.ExitCode())
	}
}

// TestReportCapture_TargetCredentialTakesUsageExitCode proves the wrapper maps
// report.TargetCredentialError to the usage exit code rather than the generic
// one, and that nothing carrying the credential reaches a stream.
func TestReportCapture_TargetCredentialTakesUsageExitCode(t *testing.T) {
	t.Parallel()

	const sentinel = "ESHU6059SENTINELc47e09"

	server := envelopeServer(t, "")
	defer server.Close()

	cmd, out, errOut := newCaptureCmd(t, server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "tool", "https://svc:"+sentinel+"@mcp.internal:5432/tool")

	err := runReportCapture(cmd, nil)
	var exitErr commandExitError
	if !asCommandExitError(err, &exitErr) {
		t.Fatalf("runReportCapture() error = %#v, want commandExitError", err)
	}
	if exitErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", exitErr.ExitCode())
	}
	egress := err.Error() + "\n" + out.String() + "\n" + errOut.String()
	if strings.Contains(egress, sentinel) {
		t.Errorf("credential sentinel reached the operator:\n%s", egress)
	}
	if !strings.Contains(err.Error(), "--tool") {
		t.Errorf("error = %q, want it to name the flag the reporter must fix", err.Error())
	}
}

// TestReportCapture_IncludePayloadsWarnsOnStderr proves the loud warning goes
// to stderr, not into the bundle on stdout.
func TestReportCapture_IncludePayloadsWarnsOnStderr(t *testing.T) {
	t.Parallel()

	server := envelopeServer(t, "")
	defer server.Close()

	cmd, out, errOut := newCaptureCmd(t, server.URL)
	mustSetFlag(t, cmd, "endpoint", "/api/v0/services/checkout/story")
	mustSetFlag(t, cmd, "include-payloads", "true")

	if err := runReportCapture(cmd, nil); err != nil {
		t.Fatalf("runReportCapture() error = %v, want nil", err)
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "private") {
		t.Fatalf("stderr = %q, want a loud private-triage-only warning", errOut.String())
	}
	var bundle reportbundle.Bundle
	if err := json.Unmarshal(out.Bytes(), &bundle); err != nil {
		t.Fatalf("decode captured bundle: %v", err)
	}
	if bundle.Redaction.Profile != reportbundle.ProfilePrivateTriage {
		t.Fatalf("Redaction.Profile = %q, want %q", bundle.Redaction.Profile, reportbundle.ProfilePrivateTriage)
	}
}

// TestReportValidate_ReadsStdinAndPrintsVerdict proves the validate wrapper
// wires cobra's stdin and stdout into the package, on both outcomes.
func TestReportValidate_ReadsStdinAndPrintsVerdict(t *testing.T) {
	t.Parallel()

	publicBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface: "api",
		Target:  "/api/v0/services/checkout/story",
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	privateBundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface:         "api",
		Target:          "/api/v0/services/checkout/story",
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}

	tests := []struct {
		name        string
		bundle      reportbundle.Bundle
		wantErr     bool
		wantVerdict string
	}{
		{name: "public bundle passes --require-public", bundle: publicBundle, wantErr: false, wantVerdict: "report bundle validation: passed"},
		{name: "private-triage bundle fails --require-public", bundle: privateBundle, wantErr: true, wantVerdict: "report bundle validation: failed"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(tt.bundle)
			if err != nil {
				t.Fatalf("marshal bundle: %v", err)
			}

			cmd := &cobra.Command{}
			addReportValidateFlags(cmd)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetIn(bytes.NewReader(raw))
			mustSetFlag(t, cmd, "require-public", "true")

			err = runReportValidate(cmd, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("runReportValidate() error = %v, wantErr %v (output: %s)", err, tt.wantErr, out.String())
			}
			if !strings.Contains(out.String(), tt.wantVerdict) {
				t.Errorf("verdict = %q, want %q", out.String(), tt.wantVerdict)
			}
		})
	}
}

// TestReportValidate_ReadsFromPath proves --from wins over stdin.
func TestReportValidate_ReadsFromPath(t *testing.T) {
	t.Parallel()

	bundle, err := reportbundle.Capture(reportbundle.CaptureInput{Surface: "api", Target: "/api/v0/x"})
	if err != nil {
		t.Fatalf("Capture() error = %v, want nil", err)
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := &cobra.Command{}
	addReportValidateFlags(cmd)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("this stdin must not be read"))
	mustSetFlag(t, cmd, "from", path)

	if err := runReportValidate(cmd, nil); err != nil {
		t.Fatalf("runReportValidate() error = %v, want nil (output: %s)", err, out.String())
	}
	if !strings.Contains(out.String(), "report bundle validation: passed") {
		t.Errorf("verdict = %q, want the passed line", out.String())
	}
}

func mustSetFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("Set(%q, %q) error = %v", name, value, err)
	}
}

// asCommandExitError is errors.As for the value-typed commandExitError, kept
// here so the two exit-code assertions above read the same way.
func asCommandExitError(err error, target *commandExitError) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(commandExitError) //nolint:errorlint // commandExitError is returned as a value, never wrapped
	if !ok {
		return false
	}
	*target = typed
	return true
}
