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

	bin := filepath.Join(t.TempDir(), "scanner-component-image")
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
			"component_id":   "eshu.scanner.image",
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
				"scope_id":        scopeID,
				"rootfs_path":     root,
				"image_reference": "registry.example/smoke:1.0",
				"image_digest":    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
		if fact["kind"] == "scanner_worker.analysis" {
			analysis = true
		}
	}
	if !analysis {
		t.Fatalf("no scanner_worker.analysis fact, want the full analysis path: %+v", page.Facts)
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
