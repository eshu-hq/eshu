// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestNeo4jProfileGroupStatementsParsesOptIn(t *testing.T) {
	t.Parallel()

	enabled, err := neo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("neo4jProfileGroupStatements() error = %v, want nil", err)
	}
	if !enabled {
		t.Fatal("neo4jProfileGroupStatements() = false, want true")
	}
}

func TestNeo4jProfileGroupStatementsRejectsInvalidBool(t *testing.T) {
	t.Parallel()

	_, err := neo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "sometimes"
		}
		return ""
	})
	if err == nil {
		t.Fatal("neo4jProfileGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS") {
		t.Fatalf("error = %q, want env var name", err.Error())
	}
}

func TestIngesterGroupProfileOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		backend     runtimecfg.GraphBackend
		group, file string
		wantGroup   bool
		wantFile    bool
		wantError   bool
	}{
		{name: "nornic both rejected", backend: runtimecfg.GraphBackendNornicDB, group: "true", file: "true", wantError: true},
		{name: "nornic group only", backend: runtimecfg.GraphBackendNornicDB, group: "true", wantGroup: true},
		{name: "nornic file only", backend: runtimecfg.GraphBackendNornicDB, file: "true", wantFile: true},
		{name: "neo4j ignores file", backend: runtimecfg.GraphBackendNeo4j, file: "true"},
		{name: "neo4j ignores invalid file", backend: runtimecfg.GraphBackendNeo4j, file: "invalid"},
		{name: "nornic rejects invalid file", backend: runtimecfg.GraphBackendNornicDB, file: "invalid", wantError: true},
		{name: "group error", backend: runtimecfg.GraphBackendNornicDB, group: "invalid", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS":
					return tc.group
				case fileGroupTimingEnv:
					return tc.file
				default:
					return ""
				}
			}
			group, file, err := ingesterGroupProfileOptions(tc.backend, getenv)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %t", err, tc.wantError)
			}
			if !tc.wantError && (group != tc.wantGroup || file != tc.wantFile) {
				t.Fatalf("options = (%t, %t), want (%t, %t)", group, file, tc.wantGroup, tc.wantFile)
			}
		})
	}
}
