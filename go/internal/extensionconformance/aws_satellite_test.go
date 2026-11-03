// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/satellite"
	"github.com/eshu-hq/eshu/go/internal/component"
)

const awsSatelliteImageRef = "ghcr.io/eshu-hq/eshu-collector-aws-cloud@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// writeSatelliteManifest writes the typed AWS satellite manifest to a temp
// path so the host loaders (inspect/verify/conform/install) judge the same
// document the satellite publishes.
func writeSatelliteManifest(t *testing.T) string {
	t.Helper()

	manifest, err := satellite.ManifestFor(awsSatelliteImageRef)
	if err != nil {
		t.Fatalf("satellite.ManifestFor() error = %v", err)
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal satellite manifest: %v", err)
	}
	path := filepath.Join(t.TempDir(), "aws-satellite.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write satellite manifest: %v", err)
	}
	return path
}

// installSatelliteActivation records the satellite's required grants,
// verifies, installs, and enables one claims-enabled instance, mirroring
// the chart reconcile state production activation uses.
func installSatelliteActivation(t *testing.T, home string) {
	t.Helper()

	manifestPath := writeSatelliteManifest(t)
	manifest, err := satellite.ManifestFor(awsSatelliteImageRef)
	if err != nil {
		t.Fatalf("satellite.ManifestFor() error = %v", err)
	}
	registry := component.NewRegistry(home)
	for _, grant := range satellite.RequiredGrants(manifest) {
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
	verification := policy.VerifyWithGrants(context.Background(), manifest, satellite.RequiredGrants(manifest))
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	configPath := filepath.Join(home, "aws-satellite.yaml")
	if err := os.WriteFile(configPath, []byte("process:\n  command: /usr/local/bin/collector\n"), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "aws-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
}

// TestSatelliteActivationAdmitsGrantedManifest proves the activation loader
// admits the installed AWS satellite manifest under its live producer
// grants: all fifteen core-owned kinds resolve, so the planner can build
// the aws collector instance.
func TestSatelliteActivationAdmitsGrantedManifest(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installSatelliteActivation(t, home)

	manifest, err := component.NewRegistry(home).LoadInstalledManifestForActivation(satellite.ComponentID, satellite.Version)
	if err != nil {
		t.Fatalf("LoadInstalledManifestForActivation() error = %v, want nil", err)
	}
	if got, want := len(manifest.Spec.EmittedFacts), 15; got != want {
		t.Fatalf("EmittedFacts = %d, want %d", got, want)
	}
	if got, want := manifest.Spec.CollectorKinds, []string{"aws"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("CollectorKinds = %v, want %v", got, want)
	}
}

// TestSatelliteInspectFailsClosedWithoutGrants is the issue #8909
// acceptance for the satellite: with no producer grants recorded, the
// granted-manifest loader (inspect/verify/conform path) rejects the
// satellite manifest with the existing core-owned error.
func TestSatelliteInspectFailsClosedWithoutGrants(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manifestPath := writeSatelliteManifest(t)
	_, _, err := component.NewRegistry(home).LoadGrantedManifestAt(manifestPath, time.Now().UTC())
	if err == nil {
		t.Fatal("LoadGrantedManifestAt() error = nil, want fail-closed without grants")
	}
	if !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("LoadGrantedManifestAt() error = %q, want the core-owned denial", err)
	}
}

// TestSatelliteInstallFailsClosedWithoutGrants proves activation cannot be
// reached without grants either: verification denies the manifest and the
// install refuses the denied verification.
func TestSatelliteInstallFailsClosedWithoutGrants(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manifestPath := writeSatelliteManifest(t)
	manifest, err := satellite.ManifestFor(awsSatelliteImageRef)
	if err != nil {
		t.Fatalf("satellite.ManifestFor() error = %v", err)
	}
	registry := component.NewRegistry(home)
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "v0.1.0",
	}
	verification := policy.VerifyWithGrants(context.Background(), manifest, nil)
	if verification.Allowed {
		t.Fatal("VerifyWithGrants() Allowed = true, want denial without grants")
	}
	if _, err := registry.Install(manifestPath, verification); err == nil {
		t.Fatal("Install() error = nil, want refusal of the denied verification")
	}
}

// satelliteFixtureKinds are the emitted kinds the pinned SDK fixture pack
// ships schemas and example payloads for, plus the schema-less
// aws_tag_observation whose payloads ride unvalidated. The aws_ssm_host
// pair is absent: the pack has no schemas for it yet (see
// TestSatelliteConformReportsMissingSSMPackSchemas).
func satelliteFixtureKinds() []string {
	return []string{
		"aws_resource", "aws_relationship", "aws_dns_record",
		"aws_image_reference", "aws_security_group_rule", "aws_iam_permission",
		"aws_resource_policy_permission", "aws_warning",
		"ec2_instance_posture", "s3_bucket_posture", "rds_instance_posture",
		"s3_external_principal_grant", "aws_tag_observation",
	}
}

// satelliteResult builds one complete SDK result claiming every
// fixture-provable satellite kind with schema-valid payloads.
func satelliteResult(t *testing.T) sdkcollector.Result {
	t.Helper()

	result := validConformanceResult()
	result.Claim.ComponentID = satellite.ComponentID
	result.Claim.InstanceID = "aws-primary"
	result.Claim.CollectorKind = "aws"
	result.Claim.SourceSystem = satellite.ComponentID
	result.Claim.Scope = sdkcollector.Scope{ID: "component:aws-primary", Kind: "component"}
	result.Claim.ConfigHandle = "component-config:aws"
	result.Facts = make([]sdkcollector.Fact, 0, len(satelliteFixtureKinds()))
	for _, kind := range satelliteFixtureKinds() {
		payload, ok := fixturepack.ValidPayload(kind)
		if !ok {
			payload = map[string]any{"observation": "tag-snapshot-" + kind}
		}
		result.Facts = append(result.Facts, sdkcollector.Fact{
			Kind:             kind,
			SchemaVersion:    "1.0.0",
			StableKey:        "aws:satellite:" + kind,
			SourceConfidence: sdkcollector.SourceConfidenceReported,
			ObservedAt:       result.Generation.ObservedAt,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: satellite.ComponentID,
				ScopeID:      "component:aws-primary",
				GenerationID: result.Generation.ID,
				FactKey:      "aws:satellite:" + kind,
				URI:          "component://aws/" + kind + "/1",
				RecordID:     "satellite-fact",
			},
			Payload: payload,
		})
	}
	return result
}

// TestSatelliteConformReportsMissingSSMPackSchemas proves the exact
// remaining fixture-conformance blocker for the satellite manifest:
// manifest-level grant admission succeeds (no core-owned, tuple, or
// phase finding), and the run fails only because the pinned SDK fixture
// pack ships no payload schemas for the aws_ssm_host pair. When the pack
// gains those schemas this test must be rewritten to expect StatusPassed.
func TestSatelliteConformReportsMissingSSMPackSchemas(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manifest, err := satellite.ManifestFor(awsSatelliteImageRef)
	if err != nil {
		t.Fatalf("satellite.ManifestFor() error = %v", err)
	}
	registry := component.NewRegistry(home)
	for _, grant := range satellite.RequiredGrants(manifest) {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant(%q) error = %v", grant.Kind, err)
		}
	}

	report, err := Run(context.Background(), Request{
		ManifestPath:  writeSatelliteManifest(t),
		FixturePaths:  []string{writeConformanceFixture(t, satelliteResult(t))},
		Mode:          ModeFixture,
		ComponentHome: home,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want the missing SSM pack-schema denial")
	}
	assertFinding(t, report, FindingPayloadSchemaInvalid, true, true)
	for _, finding := range report.Findings {
		if finding.Code == FindingManifestInvalid || finding.Code == FindingFixtureContractFailed ||
			finding.Code == FindingMissingReducerConsumer {
			t.Fatalf("finding %q (%s) indicates a grant or phase failure, want only the pack-schema gap", finding.Code, finding.Message)
		}
	}
	matched := false
	for _, finding := range report.Findings {
		if strings.Contains(finding.Message, "aws_ssm_host") && strings.Contains(finding.Message, "no PayloadSchemas entry") {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("no finding names the missing SSM pack schemas in %#v", report.Findings)
	}
}
