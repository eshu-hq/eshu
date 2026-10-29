// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// expectedDeployCheckoutCommit pins the eshu-hq/eshu-deploy commit the
// override test reads compose files from. It must match the twin pins in
// go/internal/deploy/checkout_test.go and
// go/internal/runtime/deploy_checkout_test.go: every helper fails closed on
// any other commit, so a skew between the pins surfaces as a test failure,
// not a silent drift.
const expectedDeployCheckoutCommit = "bc6b17897056e3c5dab27dd2357a013869b2bd9a"

// deployCheckoutForContract resolves the eshu-deploy checkout the override
// test reads compose files from. It fails closed: an absent
// ESHU_DEPLOY_CHECKOUT, a missing directory, or a commit mismatch fails the
// calling test instead of silently reading the wrong tree.
func deployCheckoutForContract(t *testing.T) string {
	t.Helper()

	dir := os.Getenv("ESHU_DEPLOY_CHECKOUT")
	if dir == "" {
		t.Fatal("ESHU_DEPLOY_CHECKOUT is not set; point it at an eshu-hq/eshu-deploy checkout at " + expectedDeployCheckoutCommit)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is not a directory: %v", dir, err)
	}
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is not a git checkout: %v", dir, err)
	}
	if got := strings.TrimSpace(string(out)); got != expectedDeployCheckoutCommit {
		t.Fatalf("ESHU_DEPLOY_CHECKOUT=%q is at %s, want %s", dir, got, expectedDeployCheckoutCommit)
	}
	return dir
}

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
	composeDir := filepath.Join(deployCheckoutForContract(t), "compose")
	override := postgresCommand(t, filepath.Join(composeDir, "docker-compose.read-api-latency-gate.yaml"))

	meterFlags := []string{
		"-c", "shared_preload_libraries=pg_stat_statements",
		"-c", "pg_stat_statements.track=all",
		"-c", "pg_stat_statements.max=10000",
		"-c", "compute_query_id=on",
	}
	for _, base := range []string{"docker-compose.yaml", "docker-compose.neo4j.yml"} {
		want := append(append([]string(nil), postgresCommand(t, filepath.Join(composeDir, base))...), meterFlags...)
		if !reflect.DeepEqual(override, want) {
			t.Errorf("override postgres command is not %s's command plus the meter flags\n got: %v\nwant: %v", base, override, want)
		}
	}
}
