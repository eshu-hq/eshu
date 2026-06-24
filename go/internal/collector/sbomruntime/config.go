// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbomruntime provides the claim-driven hosted SBOM and attestation
// collector runtime.
package sbomruntime

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultMaxDocumentBytes = int64(10 * 1024 * 1024)
)

// SourceType identifies how a hosted SBOM or attestation document is located.
type SourceType string

const (
	// SourceTypeConfigured fetches an explicitly configured document URL.
	SourceTypeConfigured SourceType = "configured_source"
	// SourceTypeOCIReferrer fetches a document through an OCI referrer target.
	SourceTypeOCIReferrer SourceType = "oci_referrer"
)

// ArtifactKind identifies the typed source document family.
type ArtifactKind string

const (
	// ArtifactKindSBOM parses an SBOM document into SBOM facts.
	ArtifactKindSBOM ArtifactKind = "sbom"
	// ArtifactKindAttestation parses an in-toto attestation statement.
	ArtifactKindAttestation ArtifactKind = "attestation"
)

// DocumentFormat identifies the serialization format consumed by the runtime.
type DocumentFormat string

const (
	// DocumentFormatCycloneDX parses CycloneDX JSON SBOMs.
	DocumentFormatCycloneDX DocumentFormat = "cyclonedx"
	// DocumentFormatSPDX parses SPDX JSON SBOMs.
	DocumentFormatSPDX DocumentFormat = "spdx"
	// DocumentFormatInToto parses in-toto JSON attestation statements.
	DocumentFormatInToto DocumentFormat = "in_toto"
)

// SourceConfig configures a claim-driven hosted SBOM and attestation source.
type SourceConfig struct {
	CollectorInstanceID string
	Targets             []TargetConfig
	Provider            DocumentProvider
	Now                 func() time.Time
}

// TargetConfig describes one bounded SBOM or attestation source target.
type TargetConfig struct {
	ScopeID            string
	SourceType         SourceType
	ArtifactKind       ArtifactKind
	DocumentFormat     DocumentFormat
	Provider           string
	Registry           string
	RegistryHost       string
	Region             string
	AWSProfile         string
	Repository         string
	DocumentURL        string
	SourceURI          string
	SourceRecordID     string
	SubjectDigest      string
	ReferrerDigest     string
	VerificationResult string
	VerificationPolicy string
	Username           string
	Password           string
	BearerToken        string
	MaxBytes           int64
}

func (t TargetConfig) validate() (TargetConfig, error) {
	t.ScopeID = strings.TrimSpace(t.ScopeID)
	t.SourceType = SourceType(strings.TrimSpace(string(t.SourceType)))
	t.ArtifactKind = ArtifactKind(strings.TrimSpace(string(t.ArtifactKind)))
	t.DocumentFormat = DocumentFormat(strings.TrimSpace(string(t.DocumentFormat)))
	t.Provider = strings.TrimSpace(t.Provider)
	t.Registry = strings.TrimRight(strings.TrimSpace(t.Registry), "/")
	t.RegistryHost = strings.TrimRight(strings.TrimSpace(t.RegistryHost), "/")
	t.Region = strings.TrimSpace(t.Region)
	t.AWSProfile = strings.TrimSpace(t.AWSProfile)
	t.Repository = strings.Trim(strings.TrimSpace(t.Repository), "/")
	t.DocumentURL = strings.TrimSpace(t.DocumentURL)
	t.SourceURI = strings.TrimSpace(t.SourceURI)
	t.SourceRecordID = strings.TrimSpace(t.SourceRecordID)
	t.SubjectDigest = strings.TrimSpace(t.SubjectDigest)
	t.ReferrerDigest = strings.TrimSpace(t.ReferrerDigest)
	t.VerificationResult = strings.TrimSpace(t.VerificationResult)
	t.VerificationPolicy = strings.TrimSpace(t.VerificationPolicy)
	t.Username = strings.TrimSpace(t.Username)
	t.Password = strings.TrimSpace(t.Password)
	t.BearerToken = strings.TrimSpace(t.BearerToken)
	if t.ScopeID == "" {
		return TargetConfig{}, fmt.Errorf("scope_id is required")
	}
	if err := validateArtifactKind(t.ArtifactKind); err != nil {
		return TargetConfig{}, err
	}
	if err := validateDocumentFormat(t.ArtifactKind, t.DocumentFormat); err != nil {
		return TargetConfig{}, err
	}
	switch t.SourceType {
	case SourceTypeConfigured:
		if t.DocumentURL == "" {
			return TargetConfig{}, fmt.Errorf("document_url is required")
		}
	case SourceTypeOCIReferrer:
		if t.Provider == "" {
			return TargetConfig{}, fmt.Errorf("provider is required")
		}
		if t.Registry == "" {
			return TargetConfig{}, fmt.Errorf("registry is required")
		}
		if t.Repository == "" {
			return TargetConfig{}, fmt.Errorf("repository is required")
		}
		if t.SubjectDigest == "" {
			return TargetConfig{}, fmt.Errorf("subject_digest is required")
		}
		if t.ReferrerDigest == "" {
			return TargetConfig{}, fmt.Errorf("referrer_digest is required")
		}
	default:
		return TargetConfig{}, fmt.Errorf("unsupported source_type %q", t.SourceType)
	}
	if t.MaxBytes == 0 {
		t.MaxBytes = defaultMaxDocumentBytes
	}
	if t.MaxBytes < 0 {
		return TargetConfig{}, fmt.Errorf("max_bytes must not be negative")
	}
	if t.SourceURI == "" {
		t.SourceURI = firstNonBlank(t.DocumentURL, t.Registry)
	}
	if t.SourceRecordID == "" {
		t.SourceRecordID = firstNonBlank(t.ReferrerDigest, t.DocumentURL, t.ScopeID)
	}
	return t, nil
}

func validateArtifactKind(kind ArtifactKind) error {
	switch kind {
	case ArtifactKindSBOM, ArtifactKindAttestation:
		return nil
	case "":
		return fmt.Errorf("artifact_kind is required")
	default:
		return fmt.Errorf("unsupported artifact_kind %q", kind)
	}
}

func validateDocumentFormat(kind ArtifactKind, format DocumentFormat) error {
	switch format {
	case DocumentFormatCycloneDX, DocumentFormatSPDX:
		if kind != ArtifactKindSBOM {
			return fmt.Errorf("document_format %q is only valid for SBOM targets", format)
		}
		return nil
	case DocumentFormatInToto:
		if kind != ArtifactKindAttestation {
			return fmt.Errorf("document_format %q is only valid for attestation targets", format)
		}
		return nil
	case "":
		return fmt.Errorf("document_format is required")
	default:
		return fmt.Errorf("unsupported document_format %q", format)
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
