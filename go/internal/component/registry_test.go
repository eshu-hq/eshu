package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryInstallListsInstalledComponent(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())

	installed, err := registry.Install(manifestPath, VerificationResult{
		Allowed:   true,
		Mode:      TrustModeAllowlist,
		Component: "dev.eshu.collector.aws",
		Publisher: "eshu-hq",
		Version:   "0.1.0",
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if got, want := installed.ID, "dev.eshu.collector.aws"; got != want {
		t.Fatalf("Installed.ID = %q, want %q", got, want)
	}
	if installed.ManifestDigest == "" {
		t.Fatal("Installed.ManifestDigest is empty, want digest")
	}

	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if got, want := len(components), 1; got != want {
		t.Fatalf("len(List()) = %d, want %d", got, want)
	}
	if got, want := components[0].ID, "dev.eshu.collector.aws"; got != want {
		t.Fatalf("List()[0].ID = %q, want %q", got, want)
	}
}

func TestRegistryInstallRejectsUnverifiedComponent(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())

	_, err := registry.Install(manifestPath, VerificationResult{
		Allowed: false,
		Mode:    TrustModeDisabled,
		Reason:  "component trust policy is disabled",
	})
	if err == nil {
		t.Fatal("Install() error = nil, want unverified component error")
	}
	if !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("Install() error = %v, want not verified error", err)
	}
}

func TestRegistryInstallRejectsMismatchedVerification(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())

	_, err := registry.Install(manifestPath, VerificationResult{
		Allowed:   true,
		Mode:      TrustModeAllowlist,
		Component: "dev.eshu.collector.github",
		Publisher: "eshu-hq",
		Version:   "0.1.0",
	})
	if err == nil {
		t.Fatal("Install() error = nil, want verification mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match manifest") {
		t.Fatalf("Install() error = %v, want mismatch error", err)
	}
}

func TestRegistryEnableAndDisableActivation(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}

	activation, err := registry.Enable("dev.eshu.collector.aws", Activation{
		InstanceID:    "prod-aws",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    filepath.Join(t.TempDir(), "aws.yaml"),
	})
	if err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	if got, want := activation.InstanceID, "prod-aws"; got != want {
		t.Fatalf("Activation.InstanceID = %q, want %q", got, want)
	}

	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if got, want := len(components[0].Activations), 1; got != want {
		t.Fatalf("activation count = %d, want %d", got, want)
	}

	if err := registry.Disable("dev.eshu.collector.aws", "prod-aws"); err != nil {
		t.Fatalf("Disable() error = %v, want nil", err)
	}
	components, err = registry.List()
	if err != nil {
		t.Fatalf("List() after Disable() error = %v, want nil", err)
	}
	if got, want := len(components[0].Activations), 0; got != want {
		t.Fatalf("activation count after disable = %d, want %d", got, want)
	}
}

func TestRegistryEnableUsesNewestSemanticVersion(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	v090Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.9.0"))
	v0100Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.10.0"))
	if _, err := registry.Install(v090Path, allowedVerificationForVersion("0.9.0")); err != nil {
		t.Fatalf("Install(0.9.0) error = %v, want nil", err)
	}
	if _, err := registry.Install(v0100Path, allowedVerificationForVersion("0.10.0")); err != nil {
		t.Fatalf("Install(0.10.0) error = %v, want nil", err)
	}

	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	for _, installed := range components {
		if installed.Version == "0.10.0" && len(installed.Activations) != 1 {
			t.Fatalf("0.10.0 activations = %d, want 1", len(installed.Activations))
		}
		if installed.Version == "0.9.0" && len(installed.Activations) != 0 {
			t.Fatalf("0.9.0 activations = %d, want 0", len(installed.Activations))
		}
	}
}

func TestRegistryEnableRejectsExistingActivationOnOlderVersion(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	v090Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.9.0"))
	v0100Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.10.0"))
	if _, err := registry.Install(v090Path, allowedVerificationForVersion("0.9.0")); err != nil {
		t.Fatalf("Install(0.9.0) error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); err != nil {
		t.Fatalf("Enable(0.9.0) error = %v, want nil", err)
	}
	if _, err := registry.Install(v0100Path, allowedVerificationForVersion("0.10.0")); err != nil {
		t.Fatalf("Install(0.10.0) error = %v, want nil", err)
	}

	_, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"})
	if err == nil {
		t.Fatal("Enable() error = nil, want existing activation error")
	}
	if !strings.Contains(err.Error(), "already enabled") {
		t.Fatalf("Enable() error = %v, want already enabled error", err)
	}
}

func TestRegistryDisableFindsActivationAcrossVersions(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	v090Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.9.0"))
	v0100Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.10.0"))
	if _, err := registry.Install(v090Path, allowedVerificationForVersion("0.9.0")); err != nil {
		t.Fatalf("Install(0.9.0) error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); err != nil {
		t.Fatalf("Enable(0.9.0) error = %v, want nil", err)
	}
	if _, err := registry.Install(v0100Path, allowedVerificationForVersion("0.10.0")); err != nil {
		t.Fatalf("Install(0.10.0) error = %v, want nil", err)
	}

	if err := registry.Disable("dev.eshu.collector.aws", "prod-aws"); err != nil {
		t.Fatalf("Disable() error = %v, want nil", err)
	}
	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	for _, installed := range components {
		if len(installed.Activations) != 0 {
			t.Fatalf("%s activations = %d, want 0", installed.Version, len(installed.Activations))
		}
	}
}

func TestRegistryListSortsVersionsSemantically(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	v090Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.9.0"))
	v0100Path := writeManifest(t, strings.ReplaceAll(validManifestYAML(), "version: 0.1.0", "version: 0.10.0"))
	if _, err := registry.Install(v0100Path, allowedVerificationForVersion("0.10.0")); err != nil {
		t.Fatalf("Install(0.10.0) error = %v, want nil", err)
	}
	if _, err := registry.Install(v090Path, allowedVerificationForVersion("0.9.0")); err != nil {
		t.Fatalf("Install(0.9.0) error = %v, want nil", err)
	}

	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	got := []string{components[0].Version, components[1].Version}
	want := []string{"0.9.0", "0.10.0"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("List() versions = %v, want %v", got, want)
	}
}

func TestRegistryInstallRejectsChangedDigestForActiveVersion(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	changedManifestPath := writeManifest(
		t,
		strings.ReplaceAll(
			validManifestYAML(),
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		),
	)

	_, err := registry.Install(changedManifestPath, allowedVerification())
	if err == nil {
		t.Fatal("Install() error = nil, want active reinstall error")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Fatalf("Install() error = %v, want active error", err)
	}
}

func TestRegistryUninstallIgnoresManifestPathFromRegistryState(t *testing.T) {
	t.Parallel()

	registryHome := t.TempDir()
	outsideDir := t.TempDir()
	outsideManifest := filepath.Join(outsideDir, "manifest.yaml")
	if err := os.WriteFile(outsideManifest, []byte("outside"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	registry := NewRegistry(registryHome)
	state := registryState{
		Components: []InstalledComponent{
			{
				ID:           "dev.eshu.collector.aws",
				Name:         "AWS cloud scanner",
				Publisher:    "eshu-hq",
				Version:      "0.1.0",
				ManifestPath: outsideManifest,
				Verified:     true,
				TrustMode:    TrustModeAllowlist,
			},
		},
	}
	if err := registry.save(state); err != nil {
		t.Fatalf("save() error = %v, want nil", err)
	}

	if err := registry.Uninstall("dev.eshu.collector.aws", "0.1.0"); err != nil {
		t.Fatalf("Uninstall() error = %v, want nil", err)
	}
	if _, err := os.Stat(outsideManifest); err != nil {
		t.Fatalf("outside manifest stat error = %v, want file preserved", err)
	}
}

func TestRegistryDisableRejectsMissingActivation(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}

	err := registry.Disable("dev.eshu.collector.aws", "prod-aws")
	if err == nil {
		t.Fatal("Disable() error = nil, want missing activation error")
	}
	if !strings.Contains(err.Error(), "activation") {
		t.Fatalf("Disable() error = %v, want activation error", err)
	}
}

func TestRegistryDisableRejectsInvalidInstanceID(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())

	err := registry.Disable("dev.eshu.collector.aws", "../../prod")
	if err == nil {
		t.Fatal("Disable() error = nil, want instance validation error")
	}
	if !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("Disable() error = %v, want instance_id error", err)
	}
}

func TestRegistryUninstallRejectsActiveComponent(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws", Mode: "scheduled"}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}

	err := registry.Uninstall("dev.eshu.collector.aws", "0.1.0")
	if err == nil {
		t.Fatal("Uninstall() error = nil, want active component error")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Fatalf("Uninstall() error = %v, want active component error", err)
	}
}

func TestRegistryUninstallRemovesInactiveComponent(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}

	if err := registry.Uninstall("dev.eshu.collector.aws", "0.1.0"); err != nil {
		t.Fatalf("Uninstall() error = %v, want nil", err)
	}
	components, err := registry.List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if got := len(components); got != 0 {
		t.Fatalf("len(List()) = %d, want 0", got)
	}
}

func allowedVerification() VerificationResult {
	return allowedVerificationForVersion("0.1.0")
}

func allowedVerificationForVersion(version string) VerificationResult {
	return VerificationResult{
		Allowed:   true,
		Mode:      TrustModeAllowlist,
		Component: "dev.eshu.collector.aws",
		Publisher: "eshu-hq",
		Version:   version,
	}
}
