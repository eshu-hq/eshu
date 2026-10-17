// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"strconv"
	"testing"
)

func validPagerDutyConfigurationJSON() string {
	return `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:account:example",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN"
		}]
	}`
}

func TestValidatePagerDutyCollectorConfigurationAcceptsPaginationBoundsWithinRange(t *testing.T) {
	t.Parallel()

	raw := `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:account:example",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"pagination_max_pages": 25,
			"pagination_max_records": 2500
		}]
	}`
	if err := ValidatePagerDutyCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePagerDutyCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidatePagerDutyCollectorConfigurationOmitsPaginationBoundsByDefault(t *testing.T) {
	t.Parallel()

	if err := ValidatePagerDutyCollectorConfiguration(validPagerDutyConfigurationJSON()); err != nil {
		t.Fatalf("ValidatePagerDutyCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidatePagerDutyCollectorConfigurationRejectsPaginationMaxPagesOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pages int
	}{
		{name: "negative", pages: -1},
		{name: "above max", pages: maxPagerDutyPageLimit + 1},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := `{
				"targets": [{
					"provider": "pagerduty",
					"scope_id": "pagerduty:account:example",
					"account_id": "example",
					"token_env": "PAGERDUTY_TOKEN",
					"pagination_max_pages": ` + strconv.Itoa(tt.pages) + `
				}]
			}`
			if err := ValidatePagerDutyCollectorConfiguration(raw); err == nil {
				t.Fatal("ValidatePagerDutyCollectorConfiguration() error = nil, want out-of-range pagination_max_pages error")
			}
		})
	}
}

func TestValidatePagerDutyCollectorConfigurationRejectsPaginationMaxRecordsOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records int
	}{
		{name: "negative", records: -1},
		{name: "above max", records: maxPagerDutyPaginationRecords + 1},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := `{
				"targets": [{
					"provider": "pagerduty",
					"scope_id": "pagerduty:account:example",
					"account_id": "example",
					"token_env": "PAGERDUTY_TOKEN",
					"pagination_max_records": ` + strconv.Itoa(tt.records) + `
				}]
			}`
			if err := ValidatePagerDutyCollectorConfiguration(raw); err == nil {
				t.Fatal("ValidatePagerDutyCollectorConfiguration() error = nil, want out-of-range pagination_max_records error")
			}
		})
	}
}

func TestValidatePagerDutyCollectorConfigurationRoundTripsAllowEmptySuccessor(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{
		"provider":"pagerduty",
		"scope_id":"pagerduty:account:example",
		"account_id":"example",
		"token_env":"PAGERDUTY_TOKEN",
		"allow_empty_successor":true
	}]}`

	if err := ValidatePagerDutyCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidatePagerDutyCollectorConfiguration() error = %v, want nil", err)
	}
	var decoded pagerDutyCollectorConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if len(decoded.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(decoded.Targets))
	}
	if !decoded.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = false with allow_empty_successor:true, want true")
	}

	absent := `{"targets":[{
		"provider":"pagerduty",
		"scope_id":"pagerduty:account:example",
		"account_id":"example",
		"token_env":"PAGERDUTY_TOKEN"
	}]}`
	var decodedAbsent pagerDutyCollectorConfiguration
	if err := json.Unmarshal([]byte(absent), &decodedAbsent); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if decodedAbsent.Targets[0].AllowEmptySuccessor {
		t.Fatal("AllowEmptySuccessor = true without allow_empty_successor, want false default")
	}
}
