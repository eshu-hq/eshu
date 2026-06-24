// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsTempoInstanceAndResolvesTarget(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"tempo-primary",
			"collector_kind":"tempo",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"tempo:tenant:prod",
					"instance_id":"prod",
					"base_url":"https://tempo.example.test/",
					"path_prefix":"/tempo",
					"token_env":"TEMPO_TOKEN",
					"tenant_id_env":"TEMPO_TENANT",
					"resource_limit":50,
					"tag_value_names":["resource.service.name"],
					"max_tag_values_per_tag":5,
					"stale_after":"45m",
					"enabled":true,
					"declared_ids":["tag/resource.service.name"],
					"observed_only_hint":true,
					"freshness_probe_enabled":true,
					"lookback":"2h"
				}]
			}
		}]`,
		"TEMPO_TOKEN":          "resolved-token",
		"TEMPO_TENANT":         "tenant-prod",
		envPollInterval:        "5s",
		envClaimLeaseTTL:       "1m",
		envHeartbeatInterval:   "20s",
		envCollectorOwnerID:    "tempo-owner",
		envCollectorInstanceID: "tempo-primary",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.OwnerID != "tempo-owner" {
		t.Fatalf("OwnerID = %q, want tempo-owner", config.OwnerID)
	}
	target := config.Source.Targets[0]
	if target.Token != "resolved-token" {
		t.Fatalf("Token = %q, want resolved credential", target.Token)
	}
	if target.TenantID != "tenant-prod" {
		t.Fatalf("TenantID = %q, want resolved tenant", target.TenantID)
	}
	if target.StaleAfter != 45*time.Minute {
		t.Fatalf("StaleAfter = %s, want 45m", target.StaleAfter)
	}
	if target.Lookback != 2*time.Hour {
		t.Fatalf("Lookback = %s, want 2h", target.Lookback)
	}
	if !target.FreshnessProbeEnable {
		t.Fatal("FreshnessProbeEnable = false, want true")
	}
	if _, ok := target.DeclaredIDs["tag/resource.service.name"]; !ok {
		t.Fatalf("DeclaredIDs missing tag/resource.service.name: %#v", target.DeclaredIDs)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testTempoGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigAllowsTempoWithoutCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"tempo-primary",
			"collector_kind":"tempo",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"tempo:local",
					"instance_id":"local",
					"base_url":"http://tempo.monitoring.svc:3200",
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

func TestLoadClaimedRuntimeConfigRejectsNegativeTempoLookback(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"tempo-primary",
			"collector_kind":"tempo",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"tempo:local",
					"instance_id":"local",
					"base_url":"http://tempo.monitoring.svc:3200",
					"lookback":"-1s",
					"enabled":true
				}]
			}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want negative lookback rejection")
	}
}

func testTempoGetenv(key string) string {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"tempo-primary",
			"collector_kind":"tempo",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"tempo:local",
					"instance_id":"local",
					"base_url":"http://tempo.monitoring.svc:3200",
					"enabled":true
				}]
			}
		}]`,
		envCollectorOwnerID:    "tempo-owner",
		envCollectorInstanceID: "tempo-primary",
	}
	return env[key]
}
