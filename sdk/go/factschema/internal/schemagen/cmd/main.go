// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command schemagen regenerates the checked-in JSON Schema artifacts under
// sdk/go/factschema/schema/. Run it via `go generate ./...` from the
// sdk/go/factschema module root (see the //go:generate directive in
// decode.go), or directly with:
//
//	go run ./internal/schemagen/cmd
//
// The command is deterministic: running it twice in a row against an
// unchanged struct produces byte-identical output, which is what
// schema_gen_test.go's drift test relies on.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/factschema/internal/schemagen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	moduleRoot, err := moduleRootDir()
	if err != nil {
		return err
	}

	targets := []struct {
		name     string
		generate func() ([]byte, error)
	}{
		{name: "aws_resource.v1.schema.json", generate: schemagen.AWSResourceSchema},
		{name: "aws_relationship.v1.schema.json", generate: schemagen.AWSRelationshipSchema},
		{name: "aws_security_group_rule.v1.schema.json", generate: schemagen.AWSSecurityGroupRuleSchema},
		{name: "ec2_instance_posture.v1.schema.json", generate: schemagen.EC2InstancePostureSchema},
		{name: "s3_bucket_posture.v1.schema.json", generate: schemagen.S3BucketPostureSchema},
		{name: "aws_iam_permission.v1.schema.json", generate: schemagen.AWSIAMPermissionSchema},
		{name: "aws_resource_policy_permission.v1.schema.json", generate: schemagen.AWSResourcePolicyPermissionSchema},
		{name: "aws_iam_principal.v1.schema.json", generate: schemagen.AWSIAMPrincipalSchema},
		// The incident family fact kinds are DOTTED (Wave 4a, first dotted
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		{name: "incident.record.v1.schema.json", generate: schemagen.IncidentRecordSchema},
		{name: "incident.lifecycle_event.v1.schema.json", generate: schemagen.IncidentLifecycleEventSchema},
		{name: "change.record.v1.schema.json", generate: schemagen.ChangeRecordSchema},
		{name: "incident_routing.applied_pagerduty_resource.v1.schema.json", generate: schemagen.IncidentRoutingAppliedPagerDutyResourceSchema},
		{name: "incident_routing.applied_alert_route.v1.schema.json", generate: schemagen.IncidentRoutingAppliedAlertRouteSchema},
		{name: "incident_routing.observed_pagerduty_service.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyServiceSchema},
		{name: "incident_routing.observed_pagerduty_integration.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyIntegrationSchema},
		{name: "incident_routing.coverage_warning.v1.schema.json", generate: schemagen.IncidentRoutingCoverageWarningSchema},
		{name: "gcp_cloud_resource.v1.schema.json", generate: schemagen.GCPCloudResourceSchema},
		{name: "gcp_cloud_relationship.v1.schema.json", generate: schemagen.GCPCloudRelationshipSchema},
		{name: "gcp_collection_warning.v1.schema.json", generate: schemagen.GCPCollectionWarningSchema},
		{name: "gcp_dns_record.v1.schema.json", generate: schemagen.GCPDNSRecordSchema},
		{name: "gcp_iam_policy_observation.v1.schema.json", generate: schemagen.GCPIAMPolicyObservationSchema},
		{name: "azure_cloud_resource.v1.schema.json", generate: schemagen.AzureCloudResourceSchema},
		{name: "azure_cloud_relationship.v1.schema.json", generate: schemagen.AzureCloudRelationshipSchema},
		{name: "azure_dns_record.v1.schema.json", generate: schemagen.AzureDNSRecordSchema},
		{name: "azure_collection_warning.v1.schema.json", generate: schemagen.AzureCollectionWarningSchema},
		// The kubernetes_live family fact kinds are DOTTED, matching the
		// incident family's convention above.
		{name: "kubernetes_live.pod_template.v1.schema.json", generate: schemagen.KubernetesLivePodTemplateSchema},
		{name: "kubernetes_live.relationship.v1.schema.json", generate: schemagen.KubernetesLiveRelationshipSchema},
		{name: "kubernetes_live.warning.v1.schema.json", generate: schemagen.KubernetesLiveWarningSchema},
		// The oci_registry family fact kinds are DOTTED (like the incident
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		{name: "oci_registry.repository.v1.schema.json", generate: schemagen.OCIRegistryRepositorySchema},
		{name: "oci_registry.image_manifest.v1.schema.json", generate: schemagen.OCIImageManifestSchema},
		{name: "oci_registry.image_index.v1.schema.json", generate: schemagen.OCIImageIndexSchema},
		{name: "oci_registry.image_descriptor.v1.schema.json", generate: schemagen.OCIImageDescriptorSchema},
		{name: "oci_registry.image_tag_observation.v1.schema.json", generate: schemagen.OCIImageTagObservationSchema},
		{name: "oci_registry.image_referrer.v1.schema.json", generate: schemagen.OCIImageReferrerSchema},
		{name: "oci_registry.warning.v1.schema.json", generate: schemagen.OCIRegistryWarningSchema},
		// The terraform_state family fact kinds are UNDERSCORE-separated. Five
		// are consumed by the projector's source-local canonical extractor; three
		// (candidate, provider_binding, warning) are typed-but-not-yet-consumed
		// (terraformstate/v1/doc.go) but still ship a checked-in schema.
		{name: "terraform_state_snapshot.v1.schema.json", generate: schemagen.TerraformStateSnapshotSchema},
		{name: "terraform_state_resource.v1.schema.json", generate: schemagen.TerraformStateResourceSchema},
		{name: "terraform_state_module.v1.schema.json", generate: schemagen.TerraformStateModuleSchema},
		{name: "terraform_state_output.v1.schema.json", generate: schemagen.TerraformStateOutputSchema},
		{name: "terraform_state_tag_observation.v1.schema.json", generate: schemagen.TerraformStateTagObservationSchema},
		{name: "terraform_state_candidate.v1.schema.json", generate: schemagen.TerraformStateCandidateSchema},
		{name: "terraform_state_provider_binding.v1.schema.json", generate: schemagen.TerraformStateProviderBindingSchema},
		{name: "terraform_state_warning.v1.schema.json", generate: schemagen.TerraformStateWarningSchema},
		// The package_registry family fact kinds are DOTTED (like the incident
		// and oci_registry families). Three are consumed by the projector's
		// source-local canonical extractor; six (source_hint, package_artifact,
		// vulnerability_hint, registry_event, repository_hosting, warning) are
		// typed-but-not-yet-consumed through the decode seam
		// (packageregistry/v1/doc.go) but still ship a checked-in schema.
		{name: "package_registry.package.v1.schema.json", generate: schemagen.PackageRegistryPackageSchema},
		{name: "package_registry.package_version.v1.schema.json", generate: schemagen.PackageRegistryPackageVersionSchema},
		{name: "package_registry.package_dependency.v1.schema.json", generate: schemagen.PackageRegistryPackageDependencySchema},
		{name: "package_registry.source_hint.v1.schema.json", generate: schemagen.PackageRegistrySourceHintSchema},
		{name: "package_registry.package_artifact.v1.schema.json", generate: schemagen.PackageRegistryPackageArtifactSchema},
		{name: "package_registry.vulnerability_hint.v1.schema.json", generate: schemagen.PackageRegistryVulnerabilityHintSchema},
		{name: "package_registry.registry_event.v1.schema.json", generate: schemagen.PackageRegistryRegistryEventSchema},
		{name: "package_registry.repository_hosting.v1.schema.json", generate: schemagen.PackageRegistryRepositoryHostingSchema},
		{name: "package_registry.warning.v1.schema.json", generate: schemagen.PackageRegistryWarningSchema},
		// The sbom_attestation family fact kinds are DOTTED (like the incident
		// family). document/component/warning and statement/signature_verification
		// are consumed by the reducer's sbom_attestation_attachment domain;
		// dependency_relationship/external_reference/slsa_provenance are
		// typed-but-not-yet-consumed (sbom/v1/doc.go) but still ship a checked-in
		// schema.
		{name: "sbom.document.v1.schema.json", generate: schemagen.SBOMDocumentSchema},
		{name: "sbom.component.v1.schema.json", generate: schemagen.SBOMComponentSchema},
		{name: "sbom.dependency_relationship.v1.schema.json", generate: schemagen.SBOMDependencyRelationshipSchema},
		{name: "sbom.external_reference.v1.schema.json", generate: schemagen.SBOMExternalReferenceSchema},
		{name: "sbom.warning.v1.schema.json", generate: schemagen.SBOMWarningSchema},
		{name: "attestation.statement.v1.schema.json", generate: schemagen.AttestationStatementSchema},
		{name: "attestation.signature_verification.v1.schema.json", generate: schemagen.AttestationSignatureVerificationSchema},
		{name: "attestation.slsa_provenance.v1.schema.json", generate: schemagen.AttestationSLSAProvenanceSchema},
	}

	for _, target := range targets {
		raw, err := target.generate()
		if err != nil {
			return fmt.Errorf("schemagen: generate %s: %w", target.name, err)
		}

		dest := filepath.Join(moduleRoot, "schema", target.name)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			return fmt.Errorf("schemagen: write %s: %w", dest, err)
		}
	}

	return nil
}

// modulePath is the module path declared in this module's go.mod. It anchors
// the upward search in moduleRootDir so the command finds this module's root
// rather than a parent module's go.mod higher up the tree.
const modulePath = "module github.com/eshu-hq/eshu/sdk/go/factschema"

// byteOrderMark is a UTF-8 BOM that a go.mod file may carry as its first
// bytes; declaresModule trims it before comparing the module line.
const byteOrderMark = "\ufeff"

// moduleRootDir returns the directory holding this module's go.mod by walking
// up from the working directory. It matches on the module path declared in
// go.mod, not merely the presence of a go.mod, so it never mistakes a parent
// module's root for this one. It fails fast with a clear error rather than
// writing the schema files to a wrong path.
func moduleRootDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("schemagen: getwd: %w", err)
	}

	for {
		goMod := filepath.Join(dir, "go.mod")
		if raw, readErr := os.ReadFile(goMod); readErr == nil {
			if declaresModule(raw, modulePath) {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"schemagen: could not locate the %q module root above the working directory; run via `go generate ./...` from the module root",
				modulePath,
			)
		}
		dir = parent
	}
}

// declaresModule reports whether a go.mod file's contents declare
// wantModuleLine as the module path, tolerating a leading UTF-8 byte-order
// mark and surrounding whitespace on the module line.
func declaresModule(goMod []byte, wantModuleLine string) bool {
	for _, line := range strings.Split(string(goMod), "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, byteOrderMark)) == wantModuleLine {
			return true
		}
	}
	return false
}
