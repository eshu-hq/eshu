// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa //nolint:filelength // Per-family cassette registry at natural capacity (504 lines): each live family appends one FullPath+Load pair the lockstep test and callers reference by name, and no seam splits those pairs without scattering the registry -- a table-driven restructure is the real fix when the owner schedules it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
)

// Committed-cassette loaders for the direct-materialization family Odùs
// (#6228, under the #6181 umbrella).
//
// A cassette is a recorded collector output the live gates replay instead of
// calling a real cluster or cloud account. Each family keeps two copies of the
// same fixture: the Go-compiled Odù that catalog_seed.go registers, and the
// committed cassette the gates drive. They must not drift, so the lockstep
// tests project the cassette back through loadDirectFamilyOdu and require it to
// equal the compiled Odù exactly.
//
// All these families share one loader rather than carrying a copy each. The
// shared-projection families each grew their own decoder because each reads a
// different fact shape; these read the SAME envelope fields and differ
// only in which Odù name the projection carries, so a copy per family would be
// one place per family for one contract to rot.

const (
	// kubernetesNamespaceEnvironmentFamilyCassettePath,
	// iamInstanceProfileRoleFamilyCassettePath, iamCanAssumeFamilyCassettePath,
	// iamCanPerformFamilyCassettePath,
	// workloadCloudRelationshipFamilyCassettePath,
	// iamEscalationFamilyCassettePath, ec2UsesProfileFamilyCassettePath,
	// s3LogsToFamilyCassettePath, kubernetesCorrelationFamilyCassettePath,
	// authenticatesVaultRoleFamilyCassettePath,
	// usesVaultPolicyFamilyCassettePath,
	// semanticEntityContainmentFamilyCassettePath,
	// cloudResourceFamilyCassettePath, codeTaintEvidenceFamilyCassettePath,
	// observabilityCoverageFamilyCassettePath,
	// codeInterprocEvidenceFamilyCassettePath,
	// builtFromFamilyCassettePath,
	// cloudResourceContainerImageFamilyCassettePath,
	// crossplaneSatisfiedByFamilyCassettePath,
	// derivedFromFamilyCassettePath, and
	// incidentRoutingEvidenceFamilyCassettePath are repo-root-relative so the
	// constants stay machine-independent; callers join a repo root onto
	// them through the exported helpers below.
	kubernetesNamespaceEnvironmentFamilyCassettePath = "testdata/cassettes/kubernetesnamespaceenvironment/ifa-kubernetes-namespace-environment-family.json"
	iamInstanceProfileRoleFamilyCassettePath         = "testdata/cassettes/iaminstanceprofilerole/ifa-iam-instance-profile-role-family.json"
	iamCanAssumeFamilyCassettePath                   = "testdata/cassettes/iamcanassume/ifa-iam-can-assume-family.json"
	iamCanPerformFamilyCassettePath                  = "testdata/cassettes/iamcanperform/ifa-iam-can-perform-family.json"
	workloadCloudRelationshipFamilyCassettePath      = "testdata/cassettes/workloadcloudrelationship/ifa-workload-cloud-relationship-family.json"
	iamEscalationFamilyCassettePath                  = "testdata/cassettes/iamescalation/ifa-iam-escalation-family.json"
	ec2UsesProfileFamilyCassettePath                 = "testdata/cassettes/ec2usesprofile/ifa-ec2-uses-profile-family.json"
	s3LogsToFamilyCassettePath                       = "testdata/cassettes/s3logsto/ifa-s3-logs-to-family.json"
	authenticatesVaultRoleFamilyCassettePath         = "testdata/cassettes/authenticatesvaultrole/ifa-authenticates-vault-role-family.json"
	usesVaultPolicyFamilyCassettePath                = "testdata/cassettes/usesvaultpolicy/ifa-uses-vault-policy-family.json"
	builtFromFamilyCassettePath                      = "testdata/cassettes/builtfrom/ifa-built-from-family.json"
	cloudResourceContainerImageFamilyCassettePath    = "testdata/cassettes/cloudresourcecontainerimage/ifa-cloud-resource-container-image-family.json"
	kubernetesCorrelationFamilyCassettePath          = "testdata/cassettes/kubernetescorrelation/ifa-kubernetes-correlation-family.json"
	semanticEntityContainmentFamilyCassettePath      = "testdata/cassettes/semanticentitycontainment/ifa-semantic-entity-containment-family.json"
	cloudResourceFamilyCassettePath                  = "testdata/cassettes/cloudresource/ifa-cloud-resource-family.json"
	codeTaintEvidenceFamilyCassettePath              = "testdata/cassettes/codetaint/ifa-code-taint-evidence-family.json"
	observabilityCoverageFamilyCassettePath          = "testdata/cassettes/obscoverage/ifa-observability-coverage-family.json"
	codeInterprocEvidenceFamilyCassettePath          = "testdata/cassettes/codeinterproc/ifa-code-interproc-evidence-family.json"
	crossplaneSatisfiedByFamilyCassettePath          = "testdata/cassettes/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family.json"
	derivedFromFamilyCassettePath                    = "testdata/cassettes/derivedfrom/ifa-derived-from-family.json"
	incidentRoutingEvidenceFamilyCassettePath        = "testdata/cassettes/incidentrouting/ifa-incident-routing-evidence-family.json"
)

// KubernetesNamespaceEnvironmentFamilyCassetteFullPath joins repoRoot onto the
// committed kubernetes_namespace_environment family cassette path.
func KubernetesNamespaceEnvironmentFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, kubernetesNamespaceEnvironmentFamilyCassettePath)
}

// IAMInstanceProfileRoleFamilyCassetteFullPath joins repoRoot onto the
// committed iam_instance_profile_role family cassette path.
func IAMInstanceProfileRoleFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamInstanceProfileRoleFamilyCassettePath)
}

// directFamilyCassetteFile mirrors the committed cassette's v1 wire shape.
//
// Every field the cassette carries is declared, including the ones the
// projection deliberately drops (fencing_token, observed_at, the scope
// descriptors). The decoder rejects unknown fields, so an undeclared field
// would fail the load rather than being ignored -- and a field declared here
// but not projected is a deliberate exclusion, documented at the projection.
type directFamilyCassetteFile struct {
	Collector     string                      `json:"collector"`
	SchemaVersion string                      `json:"schema_version"`
	Scopes        []directFamilyCassetteScope `json:"scopes"`
}

// directFamilyCassetteScope is one scope entry of directFamilyCassetteFile,
// named so the single-scope and cloud_resource multi-scope projections share
// one per-scope envelope projection instead of two copies.
type directFamilyCassetteScope struct {
	ScopeID       string            `json:"scope_id"`
	SourceSystem  string            `json:"source_system"`
	ScopeKind     string            `json:"scope_kind"`
	CollectorKind string            `json:"collector_kind"`
	PartitionKey  string            `json:"partition_key"`
	Metadata      map[string]string `json:"metadata"`
	GenerationID  string            `json:"generation_id"`
	ObservedAt    string            `json:"observed_at"`
	TriggerKind   string            `json:"trigger_kind"`
	Facts         []struct {
		FactKind         string `json:"fact_kind"`
		StableFactKey    string `json:"stable_fact_key"`
		SchemaVersion    string `json:"schema_version"`
		CollectorKind    string `json:"collector_kind"`
		FencingToken     int64  `json:"fencing_token"`
		SourceConfidence string `json:"source_confidence"`
		IsTombstone      bool   `json:"is_tombstone,omitempty"`
		// SourceURI is projected (unlike the transport fields below)
		// because the semantic extractor reads the entity's file path
		// from it: dropping it would make the semantic family's
		// cassette extract zero rows while its compiled Odù extracts
		// the fixture set.
		SourceURI string         `json:"source_uri,omitempty"`
		Payload   map[string]any `json:"payload"`
	} `json:"facts"`
}

// LoadKubernetesNamespaceEnvironmentFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes.
//
// It is the lockstep loader for that cassette: production registers the
// compiled familyodu.KubernetesNamespaceEnvironmentFamilyOdu in catalogSeed, and
// TestKubernetesNamespaceEnvironmentFamilyCassetteMatchesCompiledOdu compares
// the two. Exported so materializededges' tests can reach it without a second
// copy of the decoder.
func LoadKubernetesNamespaceEnvironmentFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, KubernetesNamespaceEnvironmentFamilyOduName)
}

// LoadIAMInstanceProfileRoleFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes the reducer's extractor consumes, the
// iam_instance_profile_role counterpart to
// LoadKubernetesNamespaceEnvironmentFamilyOdu.
func LoadIAMInstanceProfileRoleFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMInstanceProfileRoleFamilyOduName)
}

// IAMCanAssumeFamilyCassetteFullPath joins repoRoot onto the committed
// iam_can_assume family cassette path.
func IAMCanAssumeFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanAssumeFamilyCassettePath)
}

// LoadIAMCanAssumeFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the iam_can_assume
// counterpart to LoadIAMInstanceProfileRoleFamilyOdu.
func LoadIAMCanAssumeFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMCanAssumeFamilyOduName)
}

// IAMCanPerformFamilyCassetteFullPath joins repoRoot onto the committed
// iam_can_perform family cassette path.
func IAMCanPerformFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamCanPerformFamilyCassettePath)
}

// LoadIAMCanPerformFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the iam_can_perform
// counterpart to LoadIAMCanAssumeFamilyOdu.
func LoadIAMCanPerformFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMCanPerformFamilyOduName)
}

// WorkloadCloudRelationshipFamilyCassetteFullPath joins repoRoot onto the
// committed workload_cloud_relationship family cassette path.
func WorkloadCloudRelationshipFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, workloadCloudRelationshipFamilyCassettePath)
}

// LoadWorkloadCloudRelationshipFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// workload_cloud_relationship counterpart to LoadIAMCanPerformFamilyOdu.
func LoadWorkloadCloudRelationshipFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, WorkloadCloudRelationshipFamilyOduName)
}

// IAMEscalationFamilyCassetteFullPath joins repoRoot onto the committed
// iam_escalation family cassette path.
func IAMEscalationFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, iamEscalationFamilyCassettePath)
}

// LoadIAMEscalationFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extractor consumes, the
// iam_escalation counterpart to LoadWorkloadCloudRelationshipFamilyOdu.
func LoadIAMEscalationFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IAMEscalationFamilyOduName)
}

// EC2UsesProfileFamilyCassetteFullPath joins repoRoot onto the committed
// ec2_uses_profile family cassette path.
func EC2UsesProfileFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, ec2UsesProfileFamilyCassettePath)
}

// LoadEC2UsesProfileFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extractor consumes, the
// ec2_uses_profile counterpart to LoadIAMEscalationFamilyOdu.
func LoadEC2UsesProfileFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, EC2UsesProfileFamilyOduName)
}

// S3LogsToFamilyCassetteFullPath joins repoRoot onto the committed
// s3_logs_to family cassette path.
func S3LogsToFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, s3LogsToFamilyCassettePath)
}

// LoadS3LogsToFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the s3_logs_to
// counterpart to LoadEC2UsesProfileFamilyOdu.
func LoadS3LogsToFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, S3LogsToFamilyOduName)
}

// AuthenticatesVaultRoleFamilyCassetteFullPath joins repoRoot onto the
// committed authenticates_vault_role family cassette path.
func AuthenticatesVaultRoleFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, authenticatesVaultRoleFamilyCassettePath)
}

// LoadAuthenticatesVaultRoleFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// authenticates_vault_role counterpart to LoadS3LogsToFamilyOdu.
func LoadAuthenticatesVaultRoleFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, AuthenticatesVaultRoleFamilyOduName)
}

// UsesVaultPolicyFamilyCassetteFullPath joins repoRoot onto the committed
// uses_vault_policy family cassette path.
func UsesVaultPolicyFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, usesVaultPolicyFamilyCassettePath)
}

// LoadUsesVaultPolicyFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the reducer's extractor consumes, the
// uses_vault_policy counterpart to LoadAuthenticatesVaultRoleFamilyOdu.
func LoadUsesVaultPolicyFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, UsesVaultPolicyFamilyOduName)
}

// BuiltFromFamilyCassetteFullPath joins repoRoot onto the committed
// built_from family cassette path.
func BuiltFromFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, builtFromFamilyCassettePath)
}

// LoadBuiltFromFamilyOdu reads the committed cassette and projects it onto
// the fact envelopes the reducer's extractor consumes, the built_from
// counterpart to LoadUsesVaultPolicyFamilyOdu.
func LoadBuiltFromFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, BuiltFromFamilyOduName)
}

// CloudResourceContainerImageFamilyCassetteFullPath joins repoRoot onto the
// committed cloud_resource_container_image family cassette path.
func CloudResourceContainerImageFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, cloudResourceContainerImageFamilyCassettePath)
}

// LoadCloudResourceContainerImageFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// cloud_resource_container_image counterpart to LoadBuiltFromFamilyOdu.
func LoadCloudResourceContainerImageFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, CloudResourceContainerImageFamilyOduName)
}

// KubernetesCorrelationFamilyCassetteFullPath joins repoRoot onto the committed
// kubernetes_correlation family cassette path.
func KubernetesCorrelationFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, kubernetesCorrelationFamilyCassettePath)
}

// SemanticEntityContainmentFamilyCassetteFullPath joins repoRoot onto the
// committed semantic_entity_containment family cassette path.
func SemanticEntityContainmentFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, semanticEntityContainmentFamilyCassettePath)
}

// LoadSemanticEntityContainmentFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the semantic extractor consumes, the
// semantic_entity_containment counterpart to the loaders above.
func LoadSemanticEntityContainmentFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, SemanticEntityContainmentFamilyOduName)
}

// LoadKubernetesCorrelationFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes the reducer's extractor consumes, the
// kubernetes_correlation counterpart to LoadS3LogsToFamilyOdu.
func LoadKubernetesCorrelationFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, KubernetesCorrelationFamilyOduName)
}

// CloudResourceFamilyCassetteFullPath joins repoRoot onto the committed
// cloud_resource family cassette path.
func CloudResourceFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, cloudResourceFamilyCassettePath)
}

// LoadCloudResourceFamilyOdu reads the committed cassette and projects it
// onto the fact envelopes the three extractors consume.
//
// Unlike every sibling loader it REQUIRES multiple scopes — exactly three,
// one per provider — because the family is three writers behind one port and
// each handler loads its own scope generation (#8358). The single-scope
// loaders reject that shape as ambiguous; here the scope is carried on every
// projected envelope, so which scope produced an edge stays unambiguous, and
// the guard partitions by fact kind and asserts one scope per provider.
// The strict decode and fail-closed emptiness rules are the shared ones.
func LoadCloudResourceFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	parsed, err := parseDirectFamilyCassette(cassettePath, CloudResourceFamilyOduName)
	if err != nil {
		return familyodu.Odu{}, err
	}
	if len(parsed.Scopes) != 3 {
		return familyodu.Odu{}, fmt.Errorf(
			"ifa: %s cassette %s declares %d scopes, want exactly 3 (one per provider writer); a missing scope would silently drop one writer's pairing from the proof",
			CloudResourceFamilyOduName, cassettePath, len(parsed.Scopes),
		)
	}
	var envelopes []facts.Envelope
	for _, scope := range parsed.Scopes {
		if len(scope.Facts) == 0 {
			return familyodu.Odu{}, fmt.Errorf("ifa: %s cassette %s scope %q carries no facts; an empty provider scope makes that writer's assertion vacuous", CloudResourceFamilyOduName, cassettePath, scope.ScopeID)
		}
		envelopes = append(envelopes, projectDirectFamilyScopeFacts(scope)...)
	}
	return familyodu.Odu{Name: CloudResourceFamilyOduName, Facts: envelopes}, nil
}

// CodeTaintEvidenceFamilyCassetteFullPath joins repoRoot onto the committed
// code_taint_evidence family cassette path.
func CodeTaintEvidenceFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeTaintEvidenceFamilyCassettePath)
}

// LoadCodeTaintEvidenceFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes the reducer's extractor consumes, the
// code_taint_evidence counterpart to LoadKubernetesCorrelationFamilyOdu.
func LoadCodeTaintEvidenceFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, CodeTaintEvidenceFamilyOduName)
}

// ObservabilityCoverageFamilyCassetteFullPath joins repoRoot onto the
// committed observability_coverage family cassette path.
func ObservabilityCoverageFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, observabilityCoverageFamilyCassettePath)
}

// LoadObservabilityCoverageFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// observability_coverage counterpart to LoadCodeTaintEvidenceFamilyOdu.
func LoadObservabilityCoverageFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, ObservabilityCoverageFamilyOduName)
}

// CodeInterprocEvidenceFamilyCassetteFullPath joins repoRoot onto the
// committed code_interproc_evidence family cassette path.
func CodeInterprocEvidenceFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeInterprocEvidenceFamilyCassettePath)
}

// LoadCodeInterprocEvidenceFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// code_interproc_evidence counterpart to LoadObservabilityCoverageFamilyOdu.
func LoadCodeInterprocEvidenceFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, CodeInterprocEvidenceFamilyOduName)
}

// CrossplaneSatisfiedByFamilyCassetteFullPath joins repoRoot onto the
// committed crossplane_satisfied_by family cassette path.
func CrossplaneSatisfiedByFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, crossplaneSatisfiedByFamilyCassettePath)
}

// LoadCrossplaneSatisfiedByFamilyOdu reads the committed cassette and projects
// it onto the fact envelopes the reducer's extractor consumes.
//
// Unlike the single-scope siblings it REQUIRES exactly two scopes -- the
// claims scope and the platform XRD scope -- because the family's Odù keeps
// Claim candidates and XRDs in separate repos, mirroring the handler's
// own-scope plus cross-scope XRD load. The single-scope loader rejects that
// shape as ambiguous; here the scope is carried on every projected envelope,
// so which scope produced an edge stays unambiguous. The strict decode and
// fail-closed emptiness rules are the shared ones.
func LoadCrossplaneSatisfiedByFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	parsed, err := parseDirectFamilyCassette(cassettePath, CrossplaneSatisfiedByFamilyOduName)
	if err != nil {
		return familyodu.Odu{}, err
	}
	if len(parsed.Scopes) != 2 {
		return familyodu.Odu{}, fmt.Errorf(
			"ifa: %s cassette %s declares %d scopes, want exactly 2 (claims plus platform XRD); a missing scope would silently drop one side of the resolution from the proof",
			CrossplaneSatisfiedByFamilyOduName, cassettePath, len(parsed.Scopes),
		)
	}
	var envelopes []facts.Envelope
	for _, scope := range parsed.Scopes {
		if len(scope.Facts) == 0 {
			return familyodu.Odu{}, fmt.Errorf("ifa: %s cassette %s scope %q carries no facts; an empty scope makes that side's assertion vacuous", CrossplaneSatisfiedByFamilyOduName, cassettePath, scope.ScopeID)
		}
		envelopes = append(envelopes, projectDirectFamilyScopeFacts(scope)...)
	}
	return familyodu.Odu{Name: CrossplaneSatisfiedByFamilyOduName, Facts: envelopes}, nil
}

// DerivedFromFamilyCassetteFullPath joins repoRoot onto the committed
// derived_from family cassette path.
func DerivedFromFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, derivedFromFamilyCassettePath)
}

// LoadDerivedFromFamilyOdu is the derived_from counterpart to
// LoadBuiltFromFamilyOdu: the family's single scope holds the owning
// repository, its single-base Dockerfile, the OCI manifests, and the
// deploy-only Kubernetes reference, mirroring the handler's own-scope load.
func LoadDerivedFromFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, DerivedFromFamilyOduName)
}

// IncidentRoutingEvidenceFamilyCassetteFullPath joins repoRoot onto the
// committed incident_routing_evidence family cassette path.
func IncidentRoutingEvidenceFamilyCassetteFullPath(repoRoot string) string {
	return filepath.Join(repoRoot, incidentRoutingEvidenceFamilyCassettePath)
}

// LoadIncidentRoutingEvidenceFamilyOdu reads the committed cassette and
// projects it onto the fact envelopes the reducer's extractor consumes, the
// incident_routing_evidence counterpart to LoadDerivedFromFamilyOdu: the
// family's single PagerDuty scope holds the incident anchor, the applied
// and observed service facts (plus the tombstoned applied negative
// control), and the PagerDutyDeclaration content_entity fact the production
// loader reads out of content_entities, mirroring the handler's own-scope
// fact load plus its service-name-scoped declared read.
func LoadIncidentRoutingEvidenceFamilyOdu(cassettePath string) (familyodu.Odu, error) {
	return loadDirectFamilyOdu(cassettePath, IncidentRoutingEvidenceFamilyOduName)
}

// parseDirectFamilyCassette is the shared strict decode behind the loaders.
//
// The decoder disallows unknown fields, so a typo anywhere in the envelope
// (e.g. "fact_knd" for "fact_kind") fails loudly at load time instead of
// silently decoding to a zero value and projecting the WRONG fact set from a
// cassette whose green gate run would then attest to something it never said.
// json.Decoder.Decode reads exactly one JSON value and stops, so the second
// Decode requiring io.EOF closes the trailing-content gap.
func parseDirectFamilyCassette(cassettePath, oduName string) (directFamilyCassetteFile, error) {
	raw, err := os.ReadFile(cassettePath) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return directFamilyCassetteFile{}, fmt.Errorf("ifa: read %s cassette %s: %w", oduName, cassettePath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed directFamilyCassetteFile
	if err := decoder.Decode(&parsed); err != nil {
		return directFamilyCassetteFile{}, fmt.Errorf("ifa: parse %s cassette %s: %w", oduName, cassettePath, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return directFamilyCassetteFile{}, fmt.Errorf("ifa: %s cassette %s has trailing content after its JSON object", oduName, cassettePath)
	}
	return parsed, nil
}

// projectDirectFamilyScopeFacts projects one cassette scope onto fact
// envelopes. FencingToken and the scope descriptors are deliberately not
// projected: the compiled Odù does not carry them, they are replay-transport
// concerns rather than fixture truth, and projecting them would make the
// lockstep comparison fail on fields no extractor reads.
func projectDirectFamilyScopeFacts(scope directFamilyCassetteScope) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, len(scope.Facts))
	for _, fact := range scope.Facts {
		envelopes = append(envelopes, facts.Envelope{
			ScopeID:          scope.ScopeID,
			GenerationID:     scope.GenerationID,
			FactKind:         fact.FactKind,
			SchemaVersion:    fact.SchemaVersion,
			StableFactKey:    fact.StableFactKey,
			CollectorKind:    fact.CollectorKind,
			SourceConfidence: fact.SourceConfidence,
			IsTombstone:      fact.IsTombstone,
			SourceRef:        facts.Ref{SourceURI: fact.SourceURI},
			Payload:          fact.Payload,
		})
	}
	return envelopes
}

// loadDirectFamilyOdu is the shared strict projection behind the
// single-scope loaders. It fails closed on an empty scope or fact list: an
// Odù carrying no facts makes every downstream assertion vacuous, which is
// the failure mode the whole exhaustiveness effort exists to remove.
func loadDirectFamilyOdu(cassettePath, oduName string) (familyodu.Odu, error) {
	parsed, err := parseDirectFamilyCassette(cassettePath, oduName)
	if err != nil {
		return familyodu.Odu{}, err
	}
	if len(parsed.Scopes) != 1 {
		return familyodu.Odu{}, fmt.Errorf(
			"ifa: %s cassette %s declares %d scopes, want exactly 1; a multi-scope fixture would make the expected-edge set ambiguous about which scope produced an edge",
			oduName, cassettePath, len(parsed.Scopes),
		)
	}
	scope := parsed.Scopes[0]
	if len(scope.Facts) == 0 {
		return familyodu.Odu{}, fmt.Errorf("ifa: %s cassette %s carries no facts; an empty Odù makes every assertion vacuous", oduName, cassettePath)
	}
	return familyodu.Odu{Name: oduName, Facts: projectDirectFamilyScopeFacts(scope)}, nil
}
