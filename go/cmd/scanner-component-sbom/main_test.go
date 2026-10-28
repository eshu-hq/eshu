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

	bin := filepath.Join(t.TempDir(), "scanner-component-sbom")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v\n%s", err, out)
	}
	return bin
}

func writeSBOMRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("package-lock.json", `{
		"lockfileVersion": 3,
		"packages": {
			"": {"name": "smoke-app", "version": "1.0.0"},
			"node_modules/left-pad": {"version": "1.3.0"}
		}
	}`)
	write("go.mod", "module example.test/smoke\n\nrequire github.com/gin-gonic/gin v1.9.1\n")
	return root
}

// TestBinaryServesOnePageOverStdio builds the real binary and drives it
// through a real subprocess: one request on stdin yields exactly one JSON
// result document on stdout with a v1alpha3 page envelope, and stderr
// carries only the structured log line.
func TestBinaryServesOnePageOverStdio(t *testing.T) {
	bin := buildBinary(t)

	scopeID := "scanner-worker://repository/smoke"
	root := writeSBOMRepo(t)
	request := map[string]any{
		"protocol_version": "collector-sdk/v1alpha3",
		"claim": map[string]any{
			"component_id":   "eshu.scanner.sbom",
			"instance_id":    "smoke",
			"collector_kind": "scanner_worker",
			"source_system":  "scanner_worker",
			"scope":          map[string]any{"id": scopeID, "kind": "repository"},
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
				"scope_id":  scopeID,
				"root_path": root,
			}},
			"acceptance_unit_id": "repository:smoke",
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
	var page struct {
		ProtocolVersion string           `json:"protocol_version"`
		State           string           `json:"state"`
		Claim           map[string]any   `json:"claim"`
		Generation      map[string]any   `json:"generation"`
		Facts           []map[string]any `json:"facts"`
		Statuses        []struct {
			Class     string `json:"class"`
			FactCount int    `json:"fact_count"`
		} `json:"statuses"`
		Page *struct {
			PageIndex  int  `json:"page_index"`
			TotalFacts int  `json:"total_facts"`
			More       bool `json:"more"`
		} `json:"page"`
	}
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
		if fact["kind"] == "sbom.document" {
			analysis = true
		}
	}
	if !analysis {
		t.Fatalf("no sbom.document fact, want the lockfile evidence path: %+v", page.Facts)
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
