// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestParseConfigAdmitsExplicitAlpha2 proves an explicitly configured
// collector-sdk/v1alpha2 activation parses, including a padded protocol
// variant that must normalize rather than bypass the allowlist.
func TestParseConfigAdmitsExplicitAlpha2(t *testing.T) {
	t.Parallel()

	for _, adapter := range []string{"oci", "process"} {
		for _, protocol := range []string{"collector-sdk/v1alpha2", " collector-sdk/v1alpha2 "} {
			t.Run(adapter+"/"+protocol, func(t *testing.T) {
				t.Parallel()

				raw := `{"schema_version":"eshu.component.instance.v1",` +
					`"component_id":"dev.eshu.examples.scorecard",` +
					`"component_version":"0.1.0",` +
					`"manifest_digest":"sha256:1234",` +
					`"config_handle":"component-config:abcd",` +
					`"runtime":{"sdk_protocol":"` + protocol + `","adapter":"` + adapter + `"}}`
				config, ok, err := ParseConfig(raw)
				if err != nil {
					t.Fatalf("ParseConfig() error = %v, want nil", err)
				}
				if !ok {
					t.Fatal("ParseConfig() ok = false, want true")
				}
				if strings.TrimSpace(config.Runtime.SDKProtocol) != "collector-sdk/v1alpha2" {
					t.Fatalf("ParseConfig() SDKProtocol = %q, want alpha2", config.Runtime.SDKProtocol)
				}
			})
		}
	}
}

// TestParseConfigAdmitsExplicitAlpha3 proves an explicitly configured
// collector-sdk/v1alpha3 activation parses: the seeded scanner manifests
// stamp v1alpha3, and the coordinator must accept what the seed emits
// (#9549). Mirrors TestParseConfigAdmitsExplicitAlpha2, including the
// padded variant that must normalize rather than bypass the allowlist.
func TestParseConfigAdmitsExplicitAlpha3(t *testing.T) {
	t.Parallel()

	for _, adapter := range []string{"oci", "process"} {
		for _, protocol := range []string{"collector-sdk/v1alpha3", " collector-sdk/v1alpha3 "} {
			t.Run(adapter+"/"+protocol, func(t *testing.T) {
				t.Parallel()

				raw := `{"schema_version":"eshu.component.instance.v1",` +
					`"component_id":"dev.eshu.examples.scorecard",` +
					`"component_version":"0.1.0",` +
					`"manifest_digest":"sha256:1234",` +
					`"config_handle":"component-config:abcd",` +
					`"runtime":{"sdk_protocol":"` + protocol + `","adapter":"` + adapter + `"}}`
				config, ok, err := ParseConfig(raw)
				if err != nil {
					t.Fatalf("ParseConfig() error = %v, want nil", err)
				}
				if !ok {
					t.Fatal("ParseConfig() ok = false, want true")
				}
				if strings.TrimSpace(config.Runtime.SDKProtocol) != "collector-sdk/v1alpha3" {
					t.Fatalf("ParseConfig() SDKProtocol = %q, want alpha3", config.Runtime.SDKProtocol)
				}
			})
		}
	}
}

// TestParseConfigAdmitsEveryKnownSDKProtocol is the recurrence guard for
// #9549: every protocol constant the component package defines must pass
// this package's allowlist, so a future protocol bump cannot widen the
// manifest side and miss this gate again.
func TestParseConfigAdmitsEveryKnownSDKProtocol(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{
		component.CollectorSDKProtocolV1Alpha1,
		component.CollectorSDKProtocolV1Alpha2,
		component.CollectorSDKProtocolV1Alpha3,
	} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			raw := `{"schema_version":"eshu.component.instance.v1",` +
				`"component_id":"dev.eshu.examples.scorecard",` +
				`"component_version":"0.1.0",` +
				`"manifest_digest":"sha256:1234",` +
				`"config_handle":"component-config:abcd",` +
				`"runtime":{"sdk_protocol":"` + protocol + `","adapter":"oci"}}`
			if _, ok, err := ParseConfig(raw); err != nil || !ok {
				t.Fatalf("ParseConfig() ok=%v err=%v for known protocol %q, want ok", ok, err, protocol)
			}
		})
	}
}

// TestParseConfig pins the parse and validation contract every consumer of
// this package depends on: extension.WorkPlanner.PlanComponentExtensionWork
// delegates to this exact function, and component_extension_service.go,
// pagerduty_service.go, and governance_audit.go all detect or read a
// component-extension instance through it. component_activation_config_test.go
// is the only external test importer. A caller that only needs to
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

// TestIsConfigurationMatchesParseConfigPartition pins that IsConfiguration
// treats a valid, malformed, or incomplete activation document as
// extension-hosted and blank or in-tree configuration as not, the partition
// the scheduler and the status reader share (#8951).
func TestIsConfigurationMatchesParseConfigPartition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"blank", ``, false},
		{"empty object", `{}`, false},
		{"in-tree targets", `{"scan_interval":"15m","targets":[{"site":"example"}]}`, false},
		{"incomplete activation", `{"schema_version":"eshu.component.instance.v1","component_id":"vendor.example"}`, true},
		{"wrong schema with component id", `{"schema_version":"other","component_id":"vendor.example"}`, true},
		{"malformed json", `{"component_id":`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsConfiguration(tc.raw); got != tc.want {
				t.Fatalf("IsConfiguration(%q) = %t, want %t", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseConfigComponentConfigIsOpaqueObject pins the one opaque field:
// absent or null parses to nil, any JSON object parses unchanged and
// uninterpreted, and any other JSON value fails as an activation error.
func TestParseConfigComponentConfigIsOpaqueObject(t *testing.T) {
	t.Parallel()

	base := `{"schema_version":"eshu.component.instance.v1",` +
		`"component_id":"dev.eshu.collectors.vulnerability-intelligence",` +
		`"component_version":"0.1.0","manifest_digest":"sha256:1234",` +
		`"config_handle":"component-config:abcd",` +
		`"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}`
	for _, tc := range []struct {
		name, field, want string
		wantErr           bool
	}{
		{"absent", "", "", false},
		{"null", `,"component_config":null`, "", false},
		{
			"object", `,"component_config":{"derive_from_owned_packages":{"enabled":true},"anything":[1]}`,
			`{"derive_from_owned_packages":{"enabled":true},"anything":[1]}`, false,
		},
		{"array", `,"component_config":[1]`, "", true},
		{"string", `,"component_config":"x"`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := base + tc.field + `}`
			config, ok, err := ParseConfig(raw)
			if tc.wantErr {
				if err == nil || ok {
					t.Fatalf("ParseConfig() ok=%v err=%v, want an error", ok, err)
				}
				if !IsConfiguration(raw) {
					t.Fatal("IsConfiguration() = false, want true: a malformed activation still belongs to the generic planner")
				}
				return
			}
			if err != nil || !ok {
				t.Fatalf("ParseConfig() ok=%v err=%v, want ok", ok, err)
			}
			if got := string(config.ComponentConfig); got != tc.want {
				t.Fatalf("ComponentConfig = %q, want %q", got, tc.want)
			}
		})
	}
}
