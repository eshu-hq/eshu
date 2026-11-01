// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
)

// TestLoadConfigRefusesDistroEcosystemUnderOwnedPackages pins the operator
// outcome for #9221: an in-tree vulnerability-intelligence instance that lists
// an OS distro name under derive_from_owned_packages fails coordinator startup
// (LoadConfig, which cmd/workflow-coordinator logs as "workflow coordinator
// failed" before exiting) with the instance id, the section, the index, the
// offending value, and the accepted set. The same name under
// derive_from_installed_evidence loads.
func TestLoadConfigRefusesDistroEcosystemUnderOwnedPackages(t *testing.T) {
	t.Parallel()

	owned := `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm","debian"]}}`
	_, err := LoadConfig(staticActivationEnv(testVulnerabilityKind, true, owned))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want the unsupported ecosystem refusal")
	}
	for _, want := range []string{
		`workflow coordinator collector instance "vuln-intel-static"`,
		`derive_from_owned_packages.ecosystems[1]: unsupported ecosystem "debian"`,
		`(supported: cargo, composer, go, hex, maven, npm, nuget, pub, pypi, rubygems, swift)`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("LoadConfig() error = %q, want it to contain %q", err.Error(), want)
		}
	}
	installed := `{"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["alpine","debian","ubuntu","rocky","alma"]}}`
	if _, err := LoadConfig(staticActivationEnv(testVulnerabilityKind, true, installed)); err != nil {
		t.Fatalf("LoadConfig() installed distro list error = %v, want nil", err)
	}
}
