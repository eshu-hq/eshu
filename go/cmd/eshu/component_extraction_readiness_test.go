// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newExtractionReadinessCmdForTest(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Bool(componentJSONFlag, false, "")
	cmd.Flags().Bool(componentExtractionReadinessVerboseFlag, false, "")
	cmd.SetOut(out)
	return cmd
}

func TestExtractionReadinessCommandRegistered(t *testing.T) {
	t.Parallel()
	cmd, _, err := rootCmd.Find([]string{"component", "extraction-readiness"})
	if err != nil {
		t.Fatalf("Find(component extraction-readiness) error = %v", err)
	}
	if cmd.Name() != "extraction-readiness" {
		t.Fatalf("resolved command = %q, want extraction-readiness", cmd.Name())
	}
}

func TestExtractionReadinessListsCatalog(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	if err := runComponentExtractionReadiness(cmd, nil); err != nil {
		t.Fatalf("runComponentExtractionReadiness() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Collector extraction readiness (advisory",
		"git [keep_in_tree]",
		"pagerduty [extraction_candidate]",
		"jira [blocked]",
		"blockers:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("catalog output missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestExtractionReadinessSingleFamily(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	if err := runComponentExtractionReadiness(cmd, []string{"pagerduty"}); err != nil {
		t.Fatalf("runComponentExtractionReadiness(pagerduty) error = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "pagerduty [extraction_candidate]") {
		t.Fatalf("single-family output = %q, want pagerduty extraction_candidate", got)
	}
	if strings.Contains(got, "git [keep_in_tree]") {
		t.Fatalf("single-family output unexpectedly listed other families:\n%s", got)
	}
	if !strings.Contains(got, "production correlation path") {
		t.Fatalf("single-family output missing in-tree production caveat:\n%s", got)
	}
}

func TestExtractionReadinessBlockersUsePolicyOrder(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	if err := runComponentExtractionReadiness(cmd, []string{"jira"}); err != nil {
		t.Fatalf("runComponentExtractionReadiness(jira) error = %v", err)
	}
	got := out.String()
	// Blockers must render in canonical policy order, not alphabetical, so the
	// list lines up with the extraction-criteria table.
	if !strings.Contains(got, "blockers: trust_boundary, runtime_behavior, proof_surface") {
		t.Fatalf("blockers not in policy order:\n%s", got)
	}
}

func TestExtractionReadinessUnknownFamily(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	err := runComponentExtractionReadiness(cmd, []string{"not_a_collector"})
	if err == nil {
		t.Fatal("runComponentExtractionReadiness(unknown) error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not tracked by the extraction policy") {
		t.Fatalf("error = %v, want 'not tracked by the extraction policy'", err)
	}
}

func TestExtractionReadinessJSON(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	if err := cmd.Flags().Set(componentJSONFlag, "true"); err != nil {
		t.Fatalf("set json flag: %v", err)
	}
	if err := runComponentExtractionReadiness(cmd, []string{"git"}); err != nil {
		t.Fatalf("runComponentExtractionReadiness(git --json) error = %v", err)
	}
	var payload struct {
		Rows []struct {
			Family         string `json:"family"`
			Classification string `json:"classification"`
			Criteria       []struct {
				Criterion string `json:"criterion"`
				State     string `json:"state"`
			} `json:"criteria"`
		} `json:"collector_extraction_readiness"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, out.String())
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("json rows = %d, want 1", len(payload.Rows))
	}
	if payload.Rows[0].Family != "git" || payload.Rows[0].Classification != "keep_in_tree" {
		t.Fatalf("json row = %+v, want git keep_in_tree", payload.Rows[0])
	}
	if len(payload.Rows[0].Criteria) != 7 {
		t.Fatalf("json criteria = %d, want 7", len(payload.Rows[0].Criteria))
	}
}

func TestExtractionReadinessVerboseShowsCriteria(t *testing.T) {
	t.Parallel()
	out := &bytes.Buffer{}
	cmd := newExtractionReadinessCmdForTest(out)
	if err := cmd.Flags().Set(componentExtractionReadinessVerboseFlag, "true"); err != nil {
		t.Fatalf("set verbose flag: %v", err)
	}
	if err := runComponentExtractionReadiness(cmd, []string{"jira"}); err != nil {
		t.Fatalf("runComponentExtractionReadiness(jira --verbose) error = %v", err)
	}
	got := out.String()
	for _, want := range []string{"source_coupling: met", "runtime_behavior: unmet", "proof_surface: unmet"} {
		if !strings.Contains(got, want) {
			t.Fatalf("verbose output missing %q:\n%s", want, got)
		}
	}
}
