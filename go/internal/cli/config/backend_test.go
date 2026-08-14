// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"strings"
	"testing"
)

func TestConfigureDatabaseBackendPersistsNornicDBSelection(t *testing.T) {
	t.Setenv(homeEnvVar, t.TempDir())

	if _, err := ConfigureDatabaseBackend("nornicdb"); err != nil {
		t.Fatalf("ConfigureDatabaseBackend() error = %v, want nil", err)
	}

	if got := ResolveValue("ESHU_GRAPH_BACKEND", ""); got != "nornicdb" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", got)
	}
	if got := ResolveValue("DEFAULT_DATABASE", ""); got != "nornic" {
		t.Fatalf("DEFAULT_DATABASE = %q, want nornic", got)
	}
	if got := ResolveValue("ESHU_NEO4J_DATABASE", ""); got != "nornic" {
		t.Fatalf("ESHU_NEO4J_DATABASE = %q, want nornic", got)
	}
}

func TestConfigureDatabaseBackendPersistsNeo4jSelection(t *testing.T) {
	t.Setenv(homeEnvVar, t.TempDir())

	if _, err := ConfigureDatabaseBackend("neo4j"); err != nil {
		t.Fatalf("ConfigureDatabaseBackend() error = %v, want nil", err)
	}

	if got := ResolveValue("ESHU_GRAPH_BACKEND", ""); got != "neo4j" {
		t.Fatalf("ESHU_GRAPH_BACKEND = %q, want neo4j", got)
	}
	if got := ResolveValue("DEFAULT_DATABASE", ""); got != "neo4j" {
		t.Fatalf("DEFAULT_DATABASE = %q, want neo4j", got)
	}
	if got := ResolveValue("ESHU_NEO4J_DATABASE", ""); got != "neo4j" {
		t.Fatalf("ESHU_NEO4J_DATABASE = %q, want neo4j", got)
	}
}

// TestConfigureDatabaseBackendAcceptsAliasAndNormalizesInput pins the two
// input tolerances the CLI relies on: the "nornic" alias and the
// trim+lower-case normalization of whatever the operator typed.
func TestConfigureDatabaseBackendAcceptsAliasAndNormalizesInput(t *testing.T) {
	for _, raw := range []string{"nornic", "  NornicDB  ", "NORNIC"} {
		t.Run(strings.TrimSpace(raw), func(t *testing.T) {
			t.Setenv(homeEnvVar, t.TempDir())

			got, err := ConfigureDatabaseBackend(raw)
			if err != nil {
				t.Fatalf("ConfigureDatabaseBackend(%q) error = %v, want nil", raw, err)
			}
			if got != "nornicdb" {
				t.Fatalf("ConfigureDatabaseBackend(%q) = %q, want nornicdb", raw, got)
			}
			if v := ResolveValue("ESHU_GRAPH_BACKEND", ""); v != "nornicdb" {
				t.Fatalf("ESHU_GRAPH_BACKEND = %q, want nornicdb", v)
			}
		})
	}
}

func TestConfigureDatabaseBackendRejectsUnknownBackend(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnvVar, home)

	got, err := ConfigureDatabaseBackend("Postgres")
	if err == nil {
		t.Fatalf("ConfigureDatabaseBackend(\"Postgres\") = %q, want an error", got)
	}
	if got != "" {
		t.Errorf("backend = %q, want empty on error", got)
	}
	// The error names the normalized value and both accepted backends, so the
	// operator can retype the argument without reading the source.
	for _, want := range []string{"postgres", "nornicdb", "neo4j"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// A rejected backend must not have written a partial selection.
	if len(Load()) != 0 {
		t.Errorf("Load() = %v, want no keys written for a rejected backend", Load())
	}
}
