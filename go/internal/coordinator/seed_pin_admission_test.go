// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// installScannerActivation installs one scanner manifest with its
// RequiredGrants recorded and one claims-enabled activation, mirroring the
// chart reconcile state. imageOverride replaces the artifact image before
// install (empty keeps the platform pin).
func installScannerActivation(t *testing.T, kind scannercomponent.AnalyzerKind, imageOverride string) string {
	t.Helper()

	manifest, err := scannercomponent.ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v", kind, err)
	}
	if imageOverride != "" {
		manifest.Spec.Artifacts[0].Image = imageOverride
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal scanner manifest error = %v", err)
	}
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write scanner manifest: %v", err)
	}
	home := filepath.Join(dir, "components")
	registry := component.NewRegistry(home)
	grants := scannercomponent.RequiredGrants(manifest)
	for _, grant := range grants {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant(%q) error = %v", grant.Kind, err)
		}
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "v0.1.0",
	}
	// The bare loader rejects core-owned kinds without grants, so verify
	// the in-memory manifest against the recorded grant set directly.
	verification := policy.VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	configPath := filepath.Join(dir, "scanner.yaml")
	if err := os.WriteFile(configPath, []byte("process:\n  command: /usr/local/bin/scanner-collector\n"), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "scanner-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return home
}

func scannerCoordinatorEnv(home, componentID string) func(string) string {
	return componentCoordinatorEnv(home, map[string]string{
		"ESHU_COMPONENT_ALLOW_IDS": componentID,
	})
}

// TestLoadConfigPlansPinnedScannerActivation proves the coordinator planner
// admits a seeded scanner manifest whose artifact equals the platform pin
// (#8878).
func TestLoadConfigPlansPinnedScannerActivation(t *testing.T) {
	t.Parallel()

	home := installScannerActivation(t, scannercomponent.AnalyzerImageUnpacking, "")

	cfg, err := LoadConfig(scannerCoordinatorEnv(home, scannercomponent.IDImage))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil for the pinned artifact", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	instance := cfg.CollectorInstances[0]
	if got, want := instance.InstanceID, "scanner-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
	if got, want := instance.CollectorKind, scope.CollectorKind("scanner_worker"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
}

// TestLoadConfigDeniesDriftedScannerActivation proves the coordinator
// planner denies a seeded scanner manifest whose artifact differs from the
// platform pin before any instance is planned, without echoing the image
// (#8878).
func TestLoadConfigDeniesDriftedScannerActivation(t *testing.T) {
	t.Parallel()

	pinned, err := scannercomponent.ManifestFor(scannercomponent.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	drifted := "ghcr.io/eshu-hq/scanner-image@sha256:" + strings.Repeat("c", 64)
	if drifted == pinned.Spec.Artifacts[0].Image {
		t.Fatal("drifted seed image equals the pin, want a differing ref")
	}
	home := installScannerActivation(t, scannercomponent.AnalyzerImageUnpacking, drifted)

	_, err = LoadConfig(scannerCoordinatorEnv(home, scannercomponent.IDImage))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want denial for a drifted scanner artifact")
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("LoadConfig() error = %q, want it to name the pinned platform image", err)
	}
	if strings.Contains(err.Error(), drifted) {
		t.Fatalf("LoadConfig() error = %q, must not echo the image value", err)
	}
}

// installSatelliteActivation installs one satellite manifest with its
// documentation grants recorded and one claims-enabled activation, mirroring
// the chart reconcile state. imageOverride replaces every artifact image
// before install (empty keeps the registry pins), so a drifted install
// denies on any host platform.
func installSatelliteActivation(t *testing.T, id, imageOverride string) string {
	t.Helper()

	pins := scannercomponent.SatelliteSeedPins()[id]
	if len(pins) == 0 {
		t.Fatalf("SatelliteSeedPins()[%q] is empty, want one pin per platform", id)
	}
	manifest := component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
	}
	manifest.Metadata.ID = id
	manifest.Metadata.Name = "Seeded satellite under test"
	manifest.Metadata.Publisher = "eshu-hq"
	manifest.Metadata.Version = "0.1.0"
	manifest.Spec.CompatibleCore = ">=0.0.5 <0.2.0"
	manifest.Spec.ComponentType = component.ComponentTypeCollector
	manifest.Spec.CollectorKinds = []string{"documentation"}
	manifest.Spec.Runtime.SDKProtocol = "collector-sdk/v1alpha1"
	manifest.Spec.Runtime.Adapter = component.RuntimeAdapterProcess
	for platform, image := range pins {
		manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, component.Artifact{
			Platform: platform,
			Image:    image,
		})
	}
	manifest.Spec.EmittedFacts = []component.FactFamily{
		{Kind: "documentation_document", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"observed"}},
		{Kind: "documentation_section", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []string{"observed"}},
	}
	manifest.Spec.ConsumerContracts.Reducer.Phases = []string{"documentation_materialization"}
	manifest.Spec.Telemetry.MetricsPrefix = "eshu_dp_seeded_satellite_test_"
	if imageOverride != "" {
		for i := range manifest.Spec.Artifacts {
			manifest.Spec.Artifacts[i].Image = imageOverride
		}
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal satellite manifest error = %v", err)
	}
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write satellite manifest: %v", err)
	}
	home := filepath.Join(dir, "components")
	registry := component.NewRegistry(home)
	var grants []component.ProducerGrant
	for _, family := range manifest.Spec.EmittedFacts {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     manifest.Metadata.ID,
			Version:        manifest.Metadata.Version,
			Kind:           family.Kind,
			SchemaVersions: append([]string(nil), family.SchemaVersions...),
			Scope:          "documentation",
			ExpiresAt:      time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	for _, grant := range grants {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant(%q) error = %v", grant.Kind, err)
		}
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "v0.1.0",
	}
	// The bare loader rejects core-owned kinds without grants, so verify
	// the in-memory manifest against the recorded grant set directly.
	verification := policy.VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	configPath := filepath.Join(dir, "satellite.yaml")
	if err := os.WriteFile(configPath, []byte("process:\n  command: /usr/local/bin/satellite-collector\n"), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "satellite-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return home
}

// pinnedSeedPlatform is the platform the satellite admission tests assert
// against. Admission is scoped to the executing host's GOOS/GOARCH in
// production, and the registry pins only linux images, so tests pass this
// explicit platform instead of reading the host's (#10739).
const pinnedSeedPlatform = "linux/amd64"

// unpinnedSeedPlatform is a host platform the registry pins no satellite
// image for: a Mac developer machine. Admission must refuse it on every host.
const unpinnedSeedPlatform = "darwin/arm64"

// TestLoadConfigPlansPinnedSatelliteActivation proves the coordinator planner
// admits a seeded satellite manifest whose artifacts equal the registry pins
// (#10285).
func TestLoadConfigPlansPinnedSatelliteActivation(t *testing.T) {
	t.Parallel()

	home := installSatelliteActivation(t, scannercomponent.SatelliteDocumentImportID, "")

	cfg, err := loadConfigForPlatform(scannerCoordinatorEnv(home, scannercomponent.SatelliteDocumentImportID), nil, pinnedSeedPlatform)
	if err != nil {
		t.Fatalf("loadConfigForPlatform(%q) error = %v, want nil for the pinned artifacts", pinnedSeedPlatform, err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	instance := cfg.CollectorInstances[0]
	if got, want := instance.InstanceID, "satellite-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
	if got, want := instance.CollectorKind, scope.CollectorKind("documentation"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
}

// TestLoadConfigDeniesDriftedSatelliteActivation proves the coordinator
// planner denies a seeded satellite manifest whose artifact differs from the
// registry pins before any instance is planned, without echoing the image
// (#10285).
func TestLoadConfigDeniesDriftedSatelliteActivation(t *testing.T) {
	t.Parallel()

	id := scannercomponent.SatelliteMediaImportID
	drifted := "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:" + strings.Repeat("f", 64)
	for _, pin := range scannercomponent.SatelliteSeedPins()[id] {
		if drifted == pin {
			t.Fatal("drifted seed image equals a registry pin, want a differing ref")
		}
	}
	home := installSatelliteActivation(t, id, drifted)

	_, err := loadConfigForPlatform(scannerCoordinatorEnv(home, id), nil, pinnedSeedPlatform)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want denial for a drifted satellite artifact")
	}
	if want := "is not the pinned platform image"; !strings.Contains(err.Error(), want) {
		t.Fatalf("loadConfigForPlatform(%q) error = %q, want the digest-mismatch text %q", pinnedSeedPlatform, err, want)
	}
	if notPinned := "declares no seed image"; strings.Contains(err.Error(), notPinned) {
		t.Fatalf("loadConfigForPlatform(%q) error = %q, want a drift refusal, not the platform refusal %q", pinnedSeedPlatform, err, notPinned)
	}
	if strings.Contains(err.Error(), drifted) {
		t.Fatalf("LoadConfig() error = %q, must not echo the image value", err)
	}
}

// TestLoadConfigRefusesUnpinnedSatellitePlatform proves the coordinator
// planner refuses a satellite activation on a host platform the registry pins
// no seed image for, even when the manifest is otherwise the pinned one. The
// fail-closed rule is asserted with an explicit platform so it holds on every
// machine, not only where the host happens to be unpinned (#9877, #10739).
func TestLoadConfigRefusesUnpinnedSatellitePlatform(t *testing.T) {
	t.Parallel()

	id := scannercomponent.SatelliteDocumentImportID
	home := installSatelliteActivation(t, id, "")

	_, err := loadConfigForPlatform(scannerCoordinatorEnv(home, id), nil, unpinnedSeedPlatform)
	if err == nil {
		t.Fatalf("loadConfigForPlatform(%q) error = nil, want refusal without a pinned platform image", unpinnedSeedPlatform)
	}
	if want := "declares no seed image for platform"; !strings.Contains(err.Error(), want) {
		t.Fatalf("loadConfigForPlatform(%q) error = %q, want it to contain %q", unpinnedSeedPlatform, err, want)
	}
}

// TestHostSeedPlatformIsRuntimeGOOSGOARCH pins the production platform source:
// the planner consults only the executing host's GOOS/GOARCH slot, so a
// hardcoded or empty value here would silently change which seed image is
// admitted while every platform-explicit test stayed green (#10739).
func TestHostSeedPlatformIsRuntimeGOOSGOARCH(t *testing.T) {
	t.Parallel()

	if got, want := hostSeedPlatform(), runtime.GOOS+"/"+runtime.GOARCH; got != want {
		t.Fatalf("hostSeedPlatform() = %q, want %q", got, want)
	}
}

// TestLoadConfigAdmitsSatelliteExactlyWhenHostPlatformPinned drives LoadConfig,
// which delegates to the production LoadConfigObserved, against the pinned
// manifest. Admission must follow the executing host: admitted exactly when
// the host platform is in the registry pins for the component, refused with
// the platform text otherwise. It holds on linux and on darwin alike (#9877,
// #10739).
func TestLoadConfigAdmitsSatelliteExactlyWhenHostPlatformPinned(t *testing.T) {
	t.Parallel()

	id := scannercomponent.SatelliteDocumentImportID
	home := installSatelliteActivation(t, id, "")
	host := runtime.GOOS + "/" + runtime.GOARCH
	_, hostPinned := scannercomponent.SatelliteSeedPins()[id][host]

	_, err := LoadConfig(scannerCoordinatorEnv(home, id))
	if hostPinned {
		if err != nil {
			t.Fatalf("LoadConfig() on pinned host %q error = %v, want admission", host, err)
		}
		return
	}
	if err == nil {
		t.Fatalf("LoadConfig() on unpinned host %q error = nil, want refusal", host)
	}
	if want := fmt.Sprintf("declares no seed image for platform %q", host); !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadConfig() on unpinned host %q error = %q, want it to contain %q", host, err, want)
	}
}

// TestLoadConfigAsksTheHostPlatformSource stubs the platform source to a
// platform no seed pin lists and requires LoadConfig, which delegates to the
// production LoadConfigObserved, to refuse it. Unlike the test above, it fails on a linux/amd64 host too
// when the loader ignores the source and hardcodes a pinned platform (#10739).
// It stubs a package variable, so it must not run in parallel.
func TestLoadConfigAsksTheHostPlatformSource(t *testing.T) {
	const unpinned = "plan9/mips"
	previous := seedPlatformSource
	seedPlatformSource = func() string { return unpinned }
	t.Cleanup(func() { seedPlatformSource = previous })

	id := scannercomponent.SatelliteDocumentImportID
	home := installSatelliteActivation(t, id, "")

	_, err := LoadConfig(scannerCoordinatorEnv(home, id))
	if err == nil {
		t.Fatalf("LoadConfig() with host platform %q error = nil, want refusal", unpinned)
	}
	if want := fmt.Sprintf("declares no seed image for platform %q", unpinned); !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadConfig() error = %q, want it to contain %q", err, want)
	}
}
