// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadClaimedRuntimeConfigSelectsPagerDutyInstanceAndLoadsTokenEnv(t *testing.T) {
	t.Parallel()

	config, err := loadClaimedRuntimeConfig(func(key string) string {
		switch key {
		case envCollectorInstances:
			return `[{
				"instance_id": "pagerduty-primary",
				"collector_kind": "pagerduty",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": {
					"targets": [{
						"provider": "pagerduty",
						"scope_id": "pagerduty:account:example",
						"account_id": "example",
						"token_env": "PAGERDUTY_TOKEN",
						"api_base_url": "https://api.pagerduty.com",
						"source_uri": "https://example.pagerduty.com",
						"incident_limit": 50,
						"incident_lookback": "6h",
						"log_entry_limit": 25,
						"change_event_limit": 25,
						"allowed_service_ids": ["SVC1"],
						"config_validation_enabled": true,
						"config_resource_limit": 25,
						"pagination_max_pages": 20,
						"pagination_max_records": 2000
					}]
				}
			}]`
		case envCollectorInstanceID:
			return "pagerduty-primary"
		case envOwnerID:
			return "pod-pagerduty"
		case "PAGERDUTY_TOKEN":
			return "token-value"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.CollectorKind, scope.CollectorPagerDuty; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	target := config.Source.Targets[0]
	if got, want := target.Token, "token-value"; got != want {
		t.Fatalf("Target token = %q, want %q", got, want)
	}
	if got, want := target.IncidentLookback, 6*time.Hour; got != want {
		t.Fatalf("IncidentLookback = %s, want %s", got, want)
	}
	if !target.ConfigValidationEnabled {
		t.Fatal("ConfigValidationEnabled = false, want true")
	}
	if got, want := target.ConfigResourceLimit, 25; got != want {
		t.Fatalf("ConfigResourceLimit = %d, want %d", got, want)
	}
	if got, want := target.PaginationMaxPages, 20; got != want {
		t.Fatalf("PaginationMaxPages = %d, want %d", got, want)
	}
	if got, want := target.PaginationMaxRecords, 2000; got != want {
		t.Fatalf("PaginationMaxRecords = %d, want %d", got, want)
	}
}

func TestLoadClaimedRuntimeConfigRejectsMissingPagerDutyToken(t *testing.T) {
	t.Parallel()

	_, err := loadClaimedRuntimeConfig(func(key string) string {
		if key != envCollectorInstances {
			return ""
		}
		return `[{
			"instance_id": "pagerduty-primary",
			"collector_kind": "pagerduty",
			"mode": "continuous",
			"enabled": true,
			"claims_enabled": true,
			"configuration": {
				"targets": [{
					"provider": "pagerduty",
					"scope_id": "pagerduty:account:example",
					"account_id": "example",
					"token_env": "PAGERDUTY_TOKEN"
				}]
			}
		}]`
	})
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want missing credential error")
	}
	if strings.Contains(err.Error(), "token-value") || !strings.Contains(err.Error(), "PAGERDUTY_TOKEN") {
		t.Fatalf("loadClaimedRuntimeConfig() error = %q, want credential env reference without value", err)
	}
}
