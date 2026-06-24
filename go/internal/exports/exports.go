// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

import (
	"fmt"
	"strings"
	"time"
)

// Format identifies one supported export wire format.
//
// Format values are stable strings that appear in CLI flags, MCP/API request
// bodies, and operator logs. Renaming a Format is a wire break.
type Format string

const (
	// FormatSARIF is the SARIF v2.1.0 static analysis result format.
	FormatSARIF Format = "sarif"
	// FormatCycloneDXBOV is the CycloneDX Bill of Vulnerabilities format
	// (vulnerability-enriched SBOM). Reserved; exporter ships in a follow-up.
	FormatCycloneDXBOV Format = "cyclonedx-bov"
	// FormatSPDX is the SPDX 2.3 component/relationship format. Reserved;
	// exporter ships in a follow-up.
	FormatSPDX Format = "spdx"
	// FormatGitHubDependencySnapshot is the GitHub dependency snapshot JSON
	// format. Reserved; exporter ships in a follow-up.
	FormatGitHubDependencySnapshot Format = "github-dependency-snapshot"
)

// String returns the wire string for the format.
func (f Format) String() string {
	return string(f)
}

// ScopeKind identifies which target field bounds an export snapshot.
type ScopeKind string

const (
	// ScopeKindRepository scopes the export to one repository.
	ScopeKindRepository ScopeKind = "repository"
	// ScopeKindImageDigest scopes the export to one container image digest.
	ScopeKindImageDigest ScopeKind = "image_digest"
	// ScopeKindPackage scopes the export to one canonical package id.
	ScopeKindPackage ScopeKind = "package"
	// ScopeKindAdvisory scopes the export to one advisory (CVE or GHSA).
	ScopeKindAdvisory ScopeKind = "advisory"
)

// Scope is the bounded target of one export request.
//
// Exactly one of the identifier fields must be set; mixed scope is rejected
// because exporters drop findings and components that disagree with the scope
// target, and a snapshot that targets two things at once cannot be filtered
// safely.
type Scope struct {
	Kind          ScopeKind
	RepositoryID  string
	SubjectDigest string
	PackageID     string
	AdvisoryID    string
}

// Validate enforces single-target scope.
func (s Scope) Validate() error {
	count := 0
	if s.RepositoryID != "" {
		count++
	}
	if s.SubjectDigest != "" {
		count++
	}
	if s.PackageID != "" {
		count++
	}
	if s.AdvisoryID != "" {
		count++
	}
	if count == 0 {
		return fmt.Errorf("scope must set one of repository_id, subject_digest, package_id, advisory_id")
	}
	if count > 1 {
		return fmt.Errorf("scope must set exactly one target identifier; got %d", count)
	}
	if err := s.Kind.validate(); err != nil {
		return err
	}
	if err := s.kindMatchesIdentifier(); err != nil {
		return err
	}
	return nil
}

func (s Scope) kindMatchesIdentifier() error {
	switch s.Kind {
	case ScopeKindRepository:
		if s.RepositoryID == "" {
			return fmt.Errorf("scope kind %q requires repository_id", s.Kind)
		}
	case ScopeKindImageDigest:
		if s.SubjectDigest == "" {
			return fmt.Errorf("scope kind %q requires subject_digest", s.Kind)
		}
	case ScopeKindPackage:
		if s.PackageID == "" {
			return fmt.Errorf("scope kind %q requires package_id", s.Kind)
		}
	case ScopeKindAdvisory:
		if s.AdvisoryID == "" {
			return fmt.Errorf("scope kind %q requires advisory_id", s.Kind)
		}
	}
	return nil
}

// Target returns a stable "kind:identifier" label for telemetry and golden
// fixtures.
func (s Scope) Target() string {
	switch s.Kind {
	case ScopeKindRepository:
		return string(s.Kind) + ":" + s.RepositoryID
	case ScopeKindImageDigest:
		return string(s.Kind) + ":" + s.SubjectDigest
	case ScopeKindPackage:
		return string(s.Kind) + ":" + s.PackageID
	case ScopeKindAdvisory:
		return string(s.Kind) + ":" + s.AdvisoryID
	default:
		return string(s.Kind)
	}
}

func (k ScopeKind) validate() error {
	switch k {
	case ScopeKindRepository, ScopeKindImageDigest, ScopeKindPackage, ScopeKindAdvisory:
		return nil
	case "":
		return fmt.Errorf("scope kind must not be blank")
	default:
		return fmt.Errorf("unknown scope kind %q", k)
	}
}

// Severity is the normalized severity label exporters use when mapping into
// format-native severity vocabularies.
type Severity string

const (
	// SeverityCritical is the highest normalized severity.
	SeverityCritical Severity = "critical"
	// SeverityHigh is a normalized high severity.
	SeverityHigh Severity = "high"
	// SeverityMedium is a normalized medium severity.
	SeverityMedium Severity = "medium"
	// SeverityLow is a normalized low severity.
	SeverityLow Severity = "low"
	// SeverityNone is an informational or none-rated severity.
	SeverityNone Severity = "none"
	// SeverityUnknown means the source supplied no severity.
	SeverityUnknown Severity = "unknown"
)

// NormalizeSeverity returns the canonical [Severity] for a free-form label.
//
// Empty values map to [SeverityUnknown]. Unknown labels also map to
// [SeverityUnknown] so callers cannot smuggle arbitrary severity strings into
// format-native fields.
func NormalizeSeverity(raw string) Severity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium", "moderate":
		return SeverityMedium
	case "low":
		return SeverityLow
	case "none", "informational", "info":
		return SeverityNone
	case "":
		return SeverityUnknown
	default:
		return SeverityUnknown
	}
}

// Location is one manifest or source location attached to a finding.
//
// ManifestPath is the repo-relative or image-relative path the source system
// reported (for example `package.json`, `requirements.txt`, or a Helm chart
// values file). StartLine and EndLine are optional; zero means unknown.
type Location struct {
	ManifestPath string
	StartLine    int
	EndLine      int
}

// AdvisorySource is one source-attributed advisory observation behind a
// finding.
//
// Source is the canonical source name (for example `ghsa`, `nvd`,
// `osv-debian`). AdvisoryID is the advisory identifier as that source named
// it. URL is optional and only set when the source publishes a stable URL the
// exporter is allowed to forward.
type AdvisorySource struct {
	Source     string
	AdvisoryID string
	URL        string
}

// Finding is the format-neutral vulnerability finding input to every
// exporter.
//
// The field set is the intersection callers can reliably populate from
// reducer-owned facts today plus the fields SARIF, CycloneDX BOV, SPDX, and
// the GitHub dependency snapshot format need. Exporters tolerate empty
// optional fields and omit the corresponding format-native field rather than
// inventing values.
type Finding struct {
	FindingID           string
	CVEID               string
	AdvisoryID          string
	PackageID           string
	Ecosystem           string
	PackageName         string
	PURL                string
	ObservedVersion     string
	RequestedRange      string
	VulnerableRange     string
	FixedVersion        string
	MatchReason         string
	Summary             string
	Description         string
	Severity            Severity
	CVSSScore           float64
	CVSSVector          string
	KnownExploited      bool
	EPSSProbability     string
	RepositoryID        string
	SubjectDigest       string
	ImageRef            string
	RuntimeReachability string
	Reachability        *Reachability
	ImpactStatus        string
	Confidence          string
	WorkloadIDs         []string
	ServiceIDs          []string
	Environments        []string
	DependencyScope     string
	DependencyPath      []string
	DirectDependency    *bool
	MissingEvidence     []string
	EvidenceFactIDs     []string
	SourceFreshness     string
	Remediation         *Remediation
	Locations           []Location
	AdvisorySources     []AdvisorySource
	HelpURI             string
}

// Reachability is vulnerability reachability enrichment for one finding.
//
// It is intentionally separate from impact status and confidence. Missing or
// unavailable reachability evidence does not make a finding clean, and
// not_called only has stronger semantics when the ecosystem-specific scanner
// that produced it says so.
type Reachability struct {
	State            string
	Confidence       string
	Source           string
	Evidence         string
	Reason           string
	LanguageMaturity string
	MissingEvidence  []string
}

// Remediation is the safe-upgrade metadata attached to a vulnerability
// finding.
//
// The fields intentionally mirror reducer-owned remediation fields that are
// safe for export. Provider payload URLs and private alert bodies do not belong
// here; callers should drop them before constructing a Finding.
type Remediation struct {
	CurrentVersion      string
	VulnerableRange     string
	FixedVersionSource  string
	MatchReason         string
	FirstPatchedVersion string
	ManifestRange       string
	ManifestAllowsFix   string
	Confidence          string
	Reason              string
	Direct              *bool
	MissingEvidence     []string
}

// RuleID returns the canonical rule identifier for a finding.
//
// AdvisoryID wins over CVEID because reducers reconcile multi-source
// advisories under one durable advisory identity; CVEID is reused across
// advisory sources and is not unique enough to act as a rule key on its own.
func (f Finding) RuleID() string {
	if f.AdvisoryID != "" {
		return f.AdvisoryID
	}
	if f.CVEID != "" {
		return f.CVEID
	}
	return f.FindingID
}

// Component is the format-neutral SBOM component input shared by the SBOM
// formats (CycloneDX BOV, SPDX, GitHub dependency snapshot).
//
// The SARIF exporter does not consume Component values; the type is exported
// today so callers can assemble a single [Snapshot] once and pass it to any
// supported exporter.
type Component struct {
	BomRef        string
	Type          string
	Name          string
	Version       string
	PURL          string
	CPE           string
	Licenses      []string
	Supplier      string
	Hashes        []Hash
	Subject       string
	ManifestPath  string
	Relationships []string
}

// Hash is one component hash.
type Hash struct {
	Algorithm string
	Value     string
}

// Snapshot is the bounded, scoped input to any exporter.
//
// GeneratedAt is the moment the snapshot was assembled and is serialized into
// format-native timestamp fields. Findings and Components are the bounded
// evidence the caller has already authorized against the snapshot scope.
type Snapshot struct {
	Scope       Scope
	GeneratedAt time.Time
	Findings    []Finding
	Components  []Component
	Status      SnapshotStatus
}

// SnapshotStatus carries scanner/report readiness metadata for formats that
// can represent a run-level status alongside vulnerability findings.
type SnapshotStatus struct {
	ReportSchemaVersion string
	ReadinessState      string
	ReadinessFreshness  string
	ExitCode            int
	ExitReason          string
	ScopeMode           string
	MissingEvidence     []string
	IncompleteReasons   []string
	UnsupportedTargets  []UnsupportedTarget
}

// UnsupportedTarget describes target evidence the scanner observed but could
// not evaluate with the current matcher/export capability.
type UnsupportedTarget struct {
	TargetKind  string
	Reason      string
	Ecosystem   string
	PackageID   string
	PackageName string
	Count       int
}

// FieldRedactor rewrites caller-controlled path or locator strings before an
// exporter serializes them.
//
// Implementations must be deterministic and never return raw secret material.
// A nil redactor preserves paths verbatim and is appropriate only when the
// caller has already proven that all paths in the snapshot belong to the
// requested scope.
type FieldRedactor interface {
	RedactPath(path string) string
}

// Tool identifies the producing tool in format-native metadata blocks.
//
// Name and Version appear in SARIF tool.driver.name/version, CycloneDX
// metadata.tools, SPDX creator, and the GitHub dependency snapshot detector
// block. URI is optional and used when the format has an informational URI
// slot.
type Tool struct {
	Name    string
	Version string
	URI     string
}

// Options configures the exporter for one Snapshot rendering.
type Options struct {
	Tool     Tool
	Redactor FieldRedactor
}
