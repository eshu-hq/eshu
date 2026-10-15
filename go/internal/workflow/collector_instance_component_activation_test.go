// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func alpha2ActivationConfiguration(protocol, adapter string) string {
	return `{
		"schema_version":"eshu.component.instance.v1",
		"component_id":"dev.eshu.examples.pagerduty",
		"component_version":"0.1.0",
		"manifest_digest":"sha256:1234",
		"config_handle":"component-config:abcd",
		"runtime":{"sdk_protocol":"` + protocol + `","adapter":"` + adapter + `"}
	}`
}

// TestDesiredCollectorInstanceValidateAcceptsAlpha2ComponentActivation proves
// durable desired-instance validation admits an explicitly configured
// collector-sdk/v1alpha2 activation, including a padded protocol variant that
// must normalize rather than bypass the allowlist.
func TestDesiredCollectorInstanceValidateAcceptsAlpha2ComponentActivation(t *testing.T) {
	t.Parallel()

	for _, adapter := range []string{"oci", "process"} {
		for _, protocol := range []string{"collector-sdk/v1alpha2", " collector-sdk/v1alpha2 "} {
			t.Run(adapter+"/"+protocol, func(t *testing.T) {
				t.Parallel()

				instance := DesiredCollectorInstance{
					InstanceID:    "pagerduty-reference",
					CollectorKind: scope.CollectorPagerDuty,
					Mode:          CollectorModeScheduled,
					Enabled:       true,
					ClaimsEnabled: true,
					Configuration: alpha2ActivationConfiguration(protocol, adapter),
				}

				if err := instance.Validate(); err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
			})
		}
	}
}

// TestCollectorInstanceValidateAcceptsAlpha2ComponentActivation proves stored
// collector instances carrying an explicitly configured
// collector-sdk/v1alpha2 activation validate, including the padded protocol
// variant.
func TestCollectorInstanceValidateAcceptsAlpha2ComponentActivation(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	for _, protocol := range []string{"collector-sdk/v1alpha2", " collector-sdk/v1alpha2 "} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			instance := CollectorInstance{
				InstanceID:     "pagerduty-reference",
				CollectorKind:  scope.CollectorPagerDuty,
				Mode:           CollectorModeScheduled,
				Enabled:        true,
				ClaimsEnabled:  true,
				Configuration:  alpha2ActivationConfiguration(protocol, "process"),
				LastObservedAt: observedAt,
				CreatedAt:      observedAt,
				UpdatedAt:      observedAt,
			}

			if err := instance.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestDesiredCollectorInstanceValidateRejectsUnsupportedSDKProtocol locks the
// allowlist boundary: protocols outside v1alpha1/v1alpha2 stay rejected,
// including the padded variant.
func TestDesiredCollectorInstanceValidateRejectsUnsupportedSDKProtocol(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"collector-sdk/v9", " collector-sdk/v9 "} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			instance := DesiredCollectorInstance{
				InstanceID:    "pagerduty-reference",
				CollectorKind: scope.CollectorPagerDuty,
				Mode:          CollectorModeScheduled,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: alpha2ActivationConfiguration(protocol, "process"),
			}

			if err := instance.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want unsupported sdk_protocol error")
			}
		})
	}
}

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
