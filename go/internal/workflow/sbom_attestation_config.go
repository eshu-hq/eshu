// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type sbomAttestationCollectorConfiguration struct {
	Targets []sbomAttestationTargetConfiguration `json:"targets"`
}

type sbomAttestationTargetConfiguration struct {
	ScopeID            string `json:"scope_id"`
	SourceType         string `json:"source_type"`
	ArtifactKind       string `json:"artifact_kind"`
	DocumentFormat     string `json:"document_format"`
	Provider           string `json:"provider"`
	Registry           string `json:"registry"`
	Repository         string `json:"repository"`
	DocumentURL        string `json:"document_url"`
	SourceURI          string `json:"source_uri"`
	SourceRecordID     string `json:"source_record_id"`
	SubjectDigest      string `json:"subject_digest"`
	ReferrerDigest     string `json:"referrer_digest"`
	VerificationResult string `json:"verification_result"`
	VerificationPolicy string `json:"verification_policy"`
	MaxBytes           int64  `json:"max_bytes"`
}

// ValidateSBOMAttestationCollectorConfiguration checks bounded hosted SBOM and
// attestation targets without fetching registry or document content.
func ValidateSBOMAttestationCollectorConfiguration(raw string) error {
	var decoded sbomAttestationCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode SBOM attestation collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("SBOM attestation collector configuration requires targets")
	}
	seen := map[string]struct{}{}
	for i, target := range decoded.Targets {
		if err := validateSBOMAttestationTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate SBOM attestation target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func validateSBOMAttestationTargetConfiguration(target sbomAttestationTargetConfiguration) error {
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	sourceType := strings.TrimSpace(target.SourceType)
	if err := validateSBOMAttestationArtifact(target.ArtifactKind); err != nil {
		return err
	}
	if err := validateSBOMAttestationDocumentFormat(target.ArtifactKind, target.DocumentFormat); err != nil {
		return err
	}
	switch sourceType {
	case "configured_source":
		if err := validateSBOMAttestationURL("document_url", target.DocumentURL, false); err != nil {
			return err
		}
	case "oci_referrer":
		if strings.TrimSpace(target.Provider) == "" {
			return fmt.Errorf("provider is required")
		}
		if err := validateSBOMAttestationURL("registry", target.Registry, true); err != nil {
			return err
		}
		if strings.TrimSpace(target.Repository) == "" {
			return fmt.Errorf("repository is required")
		}
		if strings.TrimSpace(target.SubjectDigest) == "" {
			return fmt.Errorf("subject_digest is required")
		}
		if strings.TrimSpace(target.ReferrerDigest) == "" {
			return fmt.Errorf("referrer_digest is required")
		}
		if strings.TrimSpace(target.DocumentURL) != "" {
			if err := validateSBOMAttestationURL("document_url", target.DocumentURL, false); err != nil {
				return err
			}
		}
	case "":
		return fmt.Errorf("source_type is required")
	default:
		return fmt.Errorf("unsupported source_type %q", sourceType)
	}
	if target.MaxBytes < 0 {
		return fmt.Errorf("max_bytes must not be negative")
	}
	return nil
}

func validateSBOMAttestationArtifact(raw string) error {
	switch strings.TrimSpace(raw) {
	case "sbom", "attestation":
		return nil
	case "":
		return fmt.Errorf("artifact_kind is required")
	default:
		return fmt.Errorf("unsupported artifact_kind %q", strings.TrimSpace(raw))
	}
}

func validateSBOMAttestationDocumentFormat(kindRaw, formatRaw string) error {
	kind := strings.TrimSpace(kindRaw)
	format := strings.TrimSpace(formatRaw)
	switch format {
	case "cyclonedx", "spdx":
		if kind != "sbom" {
			return fmt.Errorf("document_format %q is only valid for SBOM targets", format)
		}
		return nil
	case "in_toto":
		if kind != "attestation" {
			return fmt.Errorf("document_format %q is only valid for attestation targets", format)
		}
		return nil
	case "":
		return fmt.Errorf("document_format is required")
	default:
		return fmt.Errorf("unsupported document_format %q", format)
	}
}

func validateSBOMAttestationURL(field, raw string, requireHTTPS bool) error {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	if !requireHTTPS && parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	return nil
}
