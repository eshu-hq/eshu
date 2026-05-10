package component

import (
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
	// fails closed for strict mode until a real verifier is wired in.
	TrustModeStrict = "strict"
)

// Policy describes local component trust rules.
type Policy struct {
	Mode              string
	AllowedIDs        []string
	AllowedPublishers []string
	RevokedIDs        []string
	RevokedPublishers []string
	CoreVersion       string
}

// VerificationResult reports whether a manifest is allowed by policy.
type VerificationResult struct {
	Allowed   bool
	Mode      string
	Reason    string
	Component string
	Publisher string
	Version   string
}

// Verify validates a manifest against the policy.
func (p Policy) Verify(manifest Manifest) VerificationResult {
	mode := p.Mode
	if mode == "" {
		mode = TrustModeDisabled
	}
	if err := manifest.Validate(); err != nil {
		return VerificationResult{Mode: mode, Reason: err.Error()}
	}
	result := VerificationResult{
		Mode:      mode,
		Component: manifest.Metadata.ID,
		Publisher: manifest.Metadata.Publisher,
		Version:   manifest.Metadata.Version,
	}
	if err := verifyCompatibleCore(manifest.Spec.CompatibleCore, p.coreVersion()); err != nil {
		result.Reason = err.Error()
		return result
	}
	if contains(p.RevokedIDs, manifest.Metadata.ID) {
		result.Reason = fmt.Sprintf("component %q is revoked", manifest.Metadata.ID)
		return result
	}
	if contains(p.RevokedPublishers, manifest.Metadata.Publisher) {
		result.Reason = fmt.Sprintf("revoked publisher %q", manifest.Metadata.Publisher)
		return result
	}
	switch mode {
	case TrustModeDisabled:
		result.Reason = "component trust policy is disabled"
		return result
	case TrustModeAllowlist:
		if !contains(p.AllowedIDs, manifest.Metadata.ID) {
			result.Reason = fmt.Sprintf("component %q is not allowlisted", manifest.Metadata.ID)
			return result
		}
		if !contains(p.AllowedPublishers, manifest.Metadata.Publisher) {
			result.Reason = fmt.Sprintf("publisher %q is not allowlisted", manifest.Metadata.Publisher)
			return result
		}
		result.Allowed = true
		return result
	case TrustModeStrict:
		result.Reason = "strict provenance verification is not available in this build"
		return result
	default:
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
