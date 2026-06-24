// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import "testing"

func TestRemoteE2EObservabilityComposeGatesCollectorsOnPreflight(t *testing.T) {
	t.Parallel()

	doc := readComposeDocument(t, "docker-compose.remote-e2e.observability.yaml")
	for _, tc := range []struct {
		collector        string
		enableEnv        string
		baseURLEnv       string
		preflight        string
		profile          string
		tokenEnvRef      string
		tenantIDEnvRef   string
		tokenValueEnv    string
		tenantValueEnvID string
	}{
		{
			collector:     "grafana",
			enableEnv:     "ESHU_REMOTE_E2E_GRAFANA_ENABLED",
			baseURLEnv:    "ESHU_GRAFANA_BASE_URL",
			preflight:     "collector-grafana-preflight",
			profile:       "grafana",
			tokenEnvRef:   "GRAFANA_TOKEN",
			tokenValueEnv: "GRAFANA_TOKEN",
		},
		{
			collector:        "prometheus_mimir",
			enableEnv:        "ESHU_REMOTE_E2E_PROMETHEUS_MIMIR_ENABLED",
			baseURLEnv:       "ESHU_PROMETHEUS_MIMIR_BASE_URL",
			preflight:        "collector-prometheus-mimir-preflight",
			profile:          "prometheus-mimir",
			tokenEnvRef:      "${ESHU_PROMETHEUS_MIMIR_TOKEN_ENV:-}",
			tenantIDEnvRef:   "${ESHU_PROMETHEUS_MIMIR_TENANT_ID_ENV:-}",
			tokenValueEnv:    "PROMETHEUS_MIMIR_TOKEN",
			tenantValueEnvID: "PROMETHEUS_MIMIR_TENANT_ID",
		},
		{
			collector:        "loki",
			enableEnv:        "ESHU_REMOTE_E2E_LOKI_ENABLED",
			baseURLEnv:       "ESHU_LOKI_BASE_URL",
			preflight:        "collector-loki-preflight",
			profile:          "loki",
			tokenEnvRef:      "${ESHU_LOKI_TOKEN_ENV:-}",
			tenantIDEnvRef:   "${ESHU_LOKI_TENANT_ID_ENV:-}",
			tokenValueEnv:    "LOKI_TOKEN",
			tenantValueEnvID: "LOKI_TENANT_ID",
		},
		{
			collector:        "tempo",
			enableEnv:        "ESHU_REMOTE_E2E_TEMPO_ENABLED",
			baseURLEnv:       "ESHU_TEMPO_BASE_URL",
			preflight:        "collector-tempo-preflight",
			profile:          "tempo",
			tokenEnvRef:      "${ESHU_TEMPO_TOKEN_ENV:-}",
			tenantIDEnvRef:   "${ESHU_TEMPO_TENANT_ID_ENV:-}",
			tokenValueEnv:    "TEMPO_TOKEN",
			tenantValueEnvID: "TEMPO_TENANT_ID",
		},
	} {
		tc := tc
		t.Run(tc.preflight, func(t *testing.T) {
			t.Parallel()

			preflight := requireComposeService(t, doc, tc.preflight)
			if preflight.Image != "alpine:3.21" {
				t.Fatalf("%s image = %q, want alpine:3.21", tc.preflight, preflight.Image)
			}
			if len(preflight.Profiles) != 1 || preflight.Profiles[0] != tc.profile {
				t.Fatalf("%s profiles = %#v, want [%s]", tc.preflight, preflight.Profiles, tc.profile)
			}
			if preflight.Restart != "no" {
				t.Fatalf("%s restart = %q, want no", tc.preflight, preflight.Restart)
			}
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_COLLECTOR", tc.collector)
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_ENABLE_ENV", tc.enableEnv)
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_ENABLED", "${"+tc.enableEnv+":-false}")
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_BASE_URL_ENV", tc.baseURLEnv)
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_BASE_URL", "${"+tc.baseURLEnv+":-}")
			assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_TOKEN_ENV", tc.tokenEnvRef)
			assertComposeEnv(t, preflight, tc.tokenValueEnv, "${ESHU_"+tc.tokenValueEnv+":-}")
			assertComposeVolumeContains(
				t,
				preflight,
				"./scripts/remote-e2e-observability-preflight.sh:/usr/local/bin/remote-e2e-observability-preflight.sh:ro",
			)
			assertComposeScriptContains(t, preflight, "remote-e2e-observability-preflight.sh")

			if tc.tenantIDEnvRef != "" {
				assertComposeEnv(t, preflight, "ESHU_OBSERVABILITY_TENANT_ID_ENV", tc.tenantIDEnvRef)
				assertComposeEnv(t, preflight, tc.tenantValueEnvID, "${ESHU_"+tc.tenantValueEnvID+":-}")
			}

			collector := requireComposeService(t, doc, "collector-"+tc.profile)
			assertComposeDependency(t, collector, tc.preflight)
		})
	}
}
