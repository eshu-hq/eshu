// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComponentErrorClasses(t *testing.T) {
	t.Parallel()

	invalidManifestPath := writeManifest(t, "apiVersion: nope\nkind: ComponentPackage\n")
	if _, err := LoadManifest(invalidManifestPath); ErrorCodeOf(err) != ErrorCodeInvalidManifest {
		t.Fatalf("LoadManifest() code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeInvalidManifest, err)
	}

	incompatible := validManifest()
	incompatible.Spec.CompatibleCore = ">=0.1.0 <0.2.0"
	incompatibleResult := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.0.5",
	}.Verify(incompatible)
	if got, want := incompatibleResult.Code, ErrorCodeIncompatibleCore; got != want {
		t.Fatalf("incompatible Verify().Code = %q, want %q", got, want)
	}

	revokedResult := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedIDs:        []string{"dev.eshu.collector.aws"},
		CoreVersion:       "v0.0.5",
	}.Verify(validManifest())
	if got, want := revokedResult.Code, ErrorCodeRevokedPackage; got != want {
		t.Fatalf("revoked Verify().Code = %q, want %q", got, want)
	}

	untrustedResult := Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"someone-else"},
		CoreVersion:       "v0.0.5",
	}.Verify(validManifest())
	if got, want := untrustedResult.Code, ErrorCodeUntrustedPublisher; got != want {
		t.Fatalf("untrusted Verify().Code = %q, want %q", got, want)
	}

	corruptHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(corruptHome, registryFileName), []byte("{"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	if _, err := NewRegistry(corruptHome).List(); ErrorCodeOf(err) != ErrorCodeCorruptedRegistryState {
		t.Fatalf("List() code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeCorruptedRegistryState, err)
	}

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{InstanceID: "prod-aws"}); ErrorCodeOf(err) != ErrorCodeDuplicateActivation {
		t.Fatalf("duplicate Enable() code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeDuplicateActivation, err)
	}
	if err := registry.Uninstall("dev.eshu.collector.aws", "0.1.0"); ErrorCodeOf(err) != ErrorCodeActiveUninstall {
		t.Fatalf("active Uninstall() code = %q, want %q; err=%v", ErrorCodeOf(err), ErrorCodeActiveUninstall, err)
	}
}
