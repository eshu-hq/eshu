// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"testing"

	observabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/observability/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	tfstatev1 "github.com/eshu-hq/eshu/sdk/go/factschema/terraformstate/v1"
)

func BenchmarkW1eEncodeNoRegression(b *testing.B) {
	b.Run("observability_observed_rule", func(b *testing.B) {
		rule := observabilityv1.ObservedRule{
			SourceInstanceID:     "grafana:prod",
			ProviderObjectUID:    stringPtr("rule:latency"),
			AlertRuleUID:         stringPtr("alert-latency"),
			FolderUID:            stringPtr("folder-api"),
			RuleGroup:            stringPtr("api.rules"),
			Provider:             stringPtr("grafana"),
			SourceKind:           stringPtr("grafana"),
			SourceClass:          stringPtr("observed"),
			ResourceClass:        stringPtr("alert_rule"),
			Outcome:              stringPtr("observed"),
			FreshnessState:       stringPtr("current"),
			DriftCandidateReason: stringPtr("manual_provider_resource"),
			DeclaredMatchState:   stringPtr("not_compared"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeObservabilityObservedRule(rule)
			if err != nil {
				b.Fatalf("EncodeObservabilityObservedRule() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("oci_image_manifest", func(b *testing.B) {
		size := int64(2048)
		configSize := int64(512)
		layerSize := int64(4096)
		manifest := ociregistryv1.ImageManifest{
			RepositoryID:        "oci-registry://registry.example.com/team/api",
			Digest:              "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DescriptorID:        stringPtr("oci-descriptor://registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			MediaType:           stringPtr("application/vnd.oci.image.manifest.v1+json"),
			SizeBytes:           &size,
			ArtifactType:        stringPtr("application/vnd.example.scan"),
			SourceTag:           stringPtr("latest"),
			Config:              &ociregistryv1.Descriptor{Digest: stringPtr("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), MediaType: stringPtr("application/vnd.oci.image.config.v1+json"), SizeBytes: &configSize},
			ConfigLabels:        map[string]string{"org.opencontainers.image.source": "https://github.com/example/api"},
			Layers:              []ociregistryv1.Descriptor{{Digest: stringPtr("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"), MediaType: stringPtr("application/vnd.oci.image.layer.v1.tar+gzip"), SizeBytes: &layerSize}},
			CorrelationAnchors:  []string{"oci-registry://registry.example.com/team/api", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			CollectorInstanceID: stringPtr("registry-prod"),
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeOCIImageManifest(manifest)
			if err != nil {
				b.Fatalf("EncodeOCIImageManifest() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("package_registry_version", func(b *testing.B) {
		version := packageregistryv1.PackageVersion{
			PackageID:           "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core",
			VersionID:           "maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core@3.9.9",
			Version:             "3.9.9",
			Ecosystem:           stringPtr("maven"),
			Registry:            stringPtr("repo.maven.apache.org/maven2"),
			PURL:                stringPtr("pkg:maven/org.apache.maven/maven-core@3.9.9"),
			BOMRef:              stringPtr("pkg:maven/org.apache.maven/maven-core@3.9.9"),
			PackageManager:      stringPtr("maven"),
			PublishedAt:         stringPtr("2026-05-11T09:15:00Z"),
			IsDeprecated:        boolPtr(true),
			IsYanked:            boolPtr(false),
			IsUnlisted:          boolPtr(false),
			IsRetracted:         boolPtr(false),
			ArtifactURLs:        []string{"https://repo.maven.apache.org/maven2/org/apache/maven/maven-core/3.9.9/maven-core-3.9.9.jar"},
			Checksums:           map[string]string{"sha1": "0123456789abcdef"},
			CollectorInstanceID: stringPtr("central"),
			CorrelationAnchors:  []string{"maven://repo.maven.apache.org/maven2/org.apache.maven:maven-core"},
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodePackageRegistryPackageVersion(version)
			if err != nil {
				b.Fatalf("EncodePackageRegistryPackageVersion() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})

	b.Run("terraform_state_resource", func(b *testing.B) {
		resource := tfstatev1.Resource{
			Address:      "module.api.aws_instance.api[0]",
			Mode:         stringPtr("managed"),
			ResourceType: stringPtr("aws_instance"),
			Name:         stringPtr("api"),
			Module:       stringPtr("module.api"),
			Provider:     stringPtr(`provider["registry.terraform.io/hashicorp/aws"]`),
			Attributes: map[string]any{
				"arn":       "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
				"id":        "i-1234567890abcdef0",
				"self_link": "aws_instance.api",
			},
			CorrelationAnchors: []map[string]any{{"anchor_kind": "name", "value_hash": "sha256:name"}},
		}
		b.ReportAllocs()
		for b.Loop() {
			payload, err := EncodeTerraformStateResource(resource)
			if err != nil {
				b.Fatalf("EncodeTerraformStateResource() error = %v", err)
			}
			benchmarkPayloadSink = payload
		}
	})
}
