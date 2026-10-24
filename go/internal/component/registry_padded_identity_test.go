// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"
	"time"
)

const (
	paddedTestComponentID = "dev.eshu.collector.aws"
	paddedTestVersion     = "0.1.0"
)

// legacyPaddedRegistryCases seed registry.json the way an older build could
// have written it: that build trimmed identifiers only to validate them, then
// stored the raw padded bytes. Each row names the stored component ID and
// instance_id exactly as they sit on disk.
var legacyPaddedRegistryCases = []struct {
	name        string
	componentID string
	instanceID  string
}{
	{name: "padded instance_id", componentID: paddedTestComponentID, instanceID: " prod-aws "},
	{name: "padded component id", componentID: " " + paddedTestComponentID + " ", instanceID: "prod-aws"},
	{name: "padded component id and instance_id", componentID: "\t" + paddedTestComponentID, instanceID: "prod-aws\n"},
}

// seedLegacyPaddedRegistry writes a registry holding one installed component
// with one activation under the given stored identifiers.
func seedLegacyPaddedRegistry(t *testing.T, componentID, instanceID string) Registry {
	t.Helper()

	registry := NewRegistry(t.TempDir())
	state := registryState{
		Components: []InstalledComponent{
			{
				ID:        componentID,
				Name:      "AWS cloud scanner",
				Publisher: "eshu-hq",
				Version:   paddedTestVersion,
				Verified:  true,
				TrustMode: TrustModeAllowlist,
				Activations: []Activation{
					{InstanceID: instanceID, Mode: "manual", EnabledAt: time.Now().UTC()},
				},
			},
		},
	}
	if err := registry.save(state); err != nil {
		t.Fatalf("save() error = %v, want nil", err)
	}
	return registry
}

// TestRegistryDisableAndUninstallRecoverLegacyPaddedEntries keeps an operator
// able to clear an entry that an older build stored with padded identifiers.
// Disable matches the stored component ID and instance_id exactly and does not
// demand the canonical form, and Uninstall refuses an active version, so a
// Disable that rejects the stored form would leave the entry unrecoverable
// through the CLI.
func TestRegistryDisableAndUninstallRecoverLegacyPaddedEntries(t *testing.T) {
	t.Parallel()

	for _, tc := range legacyPaddedRegistryCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			registry := seedLegacyPaddedRegistry(t, tc.componentID, tc.instanceID)

			if err := registry.Disable(tc.componentID, tc.instanceID); err != nil {
				t.Errorf("Disable(%q, %q) error = %v, want nil for the stored padded form", tc.componentID, tc.instanceID, err)
			}
			if err := registry.Uninstall(tc.componentID, paddedTestVersion); err != nil {
				t.Fatalf("Uninstall(%q, %q) error = %v, want nil once the activation is disabled", tc.componentID, paddedTestVersion, err)
			}
			components, err := registry.List()
			if err != nil {
				t.Fatalf("List() error = %v, want nil", err)
			}
			if got := len(components); got != 0 {
				t.Fatalf("len(List()) = %d, want 0 after Disable and Uninstall", got)
			}
		})
	}
}

// TestRegistryDisableMatchesStoredIdentifiersExactly pins that Disable does not
// trim to find a legacy entry: the canonical spelling of a padded stored
// instance_id or component ID is a different key and finds nothing.
func TestRegistryDisableMatchesStoredIdentifiersExactly(t *testing.T) {
	t.Parallel()

	registry := seedLegacyPaddedRegistry(t, " "+paddedTestComponentID+" ", " prod-aws ")

	// The lookup fails on a missing key, not on validation: a canonical
	// spelling is a valid identifier that matches nothing stored.
	if err := registry.Disable(" "+paddedTestComponentID+" ", "prod-aws"); ErrorCodeOf(err) != ErrorCodeNotInstalled {
		t.Fatalf("Disable() with the canonical instance_id code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeNotInstalled, err)
	}
	if err := registry.Disable(paddedTestComponentID, " prod-aws "); ErrorCodeOf(err) != ErrorCodeNotInstalled {
		t.Fatalf("Disable() with the canonical component id code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeNotInstalled, err)
	}
	if err := registry.Uninstall(" "+paddedTestComponentID+" ", paddedTestVersion); ErrorCodeOf(err) != ErrorCodeActiveUninstall {
		t.Fatalf("Uninstall() code = %q, want %q while the activation remains; err=%v", ErrorCodeOf(err), ErrorCodeActiveUninstall, err)
	}
}

// TestRegistryEnableRejectsPaddedIdentifiers pins that new activations stay
// canonical. A padded instance_id used to be stored raw and then missed exact
// lookups; Enable and PlanEnable refuse it with invalid_input and the field
// name, and write nothing.
func TestRegistryEnableRejectsPaddedIdentifiers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		field       string
		componentID string
		activation  Activation
	}{
		{name: "leading space instance_id", field: "instance_id", componentID: paddedTestComponentID, activation: Activation{InstanceID: " prod-aws"}},
		{name: "trailing space instance_id", field: "instance_id", componentID: paddedTestComponentID, activation: Activation{InstanceID: "prod-aws "}},
		{name: "trailing newline instance_id", field: "instance_id", componentID: paddedTestComponentID, activation: Activation{InstanceID: "prod-aws\n"}},
		{name: "no-break space instance_id", field: "instance_id", componentID: paddedTestComponentID, activation: Activation{InstanceID: " prod-aws"}},
		{name: "leading space mode", field: "mode", componentID: paddedTestComponentID, activation: Activation{InstanceID: "prod-aws", Mode: " scheduled"}},
		{name: "trailing space mode", field: "mode", componentID: paddedTestComponentID, activation: Activation{InstanceID: "prod-aws", Mode: "scheduled "}},
		{name: "leading space component id", field: "component id", componentID: " " + paddedTestComponentID, activation: Activation{InstanceID: "prod-aws"}},
		{name: "trailing space component id", field: "component id", componentID: paddedTestComponentID + " ", activation: Activation{InstanceID: "prod-aws"}},
	}

	for _, tc := range cases {
		for _, call := range []string{"Enable", "PlanEnable"} {
			t.Run(call+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				registry := NewRegistry(t.TempDir())
				if _, err := registry.Install(writeManifest(t, validManifestYAML()), allowedVerification()); err != nil {
					t.Fatalf("Install() error = %v, want nil", err)
				}

				var err error
				if call == "Enable" {
					_, err = registry.Enable(tc.componentID, tc.activation)
				} else {
					_, err = registry.PlanEnable(tc.componentID, tc.activation)
				}
				if err == nil {
					t.Fatalf("%s() error = nil, want a padded %s error", call, tc.field)
				}
				if got := ErrorCodeOf(err); got != ErrorCodeInvalidInput {
					t.Fatalf("%s() code = %q, want %q; err=%v", call, got, ErrorCodeInvalidInput, err)
				}
				if !strings.Contains(err.Error(), tc.field) ||
					!strings.Contains(err.Error(), "must be canonical without surrounding whitespace") {
					t.Fatalf("%s() error = %v, want the canonical-form message for %s", call, err, tc.field)
				}
				components, listErr := registry.List()
				if listErr != nil {
					t.Fatalf("List() error = %v, want nil", listErr)
				}
				if got := len(components[0].Activations); got != 0 {
					t.Fatalf("activation count = %d after a rejected %s, want 0", got, call)
				}
			})
		}
	}
}
