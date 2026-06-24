// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDesiredCollectorInstanceValidateAcceptsPagerDutyClaimsEnabled(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "pagerduty-primary",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{
			"provider":"pagerduty",
			"scope_id":"pagerduty:account:example",
			"account_id":"example",
			"token_env":"PAGERDUTY_TOKEN",
			"api_base_url":"https://api.pagerduty.com",
			"incident_limit":50,
			"incident_lookback":"6h",
			"log_entry_limit":25,
			"change_event_limit":25,
			"allowed_service_ids":["SVC1"],
			"config_validation_enabled":true,
			"config_resource_limit":25
		}]}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsPagerDutyConfigResourceLimitOutOfRange(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "pagerduty-primary",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{
			"provider":"pagerduty",
			"scope_id":"pagerduty:account:example",
			"account_id":"example",
			"token_env":"PAGERDUTY_TOKEN",
			"config_validation_enabled":true,
			"config_resource_limit":101
		}]}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want config_resource_limit error")
	}
}

func TestDesiredCollectorInstanceValidateRejectsPagerDutyMissingTokenEnv(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "pagerduty-primary",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{
			"provider":"pagerduty",
			"scope_id":"pagerduty:account:example",
			"account_id":"example"
		}]}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing token_env")
	}
}

func TestDesiredCollectorInstanceValidateAcceptsPagerDutyComponentActivation(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "pagerduty-reference",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.pagerduty",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd",
			"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"process"}
		}`,
	}

	if err := instance.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestDesiredCollectorInstanceValidateRejectsMalformedComponentActivation(t *testing.T) {
	t.Parallel()

	instance := DesiredCollectorInstance{
		InstanceID:    "pagerduty-reference",
		CollectorKind: scope.CollectorPagerDuty,
		Mode:          CollectorModeScheduled,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{
			"schema_version":"eshu.component.instance.v1",
			"component_id":"dev.eshu.examples.pagerduty",
			"component_version":"0.1.0",
			"manifest_digest":"sha256:1234",
			"config_handle":"component-config:abcd"
		}`,
	}

	if err := instance.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want component runtime error")
	}
}
