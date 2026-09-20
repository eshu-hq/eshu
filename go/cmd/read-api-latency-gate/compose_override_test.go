// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// postgresCommand returns services.postgres.command from a compose file.
func postgresCommand(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	svc, ok := doc.Services["postgres"]
	if !ok || len(svc.Command) == 0 {
		t.Fatalf("%s has no services.postgres.command", path)
	}
	return svc.Command
}

// TestComposeOverrideIsBasePostgresCommandPlusMeterFlags guards the one thing
// the override can get wrong. Compose REPLACES a service's `command` list
// rather than appending to it, so docker-compose.read-api-latency-gate.yaml
// has to repeat the whole base postgres command and add only the
// pg_stat_statements flags. If the base command changes (a new -c flag, a
// resized pool) and the override is not updated, the gate would silently run
// Postgres with different settings from every other compose stack. Checked
// against both compose files the run script can select.
func TestComposeOverrideIsBasePostgresCommandPlusMeterFlags(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	override := postgresCommand(t, filepath.Join(root, "docker-compose.read-api-latency-gate.yaml"))

	meterFlags := []string{
		"-c", "shared_preload_libraries=pg_stat_statements",
		"-c", "pg_stat_statements.track=all",
		"-c", "pg_stat_statements.max=10000",
		"-c", "compute_query_id=on",
	}
	for _, base := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		want := append(append([]string(nil), postgresCommand(t, filepath.Join(root, base))...), meterFlags...)
		if !reflect.DeepEqual(override, want) {
			t.Errorf("override postgres command is not %s's command plus the meter flags\n got: %v\nwant: %v", base, override, want)
		}
	}
}
