// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesAndChecksGeneratedEntrypoints(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := writeTestManifest(t, root)
	args := []string{"-repo-root", root, "-manifest", manifestPath}
	var stdout bytes.Buffer
	if err := run(args, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() write error = %v, want nil", err)
	}
	generatedPath := filepath.Join(root, "go", "cmd", "collector-demo", "main.go")
	if _, err := os.Stat(generatedPath); err != nil {
		t.Fatalf("generated main.go stat error = %v", err)
	}

	stdout.Reset()
	checkArgs := append(args, "-check")
	if err := run(checkArgs, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() check error = %v, want nil", err)
	}
	if got := stdout.String(); !strings.Contains(got, "generated collector entrypoints are current") {
		t.Fatalf("run() check stdout = %q, want current message", got)
	}

	if err := os.WriteFile(generatedPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("corrupt generated file: %v", err)
	}
	err := run(checkArgs, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() stale check error = nil, want stale generated file error")
	}
	if got := err.Error(); !strings.Contains(got, "is stale") {
		t.Fatalf("run() stale check error = %q, want stale message", got)
	}
}

func writeTestManifest(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "collector_entrypoints.yaml")
	if err := os.WriteFile(path, []byte(`
schema_version: 1
collectors:
  - command_dir: go/cmd/collector-demo
    runtime_name: collector-demo
    binary_name: eshu-collector-demo
    collector_label: demo collector
    go_name: Demo
    env:
      collector_instances: ESHU_COLLECTOR_INSTANCES_JSON
      instance_id: ESHU_DEMO_COLLECTOR_INSTANCE_ID
      poll_interval: ESHU_DEMO_POLL_INTERVAL
      claim_lease_ttl: ESHU_DEMO_CLAIM_LEASE_TTL
      heartbeat_interval: ESHU_DEMO_HEARTBEAT_INTERVAL
      owner_id: ESHU_DEMO_COLLECTOR_OWNER_ID
      owner_id_const_name: envCollectorOwnerID
    store_name: collector_demo
    claim_id_prefix: demo-claim
    collector_kind_expr: scope.CollectorKind("demo")
    scope_kind: demo
    auth_mode: token_env
    target_list_field: targets
    target_identity_fields: [scope_id]
    target_auth_fields: [token_env]
    source:
      import_path: github.com/eshu-hq/eshu/go/internal/collector/demo
      package_name: demo
      config_type: demo.SourceConfig
      constructor: demo.NewClaimedSource
      config_loader: loadDemoSourceConfig
      config_attacher: attachDemoRuntimeSignals
      runtime_config_type: demoRuntimeConfiguration
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
