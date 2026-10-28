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
