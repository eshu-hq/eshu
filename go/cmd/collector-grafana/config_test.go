// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsGrafanaInstanceAndResolvesCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"grafana-primary",
			"collector_kind":"grafana",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"grafana",
					"scope_id":"grafana:prod",
					"instance_id":"prod",
					"base_url":"https://grafana.example.test/",
					"token_env":"GRAFANA_TOKEN",
					"resource_limit":25,
					"stale_after":"2h",
					"enabled":true,
					"declared_uids":["dashboards/checkout","datasources/prometheus"],
					"observed_only_hint":true
				}]
			}
		}]`,
		"GRAFANA_TOKEN":        "resolved-token",
		envPollInterval:        "2s",
		envClaimLeaseTTL:       "30s",
		envHeartbeatInterval:   "10s",
		envCollectorOwnerID:    "grafana-owner",
		envCollectorInstanceID: "grafana-primary",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.InstanceID != "grafana-primary" {
		t.Fatalf("InstanceID = %q, want grafana-primary", config.Instance.InstanceID)
	}
	if config.OwnerID != "grafana-owner" {
		t.Fatalf("OwnerID = %q, want grafana-owner", config.OwnerID)
	}
	if config.PollInterval != 2*time.Second {
		t.Fatalf("PollInterval = %s, want 2s", config.PollInterval)
	}
	target := config.Source.Targets[0]
	if target.BaseURL != "https://grafana.example.test" {
		t.Fatalf("BaseURL = %q, want trimmed URL", target.BaseURL)
	}
	if target.Token != "resolved-token" {
		t.Fatalf("Token = %q, want resolved credential", target.Token)
	}
	if target.StaleAfter != 2*time.Hour {
		t.Fatalf("StaleAfter = %s, want 2h", target.StaleAfter)
	}
	if _, ok := target.DeclaredUIDs["dashboards/checkout"]; !ok {
		t.Fatalf("DeclaredUIDs missing dashboards/checkout: %#v", target.DeclaredUIDs)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testGrafanaGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsUnresolvedGrafanaCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"grafana-primary",
			"collector_kind":"grafana",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"grafana:prod",
					"instance_id":"prod",
					"base_url":"https://grafana.example.test",
					"token_env":"GRAFANA_TOKEN",
					"enabled":true
				}]
			}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want unresolved credential")
	}
}

func TestLoadClaimedRuntimeConfigRejectsNegativeGrafanaFreshnessWindow(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"grafana-primary",
			"collector_kind":"grafana",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"grafana:prod",
					"instance_id":"prod",
					"base_url":"https://grafana.example.test",
					"token_env":"GRAFANA_TOKEN",
					"stale_after":"-1s",
					"enabled":true
				}]
			}
		}]`,
		"GRAFANA_TOKEN": "resolved-token",
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want negative stale_after rejection")
	}
}

func testGrafanaGetenv(key string) string {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"grafana-primary",
			"collector_kind":"grafana",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"grafana",
					"scope_id":"grafana:prod",
					"instance_id":"prod",
					"base_url":"https://grafana.example.test/",
					"token_env":"GRAFANA_TOKEN",
					"enabled":true
				}]
			}
		}]`,
		"GRAFANA_TOKEN":        "resolved-token",
		envCollectorOwnerID:    "grafana-owner",
		envCollectorInstanceID: "grafana-primary",
	}
	return env[key]
}
