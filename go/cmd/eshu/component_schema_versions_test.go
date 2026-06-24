// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestComponentCommandTreeIncludesSchemaVersions(t *testing.T) {
	cmd, _, err := componentCmd.Find([]string{"schema-versions"})
	if err != nil {
		t.Fatalf("component schema-versions lookup error = %v, want nil", err)
	}
	if cmd == nil || cmd.Name() != "schema-versions" {
		t.Fatalf("component schema-versions command = %#v, want schema-versions subcommand", cmd)
	}
}

func TestComponentSchemaVersionsListJSON(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newSchemaVersionsCommand(out, true, "")
	if err := runComponentSchemaVersions(cmd, nil); err != nil {
		t.Fatalf("runComponentSchemaVersions() error = %v, want nil", err)
	}

	var payload struct {
		Entries []struct {
			FactKind      string `json:"fact_kind"`
			SchemaVersion string `json:"schema_version"`
		} `json:"fact_schema_versions"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; out=%s", err, out)
	}
	want := facts.TerraformStateFactKinds()[0]
	found := false
	for _, entry := range payload.Entries {
		if entry.FactKind == want {
			found = true
			if entry.SchemaVersion == "" {
				t.Fatalf("schema version for %q is empty", want)
			}
		}
	}
	if !found {
		t.Fatalf("fact_schema_versions missing %q", want)
	}
}

func TestComponentSchemaVersionsCheckSupported(t *testing.T) {
	t.Parallel()

	kind := facts.TerraformStateFactKinds()[0]
	supported, _ := facts.SchemaVersion(kind)

	out := &bytes.Buffer{}
	cmd := newSchemaVersionsCommand(out, true, kind+"="+supported)
	if err := runComponentSchemaVersions(cmd, nil); err != nil {
		t.Fatalf("runComponentSchemaVersions(supported) error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), `"compatibility": "supported"`) {
		t.Fatalf("output missing supported compatibility: %s", out)
	}
}

func TestComponentSchemaVersionsCheckUnsupportedExitsNonZero(t *testing.T) {
	t.Parallel()

	kind := facts.TerraformStateFactKinds()[0]

	out := &bytes.Buffer{}
	cmd := newSchemaVersionsCommand(out, true, kind+"=2.0.0")
	err := runComponentSchemaVersions(cmd, nil)
	if err == nil {
		t.Fatal("runComponentSchemaVersions(unsupported) error = nil, want non-nil")
	}
	if !strings.Contains(out.String(), `"compatibility": "unsupported_major"`) {
		t.Fatalf("output missing unsupported_major compatibility: %s", out)
	}
}

func TestComponentSchemaVersionsCheckUnknownKindExitsZero(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newSchemaVersionsCommand(out, true, "dev.example.unknown_kind=1.0.0")
	if err := runComponentSchemaVersions(cmd, nil); err != nil {
		t.Fatalf("runComponentSchemaVersions(unknown_kind) error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), `"compatibility": "unknown_kind"`) {
		t.Fatalf("output missing unknown_kind compatibility: %s", out)
	}
}

func TestComponentSchemaVersionsCheckRejectsMalformedFlag(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	cmd := newSchemaVersionsCommand(out, false, "no-equals-sign")
	if err := runComponentSchemaVersions(cmd, nil); err == nil {
		t.Fatal("runComponentSchemaVersions(malformed --check) error = nil, want non-nil")
	}
}

func newSchemaVersionsCommand(out *bytes.Buffer, asJSON bool, check string) *cobra.Command {
	cmd := &cobra.Command{RunE: runComponentSchemaVersions}
	cmd.SetOut(out)
	cmd.Flags().Bool(componentJSONFlag, asJSON, "")
	cmd.Flags().String(componentSchemaCheckFlag, check, "")
	return cmd
}
