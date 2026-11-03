// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package satellite

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/component"
)

const testImageRef = "ghcr.io/eshu-hq/eshu-collector-aws-cloud@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// TestManifestForDeclaresSatelliteIdentity pins the AWS satellite component
// identity: the producer ID the fact registry authorizes, the publisher that
// trust policy allowlists, and the OCI/v1alpha1 runtime contract the
// extracted satellite speaks (its collector emits ProtocolVersionV1Alpha1
// and ships a linux/amd64 image for EKS nodes).
func TestManifestForDeclaresSatelliteIdentity(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(testImageRef)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	if manifest.Metadata.ID != ComponentID {
		t.Fatalf("Metadata.ID = %q, want %q", manifest.Metadata.ID, ComponentID)
	}
	if ComponentID != "collector-aws-cloud" {
		t.Fatalf("ComponentID = %q, want the registry-authorized producer", ComponentID)
	}
	if manifest.Metadata.Publisher != "eshu-hq" {
		t.Fatalf("Metadata.Publisher = %q, want eshu-hq", manifest.Metadata.Publisher)
	}
	if manifest.Metadata.Version != Version {
		t.Fatalf("Metadata.Version = %q, want %q", manifest.Metadata.Version, Version)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != awscloud.CollectorKind {
		t.Fatalf("CollectorKinds = %v, want [%q]", manifest.Spec.CollectorKinds, awscloud.CollectorKind)
	}
	if manifest.Spec.Runtime.SDKProtocol != component.CollectorSDKProtocolV1Alpha1 {
		t.Fatalf("SDKProtocol = %q, want collector-sdk/v1alpha1", manifest.Spec.Runtime.SDKProtocol)
	}
	if manifest.Spec.Runtime.Adapter != component.RuntimeAdapterOCI {
		t.Fatalf("Adapter = %q, want oci", manifest.Spec.Runtime.Adapter)
	}
	if len(manifest.Spec.Artifacts) != 1 {
		t.Fatalf("Artifacts = %d, want exactly one digest-pinned image", len(manifest.Spec.Artifacts))
	}
	artifact := manifest.Spec.Artifacts[0]
	if artifact.Platform != "linux/amd64" {
		t.Fatalf("Platform = %q, want linux/amd64 for EKS nodes", artifact.Platform)
	}
	if artifact.Image != testImageRef {
		t.Fatalf("Image = %q, want the supplied publish ref", artifact.Image)
	}
}

// TestManifestForCoversEveryEmittedKind proves the manifest declares every
// fact kind the satellite can emit, in registry emission order, each at
// schema 1.0.0 with reported confidence. The host rejects undeclared kinds
// at admission, so this list must cover the whole satellite surface, not
// only the kinds the SDK fixture pack can prove.
func TestManifestForCoversEveryEmittedKind(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(testImageRef)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	want := []string{
		"aws_resource", "aws_relationship", "aws_tag_observation",
		"aws_dns_record", "aws_image_reference", "aws_security_group_rule",
		"aws_iam_permission", "aws_resource_policy_permission", "aws_warning",
		"ec2_instance_posture", "s3_bucket_posture", "rds_instance_posture",
		"s3_external_principal_grant",
		"aws_ssm_host_application", "aws_ssm_host_platform",
	}
	if len(manifest.Spec.EmittedFacts) != len(want) {
		t.Fatalf("EmittedFacts = %d, want %d", len(manifest.Spec.EmittedFacts), len(want))
	}
	for i, kind := range want {
		family := manifest.Spec.EmittedFacts[i]
		if family.Kind != kind {
			t.Fatalf("EmittedFacts[%d] = %q, want %q", i, family.Kind, kind)
		}
		if len(family.SchemaVersions) != 1 || family.SchemaVersions[0] != "1.0.0" {
			t.Fatalf("EmittedFacts[%d] versions = %v, want [1.0.0]", i, family.SchemaVersions)
		}
		if len(family.SourceConfidence) != 1 || family.SourceConfidence[0] != "reported" {
			t.Fatalf("EmittedFacts[%d] confidence = %v, want [reported]", i, family.SourceConfidence)
		}
		if _, ok := fixturepack.SchemaFor(kind); ok {
			if family.PayloadSchemaRef != kind {
				t.Fatalf("EmittedFacts[%d] payloadSchemaRef = %q, want %q", i, family.PayloadSchemaRef, kind)
			}
		} else if family.PayloadSchemaRef != "" {
			t.Fatalf("EmittedFacts[%d] payloadSchemaRef = %q, want empty (no shipped schema)", i, family.PayloadSchemaRef)
		}
	}
}

// TestManifestForDeclaresTrustedReducerPhases proves the manifest phases
// cover exactly the reducer domains the fact registry assigns the emitted
// kinds. The conformance harness rejects a trusted phase the manifest
// misses and a declared phase no grant authorizes.
func TestManifestForDeclaresTrustedReducerPhases(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(testImageRef)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	want := []string{
		"aws_cloud_runtime_drift",
		"ec2_instance_node_materialization",
		"rds_posture_materialization",
		"s3_external_principal_grant_materialization",
		"s3_internet_exposure_materialization",
		"ssm_host_inventory_materialization",
	}
	if len(manifest.Spec.ConsumerContracts.Reducer.Phases) != len(want) {
		t.Fatalf("Phases = %v, want %v", manifest.Spec.ConsumerContracts.Reducer.Phases, want)
	}
	for i, phase := range want {
		if manifest.Spec.ConsumerContracts.Reducer.Phases[i] != phase {
			t.Fatalf("Phases[%d] = %q, want %q", i, manifest.Spec.ConsumerContracts.Reducer.Phases[i], phase)
		}
	}
}

// TestRequiredGrantsCoverEmittedKinds proves every declared fact family has
// a matching live producer grant scoped to the satellite collector kind.
func TestRequiredGrantsCoverEmittedKinds(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(testImageRef)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	grants := RequiredGrants(manifest)
	if len(grants) != len(manifest.Spec.EmittedFacts) {
		t.Fatalf("grants = %d, want %d", len(grants), len(manifest.Spec.EmittedFacts))
	}
	for i, grant := range grants {
		family := manifest.Spec.EmittedFacts[i]
		if grant.ProducerID != manifest.Metadata.ID {
			t.Fatalf("grant[%d].ProducerID = %q, want %q", i, grant.ProducerID, manifest.Metadata.ID)
		}
		if grant.Version != manifest.Metadata.Version {
			t.Fatalf("grant[%d].Version = %q, want %q", i, grant.Version, manifest.Metadata.Version)
		}
		if grant.Kind != family.Kind {
			t.Fatalf("grant[%d].Kind = %q, want %q", i, grant.Kind, family.Kind)
		}
		if len(grant.SchemaVersions) != len(family.SchemaVersions) {
			t.Fatalf("grant[%d] versions = %v, want %v", i, grant.SchemaVersions, family.SchemaVersions)
		}
		if grant.Scope != awscloud.CollectorKind {
			t.Fatalf("grant[%d].Scope = %q, want %q", i, grant.Scope, awscloud.CollectorKind)
		}
		if grant.Revoked {
			t.Fatalf("grant[%d] revoked, want live", i)
		}
		if !grant.ExpiresAt.After(time.Now().Add(24 * time.Hour)) {
			t.Fatalf("grant[%d] expires at %s, want live with renewal margin", i, grant.ExpiresAt.Format(time.RFC3339))
		}
	}
}

// TestManifestForRejectsUnpinnedImage proves the constructor fails closed
// when the publish-owned image ref is not digest-pinned: an unpinned
// artifact must never become admission truth.
func TestManifestForRejectsUnpinnedImage(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"", "ghcr.io/eshu-hq/eshu-collector-aws-cloud:latest", "not-an-image"} {
		if _, err := ManifestFor(ref); err == nil {
			t.Fatalf("ManifestFor(%q) error = nil, want fail-closed", ref)
		}
	}
}

// TestManifestFailsClosedWithoutGrants proves the generated manifest keeps
// the fail-closed contract: bare shape validation rejects the core-owned
// kinds, so only the producer-grant path (proven in extensionconformance)
// can admit it.
func TestManifestFailsClosedWithoutGrants(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(testImageRef)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	err = manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want fail-closed for core-owned kinds")
	}
	if !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("Validate() error = %q, want the core-owned denial", err)
	}
}
