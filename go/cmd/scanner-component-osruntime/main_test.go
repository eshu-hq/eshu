// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildBinary compiles the component binary into a temp directory and
// returns its path, following the ci-gates precedent.
func buildBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "scanner-component-osruntime")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v\n%s", err, out)
	}
	return bin
}

func writeRootFS(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("etc/os-release", "ID=alpine\nVERSION_ID=3.19.1\n")
	write("etc/apk/repositories", "https://dl-cdn.alpinelinux.org/alpine/v3.19/main\n")
	write("lib/apk/db/installed", "P:smoke-pkg\nV:1.0.0-r0\nA:x86_64\no:smoke-pkg\n\n")
	return root
}

// smokeStatus mirrors the status record the binary emits, decoded strictly
// like the host decodes it. SourceLatencyMS rides the SDK omitempty wire
// contract (present iff analysis takes >=1ms), so the smoke decode accepts
// both shapes: absent decodes zero, present decodes the value.
type smokeStatus struct {
	Class           string `json:"class"`
	FactCount       int    `json:"fact_count"`
	SourceLatencyMS int    `json:"source_latency_ms,omitempty"`
}

// smokePage mirrors the result page document the binary writes to stdout.
type smokePage struct {
	ProtocolVersion string           `json:"protocol_version"`
	State           string           `json:"state"`
	Claim           map[string]any   `json:"claim"`
	Generation      map[string]any   `json:"generation"`
	Facts           []map[string]any `json:"facts"`
	Statuses        []smokeStatus    `json:"statuses"`
	Page            *struct {
		PageIndex  int  `json:"page_index"`
		TotalFacts int  `json:"total_facts"`
		More       bool `json:"more"`
	} `json:"page"`
}

// TestStatusDecodesSourceLatencyBothStates pins the smoke decode against
// the SDK wire contract: Status.SourceLatencyMS is omitempty, so slow runs
// carry source_latency_ms while 0ms runs omit it. Both shapes must decode
// under DisallowUnknownFields.
func TestStatusDecodesSourceLatencyBothStates(t *testing.T) {
	t.Parallel()

	decode := func(statusDoc string) smokePage {
		raw := `{"protocol_version":"collector-sdk/v1alpha3","state":"complete",` +
			`"statuses":[` + statusDoc + `],` +
			`"page":{"page_index":0,"total_facts":0,"more":false}}`
		var page smokePage
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&page); err != nil {
			t.Fatalf("decode page: %v\ndoc = %s", err, raw)
		}
		return page
	}

	absent := decode(`{"class":"complete","fact_count":2}`)
	if len(absent.Statuses) != 1 || absent.Statuses[0].Class != "complete" {
		t.Fatalf("statuses = %+v, want one complete status", absent.Statuses)
	}
	if got := absent.Statuses[0].SourceLatencyMS; got != 0 {
		t.Fatalf("SourceLatencyMS = %d without the field, want zero", got)
	}
	present := decode(`{"class":"complete","fact_count":2,"source_latency_ms":7}`)
	if len(present.Statuses) != 1 || present.Statuses[0].Class != "complete" {
		t.Fatalf("statuses = %+v, want one complete status", present.Statuses)
	}
	if got := present.Statuses[0].SourceLatencyMS; got != 7 {
		t.Fatalf("SourceLatencyMS = %d with the field, want 7", got)
	}
}

// TestBinaryServesOnePageOverStdio builds the real binary and drives it
// through a real subprocess: one request on stdin yields exactly one JSON
// result document on stdout with a v1alpha3 page envelope, and stderr
// carries only the structured log line.
func TestBinaryServesOnePageOverStdio(t *testing.T) {
	bin := buildBinary(t)

	scopeID := "image://registry.example/smoke@sha256:deadbeef"
	root := writeRootFS(t)
	request := map[string]any{
		"protocol_version": "collector-sdk/v1alpha3",
		"claim": map[string]any{
			"component_id":   "eshu.scanner.osruntime",
			"instance_id":    "smoke",
			"collector_kind": "scanner_worker",
			"source_system":  "scanner_worker",
			"scope":          map[string]any{"id": scopeID, "kind": "image"},
			"source_run_id":  "run-1",
			"generation_id":  "gen-1",
			"work_item_id":   "work-1",
			"fencing_token":  "7",
			"attempt":        1,
			"deadline":       time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano),
			"config_handle":  "cfg-smoke",
		},
		"config": map[string]any{
			"targets": []any{map[string]any{
				"scope_id":         scopeID,
				"rootfs_path":      root,
				"source_uri":       "oci://registry.example/smoke@sha256:deadbeef",
				"source_record_id": "sha256:deadbeef",
			}},
			"acceptance_unit_id": "image:sha256:deadbeef",
		},
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary exit = %v, stderr = %q", err, stderr.String())
	}
	var page smokePage
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&page); err != nil {
		t.Fatalf("decode page: %v\nstdout = %q", err, stdout.String())
	}
	if page.ProtocolVersion != "collector-sdk/v1alpha3" {
		t.Fatalf("protocol = %q, want v1alpha3", page.ProtocolVersion)
	}
	if page.State != "complete" {
		t.Fatalf("state = %q, want complete: %q", page.State, stdout.String())
	}
	if len(page.Facts) == 0 {
		t.Fatal("facts = 0, want image evidence")
	}
	analysis := false
	for _, fact := range page.Facts {
		if fact["kind"] == "vulnerability.os_package" {
			analysis = true
		}
	}
	if !analysis {
		t.Fatalf("no vulnerability.os_package fact, want the package evidence path: %+v", page.Facts)
	}
	if len(page.Statuses) != 1 || page.Statuses[0].Class != "complete" {
		t.Fatalf("statuses = %+v, want one complete status", page.Statuses)
	}
	if page.Page == nil || page.Page.TotalFacts != len(page.Facts) {
		t.Fatalf("page envelope = %+v, want declared total", page.Page)
	}
	if stderr.String() == "" {
		t.Fatal("stderr empty, want the structured log line")
	}
	for _, leak := range []string{root, "registry.example"} {
		if strings.Contains(stderr.String(), leak) {
			t.Fatalf("stderr leaked target material %q", leak)
		}
	}
	if got := strings.Count(strings.TrimSpace(stdout.String()), "\n"); got != 0 {
		t.Fatalf("stdout has %d extra lines, want exactly one document", got)
	}
}
