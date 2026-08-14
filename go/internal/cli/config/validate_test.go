// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
)

func TestEnvironMapParsesPairs(t *testing.T) {
	t.Parallel()
	got := EnvironMap([]string{"ESHU_A=1", "ESHU_B=x=y", "NOEQUALS"})
	if got["ESHU_A"] != "1" {
		t.Errorf("ESHU_A = %q, want 1", got["ESHU_A"])
	}
	if got["ESHU_B"] != "x=y" {
		t.Errorf("ESHU_B = %q, want x=y", got["ESHU_B"])
	}
	if _, ok := got["NOEQUALS"]; ok {
		t.Error("NOEQUALS should be skipped")
	}
}

func TestValidateEnvInvalidValueReturnsError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	env := map[string]string{"ESHU_POSTGRES_MAX_OPEN_CONNS": "not-a-number"}

	err := ValidateEnv(&out, envregistry.Default(), env, false)
	if err == nil {
		t.Fatal("expected non-nil error for an invalid value")
	}
	if !strings.Contains(out.String(), "ERROR") ||
		!strings.Contains(out.String(), "ESHU_POSTGRES_MAX_OPEN_CONNS") {
		t.Fatalf("output missing the invalid-value error:\n%s", out.String())
	}
}

func TestValidateEnvCleanSucceeds(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	env := map[string]string{
		"ESHU_GRAPH_BACKEND": "nornicdb",
		"ESHU_API_ADDR":      ":8080",
	}

	if err := ValidateEnv(&out, envregistry.Default(), env, true); err != nil {
		t.Fatalf("ValidateEnv with valid env error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Fatalf("expected OK output, got:\n%s", out.String())
	}
}

func TestValidateEnvDeprecatedWarnsWithoutError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	env := map[string]string{"ESHU_REDUCER_CLAIM_DOMAIN": "code"}

	if err := ValidateEnv(&out, envregistry.Default(), env, false); err != nil {
		t.Fatalf("deprecated-only env should not error, got %v", err)
	}
	if !strings.Contains(out.String(), "WARN") ||
		!strings.Contains(out.String(), "deprecated") {
		t.Fatalf("output missing deprecation warning:\n%s", out.String())
	}
}

// TestReportFindingsOrdersErrorsBeforeWarnings pins the report ordering and
// the summary line an operator reads at the bottom of `eshu config validate`.
func TestReportFindingsOrdersErrorsBeforeWarnings(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	findings := []envregistry.Finding{
		{Name: "ESHU_W2", Message: "ESHU_W2 warn two"},
		{Name: "ESHU_E2", Message: "ESHU_E2 error two", Error: true},
		{Name: "ESHU_W1", Message: "ESHU_W1 warn one"},
		{Name: "ESHU_E1", Message: "ESHU_E1 error one", Error: true},
	}

	err := reportFindings(&out, findings)
	if err == nil {
		t.Fatal("reportFindings() = nil, want an error when any finding is error-level")
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		"ERROR ESHU_E1 error one",
		"ERROR ESHU_E2 error two",
		"WARN  ESHU_W1 warn one",
		"WARN  ESHU_W2 warn two",
	}
	if len(lines) < len(want) {
		t.Fatalf("output has %d lines, want at least %d:\n%s", len(lines), len(want), out.String())
	}
	for i, wantLine := range want {
		if lines[i] != wantLine {
			t.Fatalf("line %d = %q, want %q\nfull output:\n%s", i, lines[i], wantLine, out.String())
		}
	}
	if !strings.Contains(out.String(), "config validate: 2 error(s), 2 warning(s)") {
		t.Errorf("output missing the error/warning tally:\n%s", out.String())
	}
}

func TestReportFindingsWarningsOnlySucceed(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	findings := []envregistry.Finding{{Name: "ESHU_W", Message: "ESHU_W warn"}}

	if err := reportFindings(&out, findings); err != nil {
		t.Fatalf("reportFindings() with warnings only error = %v, want nil", err)
	}
	if !strings.Contains(out.String(), "0 error(s), 1 warning(s)") {
		t.Errorf("output missing the warnings-only tally:\n%s", out.String())
	}
}
