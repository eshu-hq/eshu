// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/envregistry"
)

func TestEnvironMapParsesPairs(t *testing.T) {
	t.Parallel()
	got := environMap([]string{"ESHU_A=1", "ESHU_B=x=y", "NOEQUALS"})
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

	err := validateEnv(&out, envregistry.Default(), env, false)
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

	if err := validateEnv(&out, envregistry.Default(), env, true); err != nil {
		t.Fatalf("validateEnv with valid env error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Fatalf("expected OK output, got:\n%s", out.String())
	}
}

func TestValidateEnvDeprecatedWarnsWithoutError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	env := map[string]string{"ESHU_REDUCER_CLAIM_DOMAIN": "code"}

	if err := validateEnv(&out, envregistry.Default(), env, false); err != nil {
		t.Fatalf("deprecated-only env should not error, got %v", err)
	}
	if !strings.Contains(out.String(), "WARN") ||
		!strings.Contains(out.String(), "deprecated") {
		t.Fatalf("output missing deprecation warning:\n%s", out.String())
	}
}
