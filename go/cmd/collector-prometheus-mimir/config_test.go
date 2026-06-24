// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsPrometheusMimirInstanceAndResolvesOptionalCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"metrics-primary",
			"collector_kind":"prometheus_mimir",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"mimir",
					"scope_id":"mimir:tenant:prod",
					"instance_id":"prod",
					"base_url":"https://mimir.example.test/",
					"path_prefix":"/prometheus",
					"token_env":"MIMIR_TOKEN",
					"tenant_id_env":"MIMIR_TENANT",
					"resource_limit":50,
					"stale_after":"1h",
					"enabled":true,
					"declared_ids":["target/api","rule/latency"],
					"observed_only_hint":true
				}]
			}
		}]`,
		"MIMIR_TOKEN":          "resolved-token",
		"MIMIR_TENANT":         "tenant-prod",
		envPollInterval:        "3s",
		envClaimLeaseTTL:       "45s",
		envHeartbeatInterval:   "15s",
		envCollectorOwnerID:    "metrics-owner",
		envCollectorInstanceID: "metrics-primary",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.Instance.InstanceID != "metrics-primary" {
		t.Fatalf("InstanceID = %q, want metrics-primary", config.Instance.InstanceID)
	}
	if config.OwnerID != "metrics-owner" {
		t.Fatalf("OwnerID = %q, want metrics-owner", config.OwnerID)
	}
	target := config.Source.Targets[0]
	if target.BaseURL != "https://mimir.example.test" {
		t.Fatalf("BaseURL = %q, want trimmed URL", target.BaseURL)
	}
	if target.Token != "resolved-token" {
		t.Fatalf("Token = %q, want resolved optional credential", target.Token)
	}
	if target.TenantID != "tenant-prod" {
		t.Fatalf("TenantID = %q, want resolved tenant", target.TenantID)
	}
	if target.StaleAfter != time.Hour {
		t.Fatalf("StaleAfter = %s, want 1h", target.StaleAfter)
	}
	if _, ok := target.DeclaredIDs["rule/latency"]; !ok {
		t.Fatalf("DeclaredIDs missing rule/latency: %#v", target.DeclaredIDs)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testPrometheusMimirGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigAllowsPrometheusWithoutCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"metrics-primary",
			"collector_kind":"prometheus_mimir",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"prometheus",
					"scope_id":"prometheus:local",
					"instance_id":"local",
					"base_url":"http://prometheus.monitoring.svc:9090",
					"enabled":true
				}]
			}
		}]`,
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if token := config.Source.Targets[0].Token; token != "" {
		t.Fatalf("Token = %q, want empty optional credential", token)
	}
}

func TestLoadClaimedRuntimeConfigRejectsNegativePrometheusMimirFreshnessWindow(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"metrics-primary",
			"collector_kind":"prometheus_mimir",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"prometheus",
					"scope_id":"prometheus:local",
					"instance_id":"local",
					"base_url":"http://prometheus.monitoring.svc:9090",
					"stale_after":"-1s",
					"enabled":true
				}]
			}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want negative stale_after rejection")
	}
}

func testPrometheusMimirGetenv(key string) string {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"metrics-primary",
			"collector_kind":"prometheus_mimir",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"provider":"prometheus",
					"scope_id":"prometheus:local",
					"instance_id":"local",
					"base_url":"http://prometheus.monitoring.svc:9090",
					"enabled":true
				}]
			}
		}]`,
		envCollectorOwnerID:    "metrics-owner",
		envCollectorInstanceID: "metrics-primary",
	}
	return env[key]
}
