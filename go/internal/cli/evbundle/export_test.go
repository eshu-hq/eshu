// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

var fixedCreatedAt = time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)

func decodeBundle(t *testing.T, raw []byte) evidencebundle.Bundle {
	t.Helper()
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	return bundle
}

func TestExportDemoRendersAValidatedBundle(t *testing.T) {
	raw, err := ExportDemo("repo:demo/service")
	if err != nil {
		t.Fatalf("ExportDemo() error = %v", err)
	}
	bundle := decodeBundle(t, raw)
	if bundle.SchemaVersion != evidencebundle.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", bundle.SchemaVersion, evidencebundle.SchemaVersion)
	}
	if bundle.Identity.ScopeID != "repo:demo/service" {
		t.Fatalf("ScopeID = %q", bundle.Identity.ScopeID)
	}
	if bundle.Validation.Status != "passed" {
		t.Fatalf("Validation.Status = %q, want passed once Validate returned nil", bundle.Validation.Status)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported bundle does not re-validate: %v", err)
	}
}

// TestExportDemoRefusesAScopeCarryingPrivateData covers the one
// caller-supplied string the demo path stamps into the artifact. The scope
// handle is content -- it lands in Identity.ScopeID and in every reproduce
// call's repo_id -- so a path or credential in it must abort the export, not
// ride along beside correctly-redacted fields.
func TestExportDemoRefusesAScopeCarryingPrivateData(t *testing.T) {
	for name, scope := range map[string]string{
		"local_path":     "repo:/Users/example/private/repo",
		"credential_url": "repo:https://svc:hunter2@example.com/x",
		// The "repo:" prefix puts a colon immediately before the host, which
		// is the delimiter that used to slip past both private-host rules.
		"private_host":    "repo:db.internal:5432",
		"private_address": "repo:10.0.5.3",
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := ExportDemo(scope)
			if err == nil {
				t.Fatalf("ExportDemo(%q) error = nil, want a refusal\n%s", scope, raw)
			}
			if raw != nil {
				t.Fatalf("ExportDemo returned %d bytes alongside its error; a refused export must render nothing", len(raw))
			}
			if !strings.Contains(err.Error(), "validate generated evidence bundle") {
				t.Fatalf("error = %v, want it to name the validation step", err)
			}
		})
	}
}

func TestExportLiveComposesAStackWideBundle(t *testing.T) {
	raw, err := ExportLive(&stubFetcher{bodies: fullStatusBodies()}, "", fixedCreatedAt)
	if err != nil {
		t.Fatalf("ExportLive() error = %v", err)
	}
	bundle := decodeBundle(t, raw)
	if bundle.Identity.ScopeID != "live:local" {
		t.Fatalf("ScopeID = %q, want the stack-wide default", bundle.Identity.ScopeID)
	}
	if bundle.Identity.CreatedAt != fixedCreatedAt.Format(time.RFC3339) {
		t.Fatalf("CreatedAt = %q, want the caller-supplied fetch time", bundle.Identity.CreatedAt)
	}
	if bundle.Validation.Status != "passed" {
		t.Fatalf("Validation.Status = %q, want passed", bundle.Validation.Status)
	}
	if bundle.Contents.PipelineState == nil || bundle.Contents.PipelineState.RepositoryCount != 5 {
		t.Fatalf("PipelineState = %+v, want it populated from the status routes", bundle.Contents.PipelineState)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported live bundle does not re-validate: %v", err)
	}
}

func TestExportLiveFailsWhenAStatusRouteFails(t *testing.T) {
	sentinel := errors.New("status reader not configured")
	raw, err := ExportLive(&stubFetcher{
		bodies: fullStatusBodies(),
		errs:   map[string]error{PipelineEndpoint: sentinel},
	}, "", fixedCreatedAt)
	if err == nil {
		t.Fatalf("ExportLive() error = nil, want a failure\n%s", raw)
	}
	if raw != nil {
		t.Fatalf("ExportLive returned %d bytes alongside its error", len(raw))
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v does not wrap the route error", err)
	}
}

// TestExportLiveRefusesASnapshotCarryingAPrivateEndpoint plants a locating
// address inside a free-text status value -- the shape a name-keyed redactor
// cannot see -- and proves the export aborts instead of rendering it.
func TestExportLiveRefusesASnapshotCarryingAPrivateEndpoint(t *testing.T) {
	bodies := fullStatusBodies()
	bodies[PipelineEndpoint] = `{
		"health": {"state": "degraded", "reasons": ["dial tcp 10.42.7.9:5432: connection refused"]},
		"queue": {"total": 1}
	}`
	raw, err := ExportLive(&stubFetcher{bodies: bodies}, "", fixedCreatedAt)
	if err == nil {
		t.Fatalf("ExportLive() error = nil, want a refusal\n%s", raw)
	}
	if bytes.Contains(raw, []byte("10.42.7.9")) {
		t.Fatalf("the address reached the rendered bytes:\n%s", raw)
	}
	if !strings.Contains(err.Error(), "validate live evidence bundle") {
		t.Fatalf("error = %v, want it to name the validation step", err)
	}
}

func TestWriteBundleWritesTheFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteBundle(&bytes.Buffer{}, []byte("{}\n"), path); err != nil {
		t.Fatalf("WriteBundle() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("bundle mode = %04o, want 0600", perm)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if string(raw) != "{}\n" {
		t.Fatalf("bundle contents = %q", raw)
	}
}

func TestWriteBundleFallsBackToTheWriter(t *testing.T) {
	for name, path := range map[string]string{"empty": "", "whitespace": "   "} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteBundle(&out, []byte("{}\n"), path); err != nil {
				t.Fatalf("WriteBundle() error = %v", err)
			}
			if out.String() != "{}\n" {
				t.Fatalf("writer got %q", out.String())
			}
		})
	}
}

func TestWriteBundleReportsAnUnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "bundle.json")
	err := WriteBundle(&bytes.Buffer{}, []byte("{}\n"), path)
	if err == nil {
		t.Fatal("WriteBundle() error = nil for a path under a missing directory")
	}
	if !strings.Contains(err.Error(), "write evidence bundle") {
		t.Fatalf("error = %v, want it to name the write step", err)
	}
}

func TestReadBundleInputPrefersThePathOverTheReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	raw, err := ReadBundleInput(strings.NewReader(`{"from":"stdin"}`), path)
	if err != nil {
		t.Fatalf("ReadBundleInput() error = %v", err)
	}
	if string(raw) != `{"from":"file"}` {
		t.Fatalf("read %q, want the file contents", raw)
	}
}

func TestReadBundleInputFallsBackToTheReader(t *testing.T) {
	raw, err := ReadBundleInput(strings.NewReader(`{"from":"stdin"}`), "  ")
	if err != nil {
		t.Fatalf("ReadBundleInput() error = %v", err)
	}
	if string(raw) != `{"from":"stdin"}` {
		t.Fatalf("read %q, want the reader contents", raw)
	}
}

func TestReadBundleInputReportsAMissingFile(t *testing.T) {
	_, err := ReadBundleInput(strings.NewReader(""), filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("ReadBundleInput() error = nil for a missing file")
	}
	if !strings.Contains(err.Error(), "read evidence bundle") {
		t.Fatalf("error = %v, want it to name the read step", err)
	}
}

func TestValidateBundlePrintsAPassedVerdict(t *testing.T) {
	raw, err := ExportDemo("repo:demo/service")
	if err != nil {
		t.Fatalf("ExportDemo() error = %v", err)
	}
	var out bytes.Buffer
	if err := ValidateBundle(&out, raw); err != nil {
		t.Fatalf("ValidateBundle() error = %v", err)
	}
	if out.String() != "evidence bundle validation: passed\n" {
		t.Fatalf("verdict = %q", out.String())
	}
}

func TestValidateBundlePrintsAFailedVerdictAndReturnsTheReason(t *testing.T) {
	bundle := evidencebundle.BuildDemoBundle(evidencebundle.DemoBundleOptions{ScopeID: "repo:demo/service"})
	bundle.Source.Repository = "/Users/example/private/repo"
	raw, err := evidencebundle.RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var out bytes.Buffer
	err = ValidateBundle(&out, raw)
	if err == nil {
		t.Fatal("ValidateBundle() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "local absolute path") {
		t.Fatalf("error = %v, want the canary reason", err)
	}
	if out.String() != "evidence bundle validation: failed\n" {
		t.Fatalf("verdict = %q", out.String())
	}
}

func TestValidateBundleReportsUndecodableInput(t *testing.T) {
	var out bytes.Buffer
	err := ValidateBundle(&out, []byte("not json"))
	if err == nil {
		t.Fatal("ValidateBundle() error = nil for undecodable input")
	}
	if !strings.Contains(err.Error(), "decode evidence bundle") {
		t.Fatalf("error = %v, want it to name the decode step", err)
	}
	if out.Len() != 0 {
		t.Fatalf("verdict = %q, want nothing printed before a decode failure", out.String())
	}
}
