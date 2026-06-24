// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entrypoints

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestFileAppliesTopLevelSchemaVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "collector_entrypoints.yaml")
	if err := os.WriteFile(path, []byte(`
schema_version: 1
collectors:
  - command_dir: go/cmd/collector-pagerduty
    runtime_name: collector-pagerduty
    binary_name: eshu-collector-pagerduty
    collector_label: pagerduty collector
    go_name: PagerDuty
    env:
      collector_instances: ESHU_COLLECTOR_INSTANCES_JSON
      instance_id: ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID
      poll_interval: ESHU_PAGERDUTY_POLL_INTERVAL
      claim_lease_ttl: ESHU_PAGERDUTY_CLAIM_LEASE_TTL
      heartbeat_interval: ESHU_PAGERDUTY_HEARTBEAT_INTERVAL
      owner_id: ESHU_PAGERDUTY_COLLECTOR_OWNER_ID
      owner_id_const_name: envOwnerID
    store_name: collector_pagerduty
    claim_id_prefix: pagerduty-claim
    collector_kind_expr: scope.CollectorPagerDuty
    scope_kind: incident
    auth_mode: token_env
    target_list_field: targets
    target_identity_fields: [account_id]
    target_auth_fields: [token_env]
    source:
      import_path: github.com/eshu-hq/eshu/go/internal/collector/pagerduty
      package_name: pagerduty
      config_type: pagerduty.SourceConfig
      constructor: pagerduty.NewClaimedSource
      config_loader: loadPagerDutySourceConfig
      config_attacher: attachPagerDutyRuntimeSignals
      runtime_config_type: pagerDutyRuntimeConfiguration
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifests, err := LoadManifestFile(path)
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v, want nil", err)
	}
	if got, want := manifests[0].SchemaVersion, 1; got != want {
		t.Fatalf("SchemaVersion = %d, want %d", got, want)
	}
	if got, want := manifests[0].GoName, "PagerDuty"; got != want {
		t.Fatalf("GoName = %q, want %q", got, want)
	}
}

func TestLoadManifestFileRejectsDuplicateCommandDir(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "collector_entrypoints.yaml")
	if err := os.WriteFile(path, []byte(`
schema_version: 1
collectors:
  - schema_version: 1
    command_dir: go/cmd/collector-empty
    runtime_name: collector-one
    binary_name: eshu-collector-one
    collector_label: empty collector
    go_name: Empty
    env: {collector_instances: ESHU_COLLECTOR_INSTANCES_JSON, instance_id: ESHU_EMPTY_INSTANCE_ID, poll_interval: ESHU_EMPTY_POLL_INTERVAL, claim_lease_ttl: ESHU_EMPTY_CLAIM_LEASE_TTL, heartbeat_interval: ESHU_EMPTY_HEARTBEAT_INTERVAL, owner_id: ESHU_EMPTY_OWNER_ID, owner_id_const_name: envOwnerID}
    store_name: collector_empty
    claim_id_prefix: empty-claim
    collector_kind_expr: scope.CollectorKind("empty")
    scope_kind: test
    auth_mode: none
    target_list_field: targets
    target_identity_fields: [scope_id]
    target_auth_fields: [none]
    source: {import_path: github.com/eshu-hq/eshu/go/internal/collector/empty, package_name: empty, config_type: empty.SourceConfig, constructor: empty.NewClaimedSource, config_loader: loadEmptySourceConfig, config_attacher: attachEmptyRuntimeSignals, runtime_config_type: emptyRuntimeConfiguration}
  - schema_version: 1
    command_dir: go/cmd/collector-empty
    runtime_name: collector-two
    binary_name: eshu-collector-two
    collector_label: empty collector
    go_name: Empty
    env: {collector_instances: ESHU_COLLECTOR_INSTANCES_JSON, instance_id: ESHU_EMPTY_INSTANCE_ID, poll_interval: ESHU_EMPTY_POLL_INTERVAL, claim_lease_ttl: ESHU_EMPTY_CLAIM_LEASE_TTL, heartbeat_interval: ESHU_EMPTY_HEARTBEAT_INTERVAL, owner_id: ESHU_EMPTY_OWNER_ID, owner_id_const_name: envOwnerID}
    store_name: collector_empty
    claim_id_prefix: empty-claim
    collector_kind_expr: scope.CollectorKind("empty")
    scope_kind: test
    auth_mode: none
    target_list_field: targets
    target_identity_fields: [scope_id]
    target_auth_fields: [none]
    source: {import_path: github.com/eshu-hq/eshu/go/internal/collector/empty, package_name: empty, config_type: empty.SourceConfig, constructor: empty.NewClaimedSource, config_loader: loadEmptySourceConfig, config_attacher: attachEmptyRuntimeSignals, runtime_config_type: emptyRuntimeConfiguration}
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := LoadManifestFile(path)
	if err == nil {
		t.Fatal("LoadManifestFile() error = nil, want duplicate command_dir rejection")
	}
	if got := err.Error(); !strings.Contains(got, "duplicate command_dir") {
		t.Fatalf("LoadManifestFile() error = %q, want duplicate command_dir", got)
	}
}
