// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"testing"

	cicdrunv1 "github.com/eshu-hq/eshu-sdk/factschema/cicdrun/v1"
	sbomv1 "github.com/eshu-hq/eshu-sdk/factschema/sbom/v1"
	vulnerabilityv1 "github.com/eshu-hq/eshu-sdk/factschema/vulnerability/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyDecodeCases covers the supply-chain seams (SBOM/attestation,
// CI-run, vulnerability intelligence).
func supplyDecodeCases() []decodeCase {
	return []decodeCase{
		{
			name: "SBOMDocument", kind: "sbom.document", target: &sbomv1.Document{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSBOMDocument(env) },
			requiredField: "document_id",
			spotFields:    map[string]any{"document_id": "sbom-doc-1"},
		},
		{
			name: "SBOMComponent", kind: "sbom.component", target: &sbomv1.Component{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSBOMComponent(env) },
			requiredField: "document_id",
			spotFields:    map[string]any{"document_id": "sbom-doc-1"},
		},
		{
			name: "SBOMDependencyRelationship", kind: "sbom.dependency_relationship", target: &sbomv1.DependencyRelationship{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSBOMDependencyRelationship(env) },
			requiredField: "document_id",
			spotFields:    map[string]any{"document_id": "sbom-doc-1"},
		},
		{
			name: "SBOMExternalReference", kind: "sbom.external_reference", target: &sbomv1.ExternalReference{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSBOMExternalReference(env) },
			requiredField: "document_id",
			spotFields:    map[string]any{"document_id": "sbom-doc-1"},
		},
		{
			name: "SBOMWarning", kind: "sbom.warning", target: &sbomv1.Warning{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeSBOMWarning(env) },
			requiredField: "reason", typeMismatch: true,
			spotFields: map[string]any{"reason": "malformed_document"},
		},
		{
			name: "AttestationStatement", kind: "attestation.statement", target: &sbomv1.Statement{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAttestationStatement(env) },
			requiredField: "statement_id",
			spotFields:    map[string]any{"statement_id": "stmt-1"},
		},
		{
			name: "AttestationSignatureVerification", kind: "attestation.signature_verification", target: &sbomv1.SignatureVerification{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAttestationSignatureVerification(env) },
			requiredField: "statement_id",
			spotFields:    map[string]any{"statement_id": "stmt-1"},
		},
		{
			name: "AttestationSLSAProvenance", kind: "attestation.slsa_provenance", target: &sbomv1.SLSAProvenance{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAttestationSLSAProvenance(env) },
			requiredField: "statement_id",
			spotFields:    map[string]any{"statement_id": "stmt-1"},
		},
		{
			name: "CICDRun", kind: "ci.run", target: &cicdrunv1.Run{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDRun(env) },
			requiredField: "run_id",
			spotFields:    map[string]any{"provider": "github-actions", "run_id": "run-123"},
		},
		{
			name: "CICDArtifact", kind: "ci.artifact", target: &cicdrunv1.Artifact{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDArtifact(env) },
			requiredField: "run_id",
			spotFields:    map[string]any{"provider": "github-actions", "run_id": "run-123"},
		},
		{
			name: "CICDEnvironmentObservation", kind: "ci.environment_observation", target: &cicdrunv1.EnvironmentObservation{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDEnvironmentObservation(env) },
			requiredField: "provider",
			spotFields:    map[string]any{"provider": "github-actions", "run_id": "run-123"},
		},
		{
			name: "CICDDeploymentEvent", kind: "ci.deployment_event", target: &cicdrunv1.DeploymentEvent{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDDeploymentEvent(env) },
			requiredField: "deployment_id",
			spotFields:    map[string]any{"provider": "argocd", "deployment_id": "deploy-7", "environment": "production"},
		},
		{
			name: "CICDTriggerEdge", kind: "ci.trigger_edge", target: &cicdrunv1.TriggerEdge{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDTriggerEdge(env) },
			requiredField: "run_id",
			spotFields:    map[string]any{"provider": "github-actions", "run_id": "run-123"},
		},
		{
			name: "CICDStep", kind: "ci.step", target: &cicdrunv1.Step{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDStep(env) },
			requiredField: "run_id",
			spotFields:    map[string]any{"provider": "github-actions", "run_id": "run-456"},
		},
		{
			name: "CICDWorkflowImageEvidence", kind: "ci.workflow_image_evidence", target: &cicdrunv1.WorkflowImageEvidence{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeCICDWorkflowImageEvidence(env) },
			requiredField: "repository_id",
			spotFields:    map[string]any{"repository_id": "github.com/example/repo"},
		},
		{
			name: "VulnerabilityCVE", kind: "vulnerability.cve", target: &vulnerabilityv1.CVE{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityCVE(env) },
			requiredField: "advisory_id",
			spotFields:    map[string]any{"advisory_id": "GHSA-xxxx-yyyy-zzzz"},
		},
		{
			name: "VulnerabilityAffectedPackage", kind: "vulnerability.affected_package", target: &vulnerabilityv1.AffectedPackage{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityAffectedPackage(env) },
			requiredField: "advisory_id",
			spotFields:    map[string]any{"advisory_id": "GHSA-xxxx-yyyy-zzzz"},
		},
		{
			name: "VulnerabilityAffectedProduct", kind: "vulnerability.affected_product", target: &vulnerabilityv1.AffectedProduct{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityAffectedProduct(env) },
			requiredField: "cve_id", typeMismatch: true,
			spotFields: map[string]any{"cve_id": "CVE-2026-0001"},
		},
		{
			name: "VulnerabilityOSPackage", kind: "vulnerability.os_package", target: &vulnerabilityv1.OSPackage{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityOSPackage(env) },
			requiredField: "name",
			spotFields:    map[string]any{"distro": "debian", "name": "openssl", "installed_version_raw": "3.0.11-1"},
		},
		{
			name: "VulnerabilityEPSSScore", kind: "vulnerability.epss_score", target: &vulnerabilityv1.EPSSScore{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityEPSSScore(env) },
			requiredField: "cve_id",
			spotFields:    map[string]any{"cve_id": "CVE-2026-0001"},
		},
		{
			name: "VulnerabilityKnownExploited", kind: "vulnerability.known_exploited", target: &vulnerabilityv1.KnownExploited{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityKnownExploited(env) },
			requiredField: "cve_id",
			spotFields:    map[string]any{"cve_id": "CVE-2026-0001"},
		},
		{
			name: "VulnerabilityGoModuleEvidence", kind: "vulnerability.go_module_evidence", target: &vulnerabilityv1.GoModuleEvidence{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityGoModuleEvidence(env) },
			requiredField: "module_path",
			spotFields:    map[string]any{"repository_id": "github.com/example/repo", "module_path": "github.com/example/mod"},
		},
		{
			name: "VulnerabilityGoCallReachability", kind: "vulnerability.go_call_reachability", target: &vulnerabilityv1.GoCallReachability{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeVulnerabilityGoCallReachability(env) },
			requiredField: "osv_id",
			spotFields:    map[string]any{"repository_id": "github.com/example/repo", "osv_id": "GO-2026-0001"},
		},
	}
}

func TestSupplyDecodeSeams(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, supplyDecodeCases())
}
