// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

func TestEvidenceBundleExportWritesValidBundle(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--scope", "repo:demo/service", "--out", outPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("evidence bundle export error = %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported bundle did not validate: %v", err)
	}
	if bundle.SchemaVersion != evidencebundle.SchemaVersion {
		t.Fatalf("bundle.SchemaVersion = %q", bundle.SchemaVersion)
	}
}

func TestEvidenceBundleValidateRejectsPrivateCanary(t *testing.T) {
	bundle := evidencebundle.BuildDemoBundle(evidencebundle.DemoBundleOptions{ScopeID: "repo:demo/service"})
	bundle.Source.Repository = "/Users/example/private/repo"
	raw, err := evidencebundle.RenderJSON(bundle)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	var out bytes.Buffer
	cmd := newEvidenceBundleValidateCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--from", path})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("evidence bundle validate error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "local absolute path") {
		t.Fatalf("validate error = %v, want local absolute path", err)
	}
	if !strings.Contains(out.String(), "failed") {
		t.Fatalf("validate output missing failed verdict:\n%s", out.String())
	}
}

func TestRootCommandIncludesEvidenceBundle(t *testing.T) {
	if !commandPathExists("evidence bundle export") {
		t.Fatal("root command missing evidence bundle export")
	}
	if !commandPathExists("evidence bundle validate") {
		t.Fatal("root command missing evidence bundle validate")
	}
}
