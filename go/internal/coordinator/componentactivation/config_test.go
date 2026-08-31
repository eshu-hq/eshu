// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentactivation

import "testing"

// TestParseConfig pins the parse and validation contract every consumer of
// this package depends on: componentextensionplanner.WorkPlanner.PlanComponentExtensionWork
// delegates to this exact function, and component_extension_service.go,
// pagerduty_service.go, and governance_audit.go all detect or read a
// component-extension instance through it. A caller that only needs to
// detect or exclude a component-extension instance relies on ok/err; a
// caller that needs the component identity relies on Config.ComponentID.
func TestParseConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		wantOK        bool
		wantErr       bool
		wantComponent string
	}{
		{
			name:          "blank configuration is not a component-extension instance",
			configuration: "",
			wantOK:        false,
			wantErr:       false,
		},
		{
			name:          "unrelated collector configuration is not a component-extension instance",
			configuration: `{"schema_version":"git.collector.v1","provider":"github"}`,
			wantOK:        false,
			wantErr:       false,
		},
		{
			name: "valid component-extension configuration parses ok with component identity",
			configuration: `{
				"schema_version":"eshu.component.instance.v1",
				"component_id":"dev.eshu.examples.scorecard",
				"component_version":"0.1.0",
				"manifest_digest":"sha256:1234",
				"config_handle":"component-config:abcd",
				"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
			}`,
			wantOK:        true,
			wantErr:       false,
			wantComponent: "dev.eshu.examples.scorecard",
		},
		{
			name: "component-extension configuration missing component_version is an error, not a miss",
			configuration: `{
				"schema_version":"eshu.component.instance.v1",
				"component_id":"dev.eshu.examples.scorecard",
				"manifest_digest":"sha256:1234",
				"config_handle":"component-config:abcd",
				"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}
			}`,
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "unsupported runtime.sdk_protocol is an error",
			configuration: `{
				"schema_version":"eshu.component.instance.v1",
				"component_id":"dev.eshu.examples.scorecard",
				"component_version":"0.1.0",
				"manifest_digest":"sha256:1234",
				"config_handle":"component-config:abcd",
				"runtime":{"sdk_protocol":"collector-sdk/v9","adapter":"oci"}
			}`,
			wantOK:  false,
			wantErr: true,
		},
		{
			name: "unsupported runtime.adapter is an error",
			configuration: `{
				"schema_version":"eshu.component.instance.v1",
				"component_id":"dev.eshu.examples.scorecard",
				"component_version":"0.1.0",
				"manifest_digest":"sha256:1234",
				"config_handle":"component-config:abcd",
				"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"ssh"}
			}`,
			wantOK:  false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config, ok, err := ParseConfig(tt.configuration)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ok != tt.wantOK {
				t.Fatalf("ParseConfig() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantComponent != "" && config.ComponentID != tt.wantComponent {
				t.Fatalf("ParseConfig() ComponentID = %q, want %q", config.ComponentID, tt.wantComponent)
			}
		})
	}
}
