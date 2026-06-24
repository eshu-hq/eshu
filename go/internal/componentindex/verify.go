// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package componentindex

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	indexAPIVersion = "eshu.dev/community-extension-index/v1alpha1"
	indexKind       = "CommunityExtensionIndex"

	// ChannelExperimental marks an indexed package that is not trusted
	// automatically and has only basic metadata review.
	ChannelExperimental = "experimental"
	// ChannelCommunityMaintained marks a reviewed package maintained outside
	// the Eshu core project.
	ChannelCommunityMaintained = "community-maintained"
	// ChannelVerified marks a package whose metadata, proof, and provenance
	// posture passed maintainer review.
	ChannelVerified = "verified"
	// ChannelFirstParty marks a package built and released by the Eshu project.
	ChannelFirstParty = "first-party"
)

var (
	sha256DigestPattern   = regexp.MustCompile(`^sha256:[A-Fa-f0-9]{64}$`)
	artifactDigestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)
	identifierPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$|^[a-z0-9]$`)
)

// IssueCode is a stable verifier failure class.
type IssueCode string

const (
	// IssueMissingMetadata reports a required index or entry field is missing.
	IssueMissingMetadata IssueCode = "missing_metadata"
	// IssueDuplicateComponentID reports two index entries claim the same
	// component ID.
	IssueDuplicateComponentID IssueCode = "duplicate_component_id"
	// IssueDuplicateFactKind reports two entries claim the same emitted fact
	// kind.
	IssueDuplicateFactKind IssueCode = "duplicate_fact_kind"
	// IssueMalformedDigest reports a digest field is present but malformed.
	IssueMalformedDigest IssueCode = "malformed_digest"
	// IssueMutableArtifactTag reports an artifact reference lacks a digest pin.
	IssueMutableArtifactTag IssueCode = "mutable_artifact_tag"
	// IssueUnsupportedChannel reports a lifecycle channel outside the v1 set.
	IssueUnsupportedChannel IssueCode = "unsupported_channel"
	// IssueMissingReviewLink reports an index entry without a maintainer review
	// link.
	IssueMissingReviewLink IssueCode = "missing_review_link"
	// IssueRevokedInstallable reports a revoked entry that is still marked
	// installable.
	IssueRevokedInstallable IssueCode = "revoked_installable"
	// IssueUnsupportedFactKind reports a core-owned, non-namespaced, or
	// non-canonical fact-kind claim.
	IssueUnsupportedFactKind IssueCode = "unsupported_fact_kind"
	// IssueUnsupportedSchemaVersion reports an invalid fact schema version.
	IssueUnsupportedSchemaVersion IssueCode = "unsupported_schema_version"
	// IssueUnsupportedSourceConfidence reports a source-confidence value that
	// optional components cannot emit.
	IssueUnsupportedSourceConfidence IssueCode = "unsupported_source_confidence"
	// IssueMissingConsumerContract reports an entry without a reducer/query
	// consumer contract for declared facts.
	IssueMissingConsumerContract IssueCode = "missing_consumer_contract"
	// IssueMissingProvenanceSignature reports an entry that requires provenance
	// but does not point at signature evidence.
	IssueMissingProvenanceSignature IssueCode = "missing_provenance_signature"
	// IssueMissingConformanceProof reports an entry without a conformance proof
	// artifact.
	IssueMissingConformanceProof IssueCode = "missing_conformance_proof"
	// IssueFailedConformanceProof reports a conformance proof whose status is
	// not accepted for publication metadata.
	IssueFailedConformanceProof IssueCode = "failed_conformance_proof"
	// IssueMissingCompatibilityBadge reports an entry without complete badge
	// metadata for marketplace readiness.
	IssueMissingCompatibilityBadge IssueCode = "missing_compatibility_badge"
	// IssueUnsupportedPublicationStatus reports a publication status outside
	// the v1 index vocabulary.
	IssueUnsupportedPublicationStatus IssueCode = "unsupported_publication_status"
	// IssuePlaceholderPublicationMetadata reports placeholder-only metadata on
	// an entry claiming published marketplace readiness.
	IssuePlaceholderPublicationMetadata IssueCode = "placeholder_publication_metadata"
)

// Index is the top-level static community extension index document.
type Index struct {
	APIVersion string  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string  `yaml:"kind" json:"kind"`
	Entries    []Entry `yaml:"entries" json:"entries"`
}

// Entry describes one reviewed component package candidate.
type Entry struct {
	ComponentID        string             `yaml:"componentId" json:"componentId"`
	Publisher          string             `yaml:"publisher" json:"publisher"`
	Version            string             `yaml:"version" json:"version"`
	LifecycleChannel   string             `yaml:"lifecycleChannel" json:"lifecycleChannel"`
	Installable        bool               `yaml:"installable" json:"installable"`
	ManifestDigest     string             `yaml:"manifestDigest" json:"manifestDigest"`
	Artifacts          []ArtifactRef      `yaml:"artifacts" json:"artifacts"`
	CompatibleCore     string             `yaml:"compatibleCore" json:"compatibleCore"`
	ComponentType      string             `yaml:"componentType" json:"componentType"`
	CollectorKinds     []string           `yaml:"collectorKinds" json:"collectorKinds"`
	EmittedFacts       []FactClaim        `yaml:"emittedFacts" json:"emittedFacts"`
	ConsumerContracts  ConsumerContracts  `yaml:"consumerContracts" json:"consumerContracts"`
	Telemetry          Telemetry          `yaml:"telemetry" json:"telemetry"`
	Source             SourceRef          `yaml:"source" json:"source"`
	Review             ReviewRef          `yaml:"review" json:"review"`
	Provenance         Provenance         `yaml:"provenance" json:"provenance"`
	Conformance        ConformanceProof   `yaml:"conformance" json:"conformance"`
	Publication        Publication        `yaml:"publication" json:"publication"`
	CompatibilityBadge CompatibilityBadge `yaml:"compatibilityBadge" json:"compatibilityBadge"`
	Revocation         Revocation         `yaml:"revocation" json:"revocation"`
}

// ArtifactRef points at a digest-pinned artifact.
type ArtifactRef struct {
	Image string `yaml:"image" json:"image"`
}

// FactClaim declares an emitted fact family claimed by an index entry.
type FactClaim struct {
	Kind             string   `yaml:"kind" json:"kind"`
	SchemaVersions   []string `yaml:"schemaVersions" json:"schemaVersions"`
	SourceConfidence []string `yaml:"sourceConfidence" json:"sourceConfidence"`
}

// ConsumerContracts declares downstream consumers required by an entry.
type ConsumerContracts struct {
	Reducer ReducerContract `yaml:"reducer" json:"reducer"`
}

// ReducerContract names reducer phases expected by an entry.
type ReducerContract struct {
	Phases []string `yaml:"phases" json:"phases"`
}

// Telemetry declares extension-owned telemetry names.
type Telemetry struct {
	MetricsPrefix string `yaml:"metricsPrefix" json:"metricsPrefix"`
}

// SourceRef identifies the public source location for review.
type SourceRef struct {
	Repository string `yaml:"repository" json:"repository"`
}

// ReviewRef identifies the maintainer review evidence for the entry.
type ReviewRef struct {
	PR string `yaml:"pr" json:"pr"`
}

// Provenance declares the provenance posture required for the entry.
type Provenance struct {
	Required  bool   `yaml:"required" json:"required"`
	Mode      string `yaml:"mode" json:"mode"`
	Signature string `yaml:"signature" json:"signature"`
}

// ConformanceProof points at the reviewed extension conformance artifact.
type ConformanceProof struct {
	SchemaVersion string `yaml:"schemaVersion" json:"schemaVersion"`
	Status        string `yaml:"status" json:"status"`
	ProofURI      string `yaml:"proofUri" json:"proofUri"`
}

// Publication declares the marketplace publication state for an entry.
type Publication struct {
	Status string `yaml:"status" json:"status"`
}

// CompatibilityBadge records the deterministic badge inputs shown for an entry.
type CompatibilityBadge struct {
	ManifestAPIVersion  string `yaml:"manifestApiVersion" json:"manifestApiVersion"`
	ManifestDigest      string `yaml:"manifestDigest" json:"manifestDigest"`
	CompatibleCore      string `yaml:"compatibleCore" json:"compatibleCore"`
	ArtifactDigest      string `yaml:"artifactDigest" json:"artifactDigest"`
	SignatureStatus     string `yaml:"signatureStatus" json:"signatureStatus"`
	ProvenanceStatus    string `yaml:"provenanceStatus" json:"provenanceStatus"`
	RuntimeProtocol     string `yaml:"runtimeProtocol" json:"runtimeProtocol"`
	Adapter             string `yaml:"adapter" json:"adapter"`
	ConformanceProofURI string `yaml:"conformanceProofUri" json:"conformanceProofUri"`
	ConformanceStatus   string `yaml:"conformanceStatus" json:"conformanceStatus"`
	PolicyResult        string `yaml:"policyResult" json:"policyResult"`
}

// Revocation describes whether an entry is currently revoked.
type Revocation struct {
	Revoked bool   `yaml:"revoked" json:"revoked"`
	Reason  string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// Issue is one deterministic verifier finding.
type Issue struct {
	Code        IssueCode `json:"code"`
	Field       string    `json:"field"`
	ComponentID string    `json:"component_id,omitempty"`
	Message     string    `json:"message"`
}

// Report is the result of validating one index document.
type Report struct {
	Valid  bool    `json:"valid"`
	Issues []Issue `json:"issues,omitempty"`
}

// Validate checks a static community extension index without network or
// registry access.
func Validate(index Index) Report {
	verifier := indexVerifier{
		componentIDs: make(map[string]struct{}),
		factKinds:    make(map[string]string),
	}
	verifier.validate(index)
	return Report{
		Valid:  len(verifier.issues) == 0,
		Issues: verifier.issues,
	}
}

type indexVerifier struct {
	issues       []Issue
	componentIDs map[string]struct{}
	factKinds    map[string]string
}

func (v *indexVerifier) validate(index Index) {
	if strings.TrimSpace(index.APIVersion) != indexAPIVersion {
		v.add(IssueMissingMetadata, "apiVersion", "", fmt.Sprintf("apiVersion must be %q", indexAPIVersion))
	}
	if strings.TrimSpace(index.Kind) != indexKind {
		v.add(IssueMissingMetadata, "kind", "", fmt.Sprintf("kind must be %q", indexKind))
	}
	if len(index.Entries) == 0 {
		v.add(IssueMissingMetadata, "entries", "", "entries must include at least one extension")
		return
	}
	for i, entry := range index.Entries {
		v.validateEntry(i, entry)
	}
}

func (v *indexVerifier) validateEntry(index int, entry Entry) {
	prefix := fmt.Sprintf("entries[%d]", index)
	componentID := strings.TrimSpace(entry.ComponentID)
	if componentID == "" {
		v.add(IssueMissingMetadata, prefix+".componentId", "", "componentId is required")
	} else if _, exists := v.componentIDs[componentID]; exists {
		v.add(IssueDuplicateComponentID, prefix+".componentId", componentID, "componentId is already present in the index")
	} else {
		v.componentIDs[componentID] = struct{}{}
	}
	if strings.TrimSpace(entry.Publisher) == "" {
		v.add(IssueMissingMetadata, prefix+".publisher", componentID, "publisher is required")
	}
	if strings.TrimSpace(entry.Version) == "" {
		v.add(IssueMissingMetadata, prefix+".version", componentID, "version is required")
	}
	v.validateRequiredEntryShape(prefix, componentID, entry)
	v.validateChannel(prefix, componentID, entry.LifecycleChannel)
	v.validateDigest(prefix+".manifestDigest", componentID, entry.ManifestDigest)
	v.validateArtifacts(prefix, componentID, entry.Artifacts)
	v.validateFacts(prefix, componentID, entry.EmittedFacts)
	v.validateConsumerContracts(prefix, componentID, entry.ConsumerContracts)
	v.validateConformance(prefix, componentID, entry.Conformance)
	v.validatePublication(prefix, componentID, entry)
	if strings.TrimSpace(entry.Review.PR) == "" {
		v.add(IssueMissingReviewLink, prefix+".review.pr", componentID, "review PR link is required")
	}
	if entry.Revocation.Revoked && entry.Installable {
		v.add(IssueRevokedInstallable, prefix+".installable", componentID, "revoked entries must not be installable")
	}
}

func (v *indexVerifier) validateRequiredEntryShape(prefix, componentID string, entry Entry) {
	if strings.TrimSpace(entry.CompatibleCore) == "" {
		v.add(IssueMissingMetadata, prefix+".compatibleCore", componentID, "compatibleCore is required")
	}
	if strings.TrimSpace(entry.ComponentType) == "" {
		v.add(IssueMissingMetadata, prefix+".componentType", componentID, "componentType is required")
	}
	if len(entry.CollectorKinds) == 0 {
		v.add(IssueMissingMetadata, prefix+".collectorKinds", componentID, "at least one collector kind is required")
	}
	if strings.TrimSpace(entry.Telemetry.MetricsPrefix) == "" {
		v.add(IssueMissingMetadata, prefix+".telemetry.metricsPrefix", componentID, "telemetry metrics prefix is required")
	}
	if strings.TrimSpace(entry.Source.Repository) == "" {
		v.add(IssueMissingMetadata, prefix+".source.repository", componentID, "source repository is required")
	}
	if entry.Provenance.Required && strings.TrimSpace(entry.Provenance.Mode) == "" {
		v.add(IssueMissingMetadata, prefix+".provenance.mode", componentID, "provenance mode is required when provenance is required")
	}
	if entry.Provenance.Required && strings.TrimSpace(entry.Provenance.Signature) == "" {
		v.add(IssueMissingProvenanceSignature, prefix+".provenance.signature", componentID, "signature evidence is required when provenance is required")
	}
}

func (v *indexVerifier) validateChannel(prefix, componentID, channel string) {
	switch strings.TrimSpace(channel) {
	case ChannelExperimental, ChannelCommunityMaintained, ChannelVerified, ChannelFirstParty:
		return
	default:
		v.add(IssueUnsupportedChannel, prefix+".lifecycleChannel", componentID, fmt.Sprintf("unsupported lifecycle channel %q", channel))
	}
}

func (v *indexVerifier) validateDigest(field, componentID, digest string) {
	trimmed := strings.TrimSpace(digest)
	if trimmed == "" {
		v.add(IssueMissingMetadata, field, componentID, "digest is required")
		return
	}
	if !sha256DigestPattern.MatchString(trimmed) {
		v.add(IssueMalformedDigest, field, componentID, "digest must use sha256 with 64 hex characters")
	}
}

func (v *indexVerifier) validateArtifacts(prefix, componentID string, artifacts []ArtifactRef) {
	if len(artifacts) == 0 {
		v.add(IssueMissingMetadata, prefix+".artifacts", componentID, "at least one artifact is required")
		return
	}
	for i, artifact := range artifacts {
		field := fmt.Sprintf("%s.artifacts[%d].image", prefix, i)
		image := strings.TrimSpace(artifact.Image)
		if image == "" {
			v.add(IssueMissingMetadata, field, componentID, "artifact image is required")
			continue
		}
		if !strings.Contains(image, "@sha256:") {
			v.add(IssueMutableArtifactTag, field, componentID, "artifact image must be digest pinned")
			continue
		}
		if !artifactDigestPattern.MatchString(image) {
			v.add(IssueMalformedDigest, field, componentID, "artifact digest must use sha256 with 64 hex characters")
		}
	}
}

func (v *indexVerifier) validateFacts(prefix, componentID string, factClaims []FactClaim) {
	if len(factClaims) == 0 {
		v.add(IssueMissingMetadata, prefix+".emittedFacts", componentID, "at least one emitted fact kind is required")
		return
	}
	for i, fact := range factClaims {
		field := fmt.Sprintf("%s.emittedFacts[%d].kind", prefix, i)
		kind := strings.TrimSpace(fact.Kind)
		if kind == "" {
			v.add(IssueMissingMetadata, field, componentID, "fact kind is required")
			continue
		}
		if kind != fact.Kind {
			v.add(IssueUnsupportedFactKind, field, componentID, "fact kind must be canonical without surrounding whitespace")
			continue
		}
		if !identifierPattern.MatchString(kind) {
			v.add(IssueUnsupportedFactKind, field, componentID, "fact kind must use lowercase letters, numbers, dots, underscores, or hyphens")
			continue
		}
		if facts.IsCoreFactKind(kind) {
			v.add(IssueUnsupportedFactKind, field, componentID, "core-owned fact kinds cannot be claimed by community extensions")
			continue
		}
		if !strings.Contains(kind, ".") {
			v.add(IssueUnsupportedFactKind, field, componentID, "fact kind must be namespaced with a collision-resistant prefix")
			continue
		}
		if len(fact.SchemaVersions) == 0 {
			v.add(IssueMissingMetadata, fmt.Sprintf("%s.emittedFacts[%d].schemaVersions", prefix, i), componentID, "at least one schema version is required")
		}
		v.validateSchemaVersions(prefix, componentID, i, fact.SchemaVersions)
		if len(fact.SourceConfidence) == 0 {
			v.add(IssueMissingMetadata, fmt.Sprintf("%s.emittedFacts[%d].sourceConfidence", prefix, i), componentID, "at least one source confidence value is required")
		}
		v.validateSourceConfidence(prefix, componentID, i, fact.SourceConfidence)
		if owner, exists := v.factKinds[kind]; exists {
			v.add(IssueDuplicateFactKind, field, componentID, fmt.Sprintf("fact kind is already claimed by %q", owner))
			continue
		}
		v.factKinds[kind] = componentID
	}
}

func (v *indexVerifier) validateSchemaVersions(prefix, componentID string, factIndex int, versions []string) {
	for i, version := range versions {
		field := fmt.Sprintf("%s.emittedFacts[%d].schemaVersions[%d]", prefix, factIndex, i)
		normalized := normalizeSchemaVersion(version)
		if version != strings.TrimSpace(version) || !semver.IsValid(normalized) {
			v.add(IssueUnsupportedSchemaVersion, field, componentID, "schema version must be semantic versioning")
		}
	}
}

func (v *indexVerifier) validateSourceConfidence(prefix, componentID string, factIndex int, values []string) {
	for i, value := range values {
		field := fmt.Sprintf("%s.emittedFacts[%d].sourceConfidence[%d]", prefix, factIndex, i)
		if value != strings.TrimSpace(value) {
			v.add(IssueUnsupportedSourceConfidence, field, componentID, "source confidence must be canonical without surrounding whitespace")
			continue
		}
		if err := facts.ValidateSourceConfidence(value); err != nil || value == facts.SourceConfidenceUnknown {
			v.add(IssueUnsupportedSourceConfidence, field, componentID, "source confidence must be one of observed, reported, inferred, or derived")
		}
	}
}

func (v *indexVerifier) validateConsumerContracts(prefix, componentID string, contracts ConsumerContracts) {
	if len(contracts.Reducer.Phases) == 0 {
		v.add(IssueMissingConsumerContract, prefix+".consumerContracts.reducer.phases", componentID, "at least one reducer consumer phase is required")
		return
	}
	for i, phase := range contracts.Reducer.Phases {
		if strings.TrimSpace(phase) == "" {
			v.add(IssueMissingConsumerContract, fmt.Sprintf("%s.consumerContracts.reducer.phases[%d]", prefix, i), componentID, "reducer phase must not be empty")
		}
	}
}

func (v *indexVerifier) validateConformance(prefix, componentID string, proof ConformanceProof) {
	if strings.TrimSpace(proof.SchemaVersion) == "" {
		v.add(IssueMissingConformanceProof, prefix+".conformance.schemaVersion", componentID, "conformance proof schema version is required")
	}
	if strings.TrimSpace(proof.Status) == "" {
		v.add(IssueMissingConformanceProof, prefix+".conformance.status", componentID, "conformance proof status is required")
	} else if strings.TrimSpace(proof.Status) != "passed" {
		v.add(IssueFailedConformanceProof, prefix+".conformance.status", componentID, "conformance proof status must be passed")
	}
	if strings.TrimSpace(proof.ProofURI) == "" {
		v.add(IssueMissingConformanceProof, prefix+".conformance.proofUri", componentID, "conformance proof URI is required")
	}
}

func normalizeSchemaVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	return "v" + trimmed
}

func (v *indexVerifier) add(code IssueCode, field, componentID, message string) {
	v.issues = append(v.issues, Issue{
		Code:        code,
		Field:       field,
		ComponentID: componentID,
		Message:     message,
	})
}
