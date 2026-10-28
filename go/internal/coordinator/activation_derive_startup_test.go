// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const testVulnerabilityActivationComponentID = "dev.eshu.examples.vuln-intel"

// vulnerabilityActivationFileYAML renders a vulnerability activation file with
// the given host source system and scope kind and a raw YAML config block.
func vulnerabilityActivationFileYAML(hostSource, hostKind, configBlock string) string {
	return fmt.Sprintf(`host:
  sourceSystem: %s
  scope:
    id: vuln-intel://cisa/kev
    kind: %s
config:
%s`, hostSource, hostKind, configBlock)
}

// blankOwnedEcosystemsBlock enables owned-package derivation with no
// ecosystems list, beside an installed-evidence section whose explicit
// "debian" value must never reach an error or a log line.
const blankOwnedEcosystemsBlock = `  derive_from_owned_packages:
    enabled: true
    sources: [osv]
  derive_from_installed_evidence:
    enabled: true
    sources: [osv]
    ecosystems: [debian]
`

const explicitOwnedBlock = `  derive_from_owned_packages:
    enabled: true
    sources: [osv]
    ecosystems: [npm]
`

// loadConfigWithActivation installs one fixture component of the given
// collector kind, enables one claims-enabled activation whose file is raw, and
// runs the coordinator's startup LoadConfig over it.
func loadConfigWithActivation(t *testing.T, collectorKind, raw string) (Config, error) {
	t.Helper()

	home := t.TempDir()
	installQuotaComponent(t, home, testVulnerabilityActivationComponentID, collectorKind, 24)
	if _, err := component.NewRegistry(home).Enable(testVulnerabilityActivationComponentID, component.Activation{
		InstanceID:    "vuln-intel-activation",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeActivationFile(t, raw),
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	return LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COMPONENT_ALLOW_IDS": testVulnerabilityActivationComponentID,
	}))
}

// TestLoadConfigRefusesActivationDeriveBlockWithBlankList is the #9311 F1
// startup RED: a vulnerability activation whose enabled derive section leaves
// a list blank fails LoadConfig, before any durable write, with the same
// DeriveListNotExplicitError the reconcile hook returns. The error names the
// instance, section, and list and carries no configured value.
func TestLoadConfigRefusesActivationDeriveBlockWithBlankList(t *testing.T) {
	t.Parallel()

	_, err := loadConfigWithActivation(t, testVulnerabilityKind,
		vulnerabilityActivationFileYAML(testVulnerabilityKind, testVulnerabilityKind, blankOwnedEcosystemsBlock))
	var listErr workflow.DeriveListNotExplicitError
	if !errors.As(err, &listErr) {
		t.Fatalf("LoadConfig() error = %v, want DeriveListNotExplicitError", err)
	}
	if listErr.Section != "derive_from_owned_packages" || listErr.List != "ecosystems" {
		t.Fatalf("DeriveListNotExplicitError = %+v, want derive_from_owned_packages.ecosystems", listErr)
	}
	if !strings.Contains(err.Error(), `"vuln-intel-activation"`) {
		t.Fatalf("LoadConfig() error = %v, want the instance id", err)
	}
	if strings.Contains(err.Error(), "debian") {
		t.Fatalf("LoadConfig() error = %v, want no configured value", err)
	}
}

// TestLoadConfigAcceptsActivationDeriveBlockWithExplicitLists is the GREEN
// control: explicit lists start up and carry the block.
func TestLoadConfigAcceptsActivationDeriveBlockWithExplicitLists(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigWithActivation(t, testVulnerabilityKind,
		vulnerabilityActivationFileYAML(testVulnerabilityKind, testVulnerabilityKind, explicitOwnedBlock))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	if !strings.Contains(cfg.CollectorInstances[0].Configuration, `"component_config":`) {
		t.Fatalf("configuration = %s, want the carried config block", cfg.CollectorInstances[0].Configuration)
	}
}

// TestLoadConfigRefusesActivationDeriveBlockWithHostScopeKindMismatch: an
// enabled derivation whose host scope kind or source system is not
// vulnerability_intelligence fails startup with the reconcile hook's text.
func TestLoadConfigRefusesActivationDeriveBlockWithHostScopeKindMismatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, source, kind string }{
		{"scope kind", testVulnerabilityKind, "repository"},
		{"source system", "osv", testVulnerabilityKind},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := loadConfigWithActivation(t, testVulnerabilityKind,
				vulnerabilityActivationFileYAML(tc.source, tc.kind, explicitOwnedBlock))
			if err == nil || !strings.Contains(err.Error(), `must both be "vulnerability_intelligence"`) {
				t.Fatalf("LoadConfig() error = %v, want host scope kind/source system refusal", err)
			}
			if !strings.Contains(err.Error(), `"vuln-intel-activation"`) {
				t.Fatalf("LoadConfig() error = %v, want the instance id", err)
			}
		})
	}
}

// TestLoadConfigLeavesOtherKindsDeriveBlockUnvalidated proves the startup
// check runs only for the kind that carries the block: a scorecard activation
// with the same bad block starts up and stores no component_config.
func TestLoadConfigLeavesOtherKindsDeriveBlockUnvalidated(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfigWithActivation(t, "scorecard",
		vulnerabilityActivationFileYAML("openssf-scorecard", "repository", blankOwnedEcosystemsBlock))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil for a scorecard activation", err)
	}
	if strings.Contains(cfg.CollectorInstances[0].Configuration, "component_config") {
		t.Fatalf("configuration = %s, want no component_config for a scorecard activation", cfg.CollectorInstances[0].Configuration)
	}
}

// TestActivationDeriveRulesMatchAtStartupAndReconcile proves one rule set: the
// same bad block fails the startup readback and the reconcile hook with the
// same innermost error (type and text).
func TestActivationDeriveRulesMatchAtStartupAndReconcile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, source, kind, yamlBlock, jsonBlock string
	}{
		{
			name:   "blank list",
			source: testVulnerabilityKind, kind: testVulnerabilityKind,
			yamlBlock: blankOwnedEcosystemsBlock,
			jsonBlock: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"]},` +
				`"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["debian"]}}`,
		},
		{
			name:   "host scope kind mismatch",
			source: testVulnerabilityKind, kind: "repository",
			yamlBlock: explicitOwnedBlock,
			jsonBlock: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeActivationFile(t, vulnerabilityActivationFileYAML(tc.source, tc.kind, tc.yamlBlock))
			_, startupErr := desiredInstancesForComponent(
				testActivationReadbackEntry(path), testActivationManifest(testVulnerabilityKind))

			now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
			instance := testVulnerabilityActivationInstance(now,
				testVulnerabilityActivationConfiguration(tc.jsonBlock, tc.source, tc.kind))
			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			service := testVulnerabilityActivationService(t, now, instance, store, ownedViteReader(), `{"mode":"broad"}`)
			reconcileErr := service.scheduleComponentExtensionWork(context.Background(), now, []workflow.CollectorInstance{instance})

			if startupErr == nil || reconcileErr == nil {
				t.Fatalf("startup error = %v, reconcile error = %v, want both refused", startupErr, reconcileErr)
			}
			startupRoot, reconcileRoot := innermostError(startupErr), innermostError(reconcileErr)
			if fmt.Sprintf("%T", startupRoot) != fmt.Sprintf("%T", reconcileRoot) ||
				startupRoot.Error() != reconcileRoot.Error() {
				t.Fatalf("innermost startup error %T(%v) != reconcile error %T(%v)",
					startupRoot, startupRoot, reconcileRoot, reconcileRoot)
			}
			for _, err := range []error{startupErr, reconcileErr} {
				var listErr workflow.DeriveListNotExplicitError
				var hostErr activationHostMismatchError
				if !errors.As(err, &listErr) && !errors.As(err, &hostErr) {
					t.Fatalf("error = %v, want DeriveListNotExplicitError or activationHostMismatchError", err)
				}
			}
		})
	}
}

func innermostError(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// staticActivationEnv returns a coordinator environment whose only collector
// instance arrives through ESHU_COLLECTOR_INSTANCES_JSON, the static path, with
// configuration as its raw activation document.
func staticActivationEnv(collectorKind string, enabled bool, configuration string) func(string) string {
	instances := fmt.Sprintf(`[{"instance_id":"vuln-intel-static","collector_kind":%q,`+
		`"mode":"scheduled","enabled":%t,"claims_enabled":%t,"configuration":%s}]`,
		collectorKind, enabled, enabled, configuration)
	values := map[string]string{
		"ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE": "active",
		"ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":  "true",
		"ESHU_COLLECTOR_INSTANCES_JSON":             instances,
	}
	if !enabled {
		values["ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE"] = "dark"
	}
	return func(key string) string { return values[key] }
}

// staticDeriveCases are bad derive blocks a static activation document can
// carry: a blank list, an invalid value, and a host scope kind mismatch.
var staticDeriveCases = []struct {
	name, source, kind, block string
	check                     func(t *testing.T, err error)
}{
	{
		name: "blank list", source: testVulnerabilityKind, kind: testVulnerabilityKind,
		block: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"]},` +
			`"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["debian"]}}`,
		check: func(t *testing.T, err error) {
			t.Helper()
			var listErr workflow.DeriveListNotExplicitError
			if !errors.As(err, &listErr) || listErr.Section != "derive_from_owned_packages" || listErr.List != "ecosystems" {
				t.Fatalf("error = %v, want DeriveListNotExplicitError for derive_from_owned_packages.ecosystems", err)
			}
			if strings.Contains(err.Error(), "debian") {
				t.Fatalf("error = %v, want no configured value", err)
			}
		},
	},
	{
		name: "invalid value", source: testVulnerabilityKind, kind: testVulnerabilityKind,
		block: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["bogus"]}}`,
		check: func(t *testing.T, err error) {
			t.Helper()
			if err == nil || !strings.Contains(err.Error(), `unsupported ecosystem "bogus"`) {
				t.Fatalf("error = %v, want the unsupported ecosystem refusal", err)
			}
		},
	},
	{
		name: "host scope kind mismatch", source: testVulnerabilityKind, kind: "repository",
		block: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`,
		check: func(t *testing.T, err error) {
			t.Helper()
			var hostErr activationHostMismatchError
			if !errors.As(err, &hostErr) {
				t.Fatalf("error = %v, want activationHostMismatchError", err)
			}
		},
	},
}

// TestStaticActivationDeriveBlockFailsStartup is the #9311 F4 RED: a
// vulnerability activation document that arrives through
// ESHU_COLLECTOR_INSTANCES_JSON with a bad derive block fails LoadConfig and
// Config.Validate, before any durable write, with the same innermost error
// (type and text) the reconcile hook returns. A disabled instance fails too,
// the same as an in-tree instance whose derive config is invalid.
func TestStaticActivationDeriveBlockFailsStartup(t *testing.T) {
	t.Parallel()

	for _, tc := range staticDeriveCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configuration := testVulnerabilityActivationConfiguration(tc.block, tc.source, tc.kind)

			cfg, loadErr := LoadConfig(staticActivationEnv(testVulnerabilityKind, true, configuration))
			if loadErr == nil {
				t.Fatalf("LoadConfig() config = %+v, error = nil, want the derive refusal", cfg.CollectorInstances)
			}
			tc.check(t, loadErr)
			if !strings.Contains(loadErr.Error(), `"vuln-intel-static"`) {
				t.Fatalf("LoadConfig() error = %v, want the instance id", loadErr)
			}
			_, disabledErr := LoadConfig(staticActivationEnv(testVulnerabilityKind, false, configuration))
			tc.check(t, disabledErr)

			now := time.Date(2026, time.October, 28, 1, 0, 0, 0, time.UTC)
			instance := testVulnerabilityActivationInstance(now, configuration)
			validateErr := Config{
				DeploymentMode: deploymentModeActive,
				ClaimsEnabled:  true,
				CollectorInstances: []workflow.DesiredCollectorInstance{{
					InstanceID: instance.InstanceID, CollectorKind: instance.CollectorKind, Mode: instance.Mode,
					Enabled: true, ClaimsEnabled: true, Configuration: configuration,
				}},
			}.Validate()
			tc.check(t, validateErr)

			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			service := testVulnerabilityActivationService(t, now, instance, store, ownedViteReader(), `{"mode":"broad"}`)
			reconcileErr := service.scheduleComponentExtensionWork(context.Background(), now, []workflow.CollectorInstance{instance})
			tc.check(t, reconcileErr)
			startupRoot, reconcileRoot := innermostError(loadErr), innermostError(reconcileErr)
			if fmt.Sprintf("%T", startupRoot) != fmt.Sprintf("%T", reconcileRoot) ||
				startupRoot.Error() != reconcileRoot.Error() {
				t.Fatalf("innermost startup error %T(%v) != reconcile error %T(%v)",
					startupRoot, startupRoot, reconcileRoot, reconcileRoot)
			}
		})
	}
}

// TestStaticActivationDeriveBlockGreenAndOtherKinds is the control: a static
// vulnerability activation with explicit, valid lists starts up, and a
// scorecard activation carrying a bad block is not validated by this rule.
func TestStaticActivationDeriveBlockGreenAndOtherKinds(t *testing.T) {
	t.Parallel()

	good := testVulnerabilityActivationConfiguration(
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`,
		testVulnerabilityKind, testVulnerabilityKind)
	if _, err := LoadConfig(staticActivationEnv(testVulnerabilityKind, true, good)); err != nil {
		t.Fatalf("LoadConfig(explicit lists) error = %v, want nil", err)
	}
	bad := testVulnerabilityActivationConfiguration(staticDeriveCases[0].block, "openssf-scorecard", "repository")
	if _, err := LoadConfig(staticActivationEnv("scorecard", true, bad)); err != nil {
		t.Fatalf("LoadConfig(scorecard) error = %v, want nil for a non-vulnerability kind", err)
	}
}
