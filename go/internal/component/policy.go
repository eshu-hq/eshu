// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

const (
	// TrustModeDisabled rejects all optional components.
	TrustModeDisabled = "disabled"
	// TrustModeAllowlist accepts explicitly allowed component identities and
	// publishers.
	TrustModeAllowlist = "allowlist"
	// TrustModeStrict requires provenance verification. The first local slice
	// requires allowlist approval plus verified artifact provenance.
	TrustModeStrict = "strict"
)

// Policy describes local component trust rules.
type Policy struct {
	Mode               string
	AllowedIDs         []string
	AllowedPublishers  []string
	RevokedIDs         []string
	RevokedPublishers  []string
	CoreVersion        string
	Provenance         ProvenancePolicy
	ProvenanceVerifier ProvenanceVerifier
}

// VerificationResult reports whether a manifest is allowed by policy.
type VerificationResult struct {
	Allowed   bool      `json:"allowed"`
	Mode      string    `json:"mode"`
	Code      ErrorCode `json:"code,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Component string    `json:"component"`
	Publisher string    `json:"publisher"`
	Version   string    `json:"version"`
}

// Verify validates a manifest against the policy.
func (p Policy) Verify(manifest Manifest) VerificationResult {
	return p.VerifyContext(context.Background(), manifest)
}

// VerifyContext validates a manifest against the policy.
func (p Policy) VerifyContext(ctx context.Context, manifest Manifest) VerificationResult {
	if ctx == nil {
		ctx = context.Background()
	}
	mode := p.Mode
	if mode == "" {
		mode = TrustModeDisabled
	}
	if err := manifest.Validate(); err != nil {
		return VerificationResult{Mode: mode, Code: ErrorCodeInvalidManifest, Reason: err.Error()}
	}
	result := VerificationResult{
		Mode:      mode,
		Component: manifest.Metadata.ID,
		Publisher: manifest.Metadata.Publisher,
		Version:   manifest.Metadata.Version,
	}
	if err := verifyCompatibleCoreRangeSyntax(manifest.Spec.CompatibleCore); err != nil {
		result.Code = ErrorCodeInvalidManifest
		result.Reason = err.Error()
		return result
	}
	if contains(p.RevokedIDs, manifest.Metadata.ID) {
		result.Code = ErrorCodeRevokedPackage
		result.Reason = fmt.Sprintf("component %q is revoked", manifest.Metadata.ID)
		return result
	}
	if contains(p.RevokedPublishers, manifest.Metadata.Publisher) {
		result.Code = ErrorCodeRevokedPackage
		result.Reason = fmt.Sprintf("revoked publisher %q", manifest.Metadata.Publisher)
		return result
	}
	if err := verifyCompatibleCore(manifest.Spec.CompatibleCore, p.coreVersion()); err != nil {
		result.Code = ErrorCodeIncompatibleCore
		result.Reason = err.Error()
		return result
	}
	switch mode {
	case TrustModeDisabled:
		result.Code = ErrorCodeUntrustedPublisher
		result.Reason = "component trust policy is disabled"
		return result
	case TrustModeAllowlist:
		if !p.verifyAllowlist(manifest, &result) {
			return result
		}
		result.Allowed = true
		return result
	case TrustModeStrict:
		if !p.verifyAllowlist(manifest, &result) {
			return result
		}
		requirement, err := p.Provenance.requirement()
		if err != nil {
			result.Code = ErrorCodeOf(err)
			if result.Code == "" {
				result.Code = ErrorCodeProvenanceRequired
			}
			result.Reason = err.Error()
			return result
		}
		if p.ProvenanceVerifier == nil {
			result.Code = ErrorCodeProvenanceRequired
			result.Reason = "strict provenance verification requires a configured verifier"
			return result
		}
		if err := p.ProvenanceVerifier.VerifyProvenance(ctx, manifest, requirement); err != nil {
			result.Code = ErrorCodeOf(err)
			if result.Code == "" {
				result.Code = ErrorCodeProvenanceInvalid
			}
			result.Reason = err.Error()
			return result
		}
		result.Allowed = true
		return result
	default:
		result.Code = ErrorCodeInvalidInput
		result.Reason = fmt.Sprintf("unknown trust mode %q", mode)
		return result
	}
}

type coreComparator struct {
	operator string
	version  string
}

func (p Policy) coreVersion() string {
	if version := strings.TrimSpace(p.CoreVersion); version != "" {
		return version
	}
	return buildinfo.AppVersion()
}

func (p Policy) verifyAllowlist(manifest Manifest, result *VerificationResult) bool {
	if !contains(p.AllowedIDs, manifest.Metadata.ID) {
		result.Code = ErrorCodeUntrustedPublisher
		result.Reason = fmt.Sprintf("component %q is not allowlisted", manifest.Metadata.ID)
		return false
	}
	if !contains(p.AllowedPublishers, manifest.Metadata.Publisher) {
		result.Code = ErrorCodeUntrustedPublisher
		result.Reason = fmt.Sprintf("publisher %q is not allowlisted", manifest.Metadata.Publisher)
		return false
	}
	return true
}

func (p Policy) isZero() bool {
	return p.Mode == "" &&
		len(p.AllowedIDs) == 0 &&
		len(p.AllowedPublishers) == 0 &&
		len(p.RevokedIDs) == 0 &&
		len(p.RevokedPublishers) == 0 &&
		strings.TrimSpace(p.CoreVersion) == "" &&
		p.Provenance.isZero() &&
		p.ProvenanceVerifier == nil
}

func verifyCompatibleCore(rangeExpression, currentVersion string) error {
	comparators, err := parseCoreRange(rangeExpression)
	if err != nil {
		return fmt.Errorf("spec.compatibleCore %q is invalid: %w", rangeExpression, err)
	}
	current := strings.TrimSpace(currentVersion)
	if current == "dev" {
		return nil
	}
	normalizedCurrent := normalizeSemver(current)
	if !semver.IsValid(normalizedCurrent) {
		return fmt.Errorf("current Eshu core version %q is not semantic version", currentVersion)
	}
	for _, comparator := range comparators {
		comparison := semver.Compare(normalizedCurrent, comparator.version)
		if !comparator.matches(comparison) {
			return fmt.Errorf("component is incompatible with Eshu core %q; requires %q", currentVersion, rangeExpression)
		}
	}
	return nil
}

func verifyCompatibleCoreRangeSyntax(rangeExpression string) error {
	if _, err := parseCoreRange(rangeExpression); err != nil {
		return fmt.Errorf("spec.compatibleCore %q is invalid: %w", rangeExpression, err)
	}
	return nil
}

func parseCoreRange(rangeExpression string) ([]coreComparator, error) {
	fields := strings.Fields(rangeExpression)
	if len(fields) == 0 {
		return nil, fmt.Errorf("range is required")
	}
	comparators := make([]coreComparator, 0, len(fields))
	for _, field := range fields {
		comparator, err := parseCoreComparator(field)
		if err != nil {
			return nil, err
		}
		comparators = append(comparators, comparator)
	}
	return comparators, nil
}

func parseCoreComparator(raw string) (coreComparator, error) {
	for _, operator := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(raw, operator) {
			return newCoreComparator(operator, strings.TrimPrefix(raw, operator))
		}
	}
	return newCoreComparator("=", raw)
}

func newCoreComparator(operator, version string) (coreComparator, error) {
	normalized := normalizeSemver(version)
	if !semver.IsValid(normalized) {
		return coreComparator{}, fmt.Errorf("version %q is not semantic version", version)
	}
	return coreComparator{operator: operator, version: normalized}, nil
}

func (c coreComparator) matches(comparison int) bool {
	switch c.operator {
	case ">=":
		return comparison >= 0
	case ">":
		return comparison > 0
	case "<=":
		return comparison <= 0
	case "<":
		return comparison < 0
	case "=":
		return comparison == 0
	default:
		return false
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
