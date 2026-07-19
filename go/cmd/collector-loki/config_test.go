// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadClaimedRuntimeConfigSelectsLokiInstanceAndResolvesTarget(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:tenant:prod",
					"instance_id":"prod",
					"base_url":"https://loki.example.test/",
					"path_prefix":"/loki",
					"token_env":"LOKI_TOKEN",
					"tenant_id_env":"LOKI_TENANT",
					"resource_limit":50,
					"label_value_names":["app","trace_id"],
					"max_label_values_per_label":5,
					"series_matchers":["{app=~\".+\"}"],
					"stale_after":"30m",
					"enabled":true,
					"declared_ids":["series/app","rule/HighErrors"],
					"observed_only_hint":true
				}]
			}
		}]`,
		"LOKI_TOKEN":           "resolved-token",
		"LOKI_TENANT":          "tenant-prod",
		envPollInterval:        "4s",
		envClaimLeaseTTL:       "50s",
		envHeartbeatInterval:   "10s",
		envCollectorOwnerID:    "loki-owner",
		envCollectorInstanceID: "loki-primary",
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if config.OwnerID != "loki-owner" {
		t.Fatalf("OwnerID = %q, want loki-owner", config.OwnerID)
	}
	target := config.Source.Targets[0]
	if target.Token != "resolved-token" {
		t.Fatalf("Token = %q, want resolved credential", target.Token)
	}
	if target.TenantID != "tenant-prod" {
		t.Fatalf("TenantID = %q, want resolved tenant", target.TenantID)
	}
	if target.StaleAfter != 30*time.Minute {
		t.Fatalf("StaleAfter = %s, want 30m", target.StaleAfter)
	}
	if got := target.LabelValueNames; len(got) != 2 || got[0] != "app" || got[1] != "trace_id" {
		t.Fatalf("LabelValueNames = %#v, want app and trace_id", got)
	}
	if _, ok := target.DeclaredIDs["rule/HighErrors"]; !ok {
		t.Fatalf("DeclaredIDs missing rule/HighErrors: %#v", target.DeclaredIDs)
	}
}

func TestBuildClaimedServiceWiresDefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	service, err := buildClaimedService(nil, testLokiGetenv, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigAllowsLokiWithoutCredential(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:local",
					"instance_id":"local",
					"base_url":"http://loki.monitoring.svc:3100",
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

func TestLoadClaimedRuntimeConfigResolvesLokiSeriesLookback(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:tenant:prod",
					"instance_id":"prod",
					"base_url":"https://loki.example.test/",
					"series_lookback":"2h",
					"enabled":true
				}]
			}
		}]`,
	}

	config, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Source.Targets[0].SeriesLookback, 2*time.Hour; got != want {
		t.Fatalf("SeriesLookback = %s, want %s", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsNegativeLokiSeriesLookback(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:local",
					"instance_id":"local",
					"base_url":"http://loki.monitoring.svc:3100",
					"series_lookback":"-1s",
					"enabled":true
				}]
			}
		}]`,
	}

	if _, err := loadClaimedRuntimeConfig(func(key string) string { return env[key] }); err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want negative series_lookback rejection")
	}
}

func TestLoadClaimedRuntimeConfigRejectsNegativeLokiFreshnessWindow(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:local",
					"instance_id":"local",
					"base_url":"http://loki.monitoring.svc:3100",
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

func testLokiGetenv(key string) string {
	env := map[string]string{
		envCollectorInstances: `[{
			"instance_id":"loki-primary",
			"collector_kind":"loki",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"targets":[{
					"scope_id":"loki:local",
					"instance_id":"local",
					"base_url":"http://loki.monitoring.svc:3100",
					"enabled":true
				}]
			}
		}]`,
		envCollectorOwnerID:    "loki-owner",
		envCollectorInstanceID: "loki-primary",
	}
	return env[key]
}
