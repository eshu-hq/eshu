// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// End-to-end share-safety sentinel for `eshu evidence bundle export`.
//
// The sentinel is planted INSIDE A VALUE the status routes return -- never
// under a sensitive-looking key -- and the assertions read the bytes the
// command actually rendered: the JSON written to --out and the JSON written
// to stdout. A canary keyed on field names cannot see a leak of this shape,
// because the leaking string arrives as a value in an ordinary field.
//
// The absence assertions are only meaningful because
// TestEvidenceBundleCarrierValuesReachTheArtifact proves every carrier field
// really does reach the artifact when its value is benign. A carrier the
// command silently dropped would make the matching absence assertion pass
// while guarding nothing.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// shareSafetyMarker is the token searched for in every rendered byte. It
	// is deliberately neither a key name nor a word a redactor keys on.
	shareSafetyMarker = "Q7PROBE6059Z"
	// shareSafetyBenign is the positive control: a value with no sensitive
	// shape, which SHOULD survive into the artifact.
	shareSafetyBenign = "benign" + shareSafetyMarker + "value"
)

// shareSafetySensitiveValues are the sensitive shapes planted inside a
// carrier value. Each embeds shareSafetyMarker so one search covers them all.
func shareSafetySensitiveValues() map[string]string {
	return map[string]string{
		"credential_url":  "https://svc:" + shareSafetyMarker + "@db.internal:5432/health",
		"private_address": "instance 10.42.7.9 unreachable " + shareSafetyMarker,
		"local_path":      "config at /Users/ops/" + shareSafetyMarker + "/eshu.yaml",
		"github_token":    "ghp_" + shareSafetyMarker + "ABCDEFGH rejected",
		// A colon immediately before the host, which is what a labelled
		// diagnostic writes and what both private-host rules used to let
		// through.
		"colon_prefixed_host": "upstream:db.internal:5432 refused " + shareSafetyMarker,
		"colon_prefixed_addr": "peer:10.42.7.9 unreachable " + shareSafetyMarker,
	}
}

// shareSafetyCarriers names every free-text or identifier string the live
// export copies out of the three status routes and into the bundle.
func shareSafetyCarriers() []string {
	return []string{
		"semantic_reason", "semantic_state", "profile_id", "profile_kind",
		"profile_state", "profile_reason", "health_state", "health_reason",
		"stage", "domain", "collector_kind", "collector_status", "collector_health",
	}
}

// shareSafetyStatusBodies renders the three status-route bodies with `value`
// substituted into the named carrier field.
func shareSafetyStatusBodies(t *testing.T, carrier, value string) (index, pipeline, collectors string) {
	t.Helper()
	f := map[string]string{
		"semantic_reason":  "provider_not_configured",
		"semantic_state":   "unavailable",
		"profile_id":       "default",
		"profile_kind":     "openai_compatible",
		"profile_state":    "unavailable",
		"profile_reason":   "not_configured",
		"health_state":     "degraded",
		"health_reason":    "queue backlog",
		"stage":            "parse",
		"domain":           "aws_relationship_materialization",
		"collector_kind":   "git",
		"collector_status": "ready",
		"collector_health": "healthy",
	}
	if _, ok := f[carrier]; !ok {
		t.Fatalf("unknown share-safety carrier %q", carrier)
	}
	f[carrier] = value

	index = fmt.Sprintf(`{
		"repository_count": 5,
		"queue_blockages": [{"stage": "reduce", "domain": "code", "blocked": 3}],
		"semantic_extraction": {
			"state": %q,
			"reason": %q,
			"provider_configured": true,
			"provider_profiles": [{"profile_id": %q, "provider_kind": %q, "state": %q, "reason": %q}]
		}
	}`, f["semantic_state"], f["semantic_reason"], f["profile_id"], f["profile_kind"], f["profile_state"], f["profile_reason"])

	pipeline = fmt.Sprintf(`{
		"health": {"state": %q, "reasons": [%q]},
		"queue": {"total": 18, "outstanding": 7, "pending": 4},
		"generation_history": {"active": 1, "completed": 9},
		"stage_summaries": [{"stage": %q, "pending": 2, "succeeded": 11}],
		"domain_backlogs": [{"domain": %q, "outstanding": 1, "blocked": 9}],
		"scope_activity": {"active": 5, "changed": 1, "unchanged": 4}
	}`, f["health_state"], f["health_reason"], f["stage"], f["domain"])

	collectors = fmt.Sprintf(`{"collectors": [{"collector_kind": %q, "status_category": %q, "health": %q}]}`,
		f["collector_kind"], f["collector_status"], f["collector_health"])
	return index, pipeline, collectors
}

// shareSafetyServer serves the three status routes from fixed bodies.
func shareSafetyServer(t *testing.T, index, pipeline, collectors string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/status/index":
			_, _ = w.Write([]byte(index))
		case "/api/v0/status/pipeline":
			_, _ = w.Write([]byte(pipeline))
		case "/api/v0/status/collectors":
			_, _ = w.Write([]byte(collectors))
		default:
			// t.Fatal* is illegal off the test goroutine.
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// shareSafetyRunLiveExport runs `evidence bundle export --live` twice against
// the same stub: once writing to --out, once to stdout. It returns the bytes
// of both renderings plus each command's error.
func shareSafetyRunLiveExport(t *testing.T, serverURL string) (fileBytes, stdoutBytes []byte, fileErr, stdoutErr error) {
	t.Helper()

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	fileCmd := newEvidenceBundleExportCommand()
	fileCmd.SetOut(&bytes.Buffer{})
	fileCmd.SetErr(&bytes.Buffer{})
	fileCmd.SetArgs([]string{"--live", "--service-url", serverURL, "--out", outPath})
	fileErr = fileCmd.Execute()
	if raw, err := os.ReadFile(outPath); err == nil {
		fileBytes = raw
	}

	var stdout bytes.Buffer
	stdoutCmd := newEvidenceBundleExportCommand()
	stdoutCmd.SetOut(&stdout)
	stdoutCmd.SetErr(&bytes.Buffer{})
	stdoutCmd.SetArgs([]string{"--live", "--service-url", serverURL})
	stdoutErr = stdoutCmd.Execute()
	stdoutBytes = stdout.Bytes()

	return fileBytes, stdoutBytes, fileErr, stdoutErr
}

// TestEvidenceBundleCarrierValuesReachTheArtifact is the positive control: a
// benign marker planted in each carrier MUST appear in both renderings.
func TestEvidenceBundleCarrierValuesReachTheArtifact(t *testing.T) {
	for _, carrier := range shareSafetyCarriers() {
		t.Run(carrier, func(t *testing.T) {
			index, pipeline, collectors := shareSafetyStatusBodies(t, carrier, shareSafetyBenign)
			server := shareSafetyServer(t, index, pipeline, collectors)
			fileBytes, stdoutBytes, fileErr, stdoutErr := shareSafetyRunLiveExport(t, server.URL)
			if fileErr != nil {
				t.Fatalf("--out export failed for benign carrier %s: %v", carrier, fileErr)
			}
			if stdoutErr != nil {
				t.Fatalf("stdout export failed for benign carrier %s: %v", carrier, stdoutErr)
			}
			if len(fileBytes) == 0 || len(stdoutBytes) == 0 {
				t.Fatalf("harness failure: empty rendering (file=%d stdout=%d bytes)", len(fileBytes), len(stdoutBytes))
			}
			if !bytes.Contains(fileBytes, []byte(shareSafetyBenign)) {
				t.Fatalf("carrier %s never reaches the --out artifact; the absence assertion on it would be vacuous\n%s", carrier, fileBytes)
			}
			if !bytes.Contains(stdoutBytes, []byte(shareSafetyBenign)) {
				t.Fatalf("carrier %s never reaches the stdout artifact\n%s", carrier, stdoutBytes)
			}
		})
	}
}

// TestEvidenceBundleSensitiveValuesNeverReachTheArtifact plants a
// sensitive-shaped value inside each carrier and asserts the marker appears
// in no rendered byte.
func TestEvidenceBundleSensitiveValuesNeverReachTheArtifact(t *testing.T) {
	for _, carrier := range shareSafetyCarriers() {
		for shape, value := range shareSafetySensitiveValues() {
			t.Run(carrier+"/"+shape, func(t *testing.T) {
				index, pipeline, collectors := shareSafetyStatusBodies(t, carrier, value)
				server := shareSafetyServer(t, index, pipeline, collectors)
				fileBytes, stdoutBytes, _, _ := shareSafetyRunLiveExport(t, server.URL)
				if bytes.Contains(fileBytes, []byte(shareSafetyMarker)) {
					t.Fatalf("sentinel reached the --out artifact through carrier %s (%s):\n%s", carrier, shape, fileBytes)
				}
				if bytes.Contains(stdoutBytes, []byte(shareSafetyMarker)) {
					t.Fatalf("sentinel reached the stdout artifact through carrier %s (%s):\n%s", carrier, shape, stdoutBytes)
				}
			})
		}
	}
}

// TestEvidenceBundleScopeFlagSensitiveValueNeverReachesTheArtifact covers the
// one caller-supplied string the demo path stamps straight into the bundle:
// --scope lands in Identity.ScopeID and in every reproduce call's repo_id
// argument.
func TestEvidenceBundleScopeFlagSensitiveValueNeverReachesTheArtifact(t *testing.T) {
	exportScope := func(t *testing.T, scope string) ([]byte, error) {
		t.Helper()
		outPath := filepath.Join(t.TempDir(), "bundle.json")
		cmd := newEvidenceBundleExportCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--scope", scope, "--out", outPath})
		err := cmd.Execute()
		raw, readErr := os.ReadFile(outPath)
		if readErr != nil {
			raw = nil
		}
		return raw, err
	}

	for shape, value := range shareSafetySensitiveValues() {
		t.Run(shape, func(t *testing.T) {
			raw, _ := exportScope(t, "repo:"+value)
			if bytes.Contains(raw, []byte(shareSafetyMarker)) {
				t.Fatalf("sentinel reached the demo bundle through --scope (%s):\n%s", shape, raw)
			}
		})
	}

	t.Run("benign_control", func(t *testing.T) {
		raw, err := exportScope(t, "repo:"+shareSafetyBenign)
		if err != nil {
			t.Fatalf("benign --scope export failed: %v", err)
		}
		if len(raw) == 0 {
			t.Fatal("harness failure: benign --scope export wrote no bytes")
		}
		if !bytes.Contains(raw, []byte(shareSafetyBenign)) {
			t.Fatalf("--scope never reaches the artifact; the absence assertions above would be vacuous\n%s", raw)
		}
	})
}

// TestEvidenceBundleLiveExportMarksATruncatedDomainList runs the whole command
// against a status route that reports its domain list was capped, and reads
// the bounds back out of the file. A bundle that shows a partial enumeration
// as complete is a wrong answer about the stack, and the API route
// (internal/query/evidence_bundle_live.go) has always carried this flag.
func TestEvidenceBundleLiveExportMarksATruncatedDomainList(t *testing.T) {
	index := `{"repository_count": 5, "semantic_extraction": {"state": "unavailable", "reason": "provider_not_configured"}}`
	pipeline := `{
		"health": {"state": "degraded", "reasons": ["queue backlog"]},
		"queue": {"total": 18, "outstanding": 7},
		"domain_backlogs": [{"domain": "aws_relationship_materialization", "outstanding": 1}],
		"domain_backlogs_truncated": true
	}`
	collectors := `{"collectors": [{"collector_kind": "git", "status_category": "ready", "health": "healthy"}]}`
	server := shareSafetyServer(t, index, pipeline, collectors)

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--service-url", server.URL, "--out", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evidence bundle export --live error = %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle struct {
		Bounds struct {
			Truncated       bool     `json:"truncated"`
			TruncatedLayers []string `json:"truncated_layers"`
		} `json:"bounds"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	if !bundle.Bounds.Truncated {
		t.Fatalf("bounds.truncated = false for a capped domain list; the artifact presents it as complete\n%s", raw)
	}
	if !strings.Contains(strings.Join(bundle.Bounds.TruncatedLayers, ","), "domain_backlogs") {
		t.Fatalf("bounds.truncated_layers = %v, want it to name domain_backlogs", bundle.Bounds.TruncatedLayers)
	}
}

// TestEvidenceBundleLiveExportLeavesACompleteDomainListUnmarked is the paired
// negative: without the flag the bundle must NOT claim truncation, or the
// marker means nothing.
func TestEvidenceBundleLiveExportLeavesACompleteDomainListUnmarked(t *testing.T) {
	index := `{"repository_count": 5, "semantic_extraction": {"state": "unavailable", "reason": "provider_not_configured"}}`
	pipeline := `{
		"health": {"state": "degraded", "reasons": ["queue backlog"]},
		"queue": {"total": 18, "outstanding": 7},
		"domain_backlogs": [{"domain": "aws_relationship_materialization", "outstanding": 1}]
	}`
	collectors := `{"collectors": [{"collector_kind": "git", "status_category": "ready", "health": "healthy"}]}`
	server := shareSafetyServer(t, index, pipeline, collectors)

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--service-url", server.URL, "--out", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evidence bundle export --live error = %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle struct {
		Bounds struct {
			Truncated bool `json:"truncated"`
		} `json:"bounds"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	if bundle.Bounds.Truncated {
		t.Fatalf("bounds.truncated = true for a complete domain list\n%s", raw)
	}
}
