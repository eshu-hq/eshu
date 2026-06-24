// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelmPagerDutyCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-pagerduty-collector") {
		t.Fatal("default chart render included eshu-pagerduty-collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "pagerduty-collector-values.yaml")
	values := []byte(`
contentStore:
  dsn: postgresql://eshu:secret@postgres:5432/eshu
neo4j:
  auth:
    secretName: ""
observability:
  prometheus:
    enabled: true
    serviceMonitor:
      enabled: true
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: pagerduty-primary
      collector_kind: pagerduty
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: pagerduty
            scope_id: pagerduty:account:example
            account_id: example-account
            token_env: PAGERDUTY_API_TOKEN
            api_base_url: https://api.pagerduty.com
            source_uri: pagerduty://example-account
            incident_limit: 25
            incident_lookback: 12h
            log_entry_limit: 50
            change_event_limit: 50
            allowed_service_ids: [PABC123]
            config_validation_enabled: true
            config_resource_limit: 20
pagerDutyCollector:
  enabled: true
  instanceId: pagerduty-primary
  pollInterval: 11s
  claimLeaseTTL: 2m
  heartbeatInterval: 40s
  collectorInstances:
    - instance_id: pagerduty-primary
      collector_kind: pagerduty
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: pagerduty
            scope_id: pagerduty:account:example
            account_id: example-account
            token_env: PAGERDUTY_API_TOKEN
            api_base_url: https://api.pagerduty.com
            source_uri: pagerduty://example-account
            incident_limit: 25
            incident_lookback: 12h
            log_entry_limit: 50
            change_event_limit: 50
            allowed_service_ids: [PABC123]
            config_validation_enabled: true
            config_resource_limit: 20
  extraEnv:
    - name: PAGERDUTY_API_TOKEN
      valueFrom:
        secretKeyRef:
          name: pagerduty-credentials
          key: api-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write pagerduty collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "pagerduty-collector", "eshu-collector-pagerduty", map[string]string{
		"ESHU_PAGERDUTY_COLLECTOR_INSTANCE_ID": "pagerduty-primary",
		"ESHU_PAGERDUTY_POLL_INTERVAL":         "11s",
		"ESHU_PAGERDUTY_CLAIM_LEASE_TTL":       "2m",
		"ESHU_PAGERDUTY_HEARTBEAT_INTERVAL":    "40s",
	})
	assertHelmValueFromEnv(t, manifests, "pagerduty-collector", "PAGERDUTY_API_TOKEN")
}
