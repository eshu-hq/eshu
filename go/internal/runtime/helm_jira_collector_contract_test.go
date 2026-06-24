// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelmJiraCollectorDeployment(t *testing.T) {
	t.Parallel()

	defaultManifests := renderHelmChart(t)
	if helmManifestExists(defaultManifests, "Deployment", "eshu-jira-collector") {
		t.Fatal("default chart render included eshu-jira-collector deployment")
	}

	valuesPath := filepath.Join(t.TempDir(), "jira-collector-values.yaml")
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
    - instance_id: jira-primary
      collector_kind: jira
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: jira_cloud
            scope_id: jira:site:example
            site_id: example.atlassian.net
            base_url: https://example.atlassian.net
            email_env: JIRA_EMAIL
            token_env: JIRA_API_TOKEN
            jql: project = OPS ORDER BY updated ASC
            issue_limit: 50
            updated_lookback: 24h
            changelog_limit: 50
            remote_link_limit: 50
            metadata_limit: 100
jiraCollector:
  enabled: true
  instanceId: jira-primary
  pollInterval: 9s
  claimLeaseTTL: 90s
  heartbeatInterval: 30s
  collectorInstances:
    - instance_id: jira-primary
      collector_kind: jira
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: jira_cloud
            scope_id: jira:site:example
            site_id: example.atlassian.net
            base_url: https://example.atlassian.net
            email_env: JIRA_EMAIL
            token_env: JIRA_API_TOKEN
            jql: project = OPS ORDER BY updated ASC
            issue_limit: 50
            updated_lookback: 24h
            changelog_limit: 50
            remote_link_limit: 50
            metadata_limit: 100
  extraEnv:
    - name: JIRA_EMAIL
      valueFrom:
        secretKeyRef:
          name: jira-credentials
          key: email
    - name: JIRA_API_TOKEN
      valueFrom:
        secretKeyRef:
          name: jira-credentials
          key: api-token
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("write jira collector values: %v", err)
	}

	manifests := renderHelmChart(t, "-f", valuesPath)
	assertClaimCollector(t, manifests, "jira-collector", "eshu-collector-jira", map[string]string{
		"ESHU_JIRA_COLLECTOR_INSTANCE_ID": "jira-primary",
		"ESHU_JIRA_POLL_INTERVAL":         "9s",
		"ESHU_JIRA_CLAIM_LEASE_TTL":       "90s",
		"ESHU_JIRA_HEARTBEAT_INTERVAL":    "30s",
	})
	assertHelmValueFromEnv(t, manifests, "jira-collector", "JIRA_EMAIL")
	assertHelmValueFromEnv(t, manifests, "jira-collector", "JIRA_API_TOKEN")
}
