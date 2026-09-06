// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainmodel

import "github.com/eshu-hq/eshu/go/internal/facts"

// ImpactCVE is the reducer's internal projection of one vulnerability
// advisory fact (a CVE/GHSA/OSV record) used to classify supply-chain-impact
// findings.
type ImpactCVE struct {
	FactID          string
	CVEID           string
	AdvisoryID      string
	Source          string
	CVSSScore       float64
	CVSSVector      string
	SeverityLabel   string
	PublishedAt     string
	SourceUpdatedAt string
	WithdrawnAt     string
}

// AffectedPackage is the reducer's internal projection of one
// advisory-to-package affected-range record: the package identity, version
// ranges, and fixed-version evidence one advisory names for one ecosystem.
type AffectedPackage struct {
	FactID           string
	CVEID            string
	Source           string
	AdvisoryID       string
	PackageID        string
	Ecosystem        string
	Name             string
	PURL             string
	AffectedVersions []string
	AffectedRanges   []AffectedRange
	AffectedRangeRaw string
	FixedVersions    []string
}

// AffectedRange is one typed version-range event group inside an
// [AffectedPackage]'s affected-range evidence.
type AffectedRange struct {
	Kind   string
	Events []AffectedRangeEvent
}

// AffectedRangeEvent is one introduced/fixed/last-affected/limit boundary
// inside an [AffectedRange].
type AffectedRangeEvent struct {
	Introduced   string
	Fixed        string
	LastAffected string
	Limit        string
}

// AffectedProduct is the reducer's internal projection of one CPE-based
// affected-product match record for an advisory.
type AffectedProduct struct {
	FactID          string
	CVEID           string
	Criteria        string
	MatchCriteriaID string
	Vulnerable      bool
}

// PackageConsumption is the reducer's internal projection of one
// package-consumption correlation record: the manifest/lockfile evidence that
// anchors a repository's observed version, dependency path, and dependency
// scope for a package.
type PackageConsumption struct {
	FactID                    string
	EvidenceKind              string
	PackageID                 string
	RepositoryID              string
	DependencyRange           string
	ObservedVersion           string
	RequestedRange            string
	InstalledVersion          string
	DependencyPath            []string
	DependencyDepth           int
	DirectDependency          *bool
	DependencyScope           string
	VersionEvidence           string
	UnresolvedMSBuildProperty string
	AmbiguousMSBuildProperty  string
	PackageAPIPackages        []string
	PackageAPIIdentitySource  string
	DependencyResolutionState string
	SourceSet                 string
	GeneratedCode             *bool
	PartialEvidence           bool
	Lockfile                  bool
}

// SBOMComponent is the reducer's internal projection of one SBOM component
// record: the package identity an SBOM document names for a container image.
type SBOMComponent struct {
	FactID     string
	DocumentID string
	PURL       string
	CPE        string
	PackageID  string
	Version    string
}

// OSPackage is the reducer's internal projection of one OS-package
// vulnerability-scan record: the distro package identity and installed
// version an image scanner observed for one scan target (ScopeID +
// GenerationID).
type OSPackage struct {
	FactID               string
	ScopeID              string
	GenerationID         string
	PackageID            string
	PURL                 string
	Distro               string
	DistroVersion        string
	PackageManager       string
	Name                 string
	Arch                 string
	InstalledVersion     string
	RepositoryClass      string
	VendorAdvisorySource string
}

// ScannerAnalysis is the reducer's internal projection of one
// scanner_worker.analysis envelope: the sibling fact that carries the real,
// content-addressed image digest/reference for the image a scanner_worker
// analyzer (including the OS-package analyzer) inspected. OSPackage facts
// only carry an opaque ScopeID (a scan-target locator, never a sha256), so the
// reducer joins an OSPackage to its sibling analysis by ScopeID+GenerationID
// ([ScopeGenerationKey]) to anchor the finding's subject digest on a real
// image digest instead of the scope ID.
type ScannerAnalysis struct {
	FactID         string
	ScopeID        string
	GenerationID   string
	ImageDigest    string
	ImageReference string
}

// ScopeGenerationKey returns the composite key (ScopeID+GenerationID) that
// [ScannerAnalysis] evidence is indexed by, and the same key an OSPackage's
// sibling scanner_worker.analysis is looked up by. It defers to
// facts.Envelope's own ScopeGenerationKey formatting so this join key stays
// byte-identical to the durable scope-generation boundary the rest of the
// platform uses, rather than re-deriving an equivalent format locally.
func ScopeGenerationKey(scopeID, generationID string) string {
	return facts.Envelope{ScopeID: scopeID, GenerationID: generationID}.ScopeGenerationKey()
}

// Attachment is the reducer's internal projection of one SBOM-attestation
// attachment record: the subject digest and verification status an
// attestation names for a document.
type Attachment struct {
	FactID        string
	DocumentID    string
	SubjectDigest string
	Status        string
}

// DeploymentContext is the reducer's internal projection of one
// deploy-event/deployment-declaration record used to anchor a
// supply-chain-impact finding to a runtime environment.
type DeploymentContext struct {
	FactID         string
	ArtifactDigest string
	ImageRef       string
	RepositoryID   string
	Environment    string
	// EnvironmentEvidence is the #5425 corroboration state normalized by the
	// reducer's environment-evidence normalizer: "deploy_event" or
	// "declared".
	EnvironmentEvidence string
	Outcome             string
	ProvenanceOnly      bool
}

// DeploymentLaneContext is the reducer's internal projection of one
// repository's deployment-lane membership: the deployment IDs a repository
// participates in.
type DeploymentLaneContext struct {
	FactID        string
	RepositoryID  string
	DeploymentIDs []string
}

// WorkloadContext is the reducer's internal projection of one
// repository-to-workload correlation used to anchor a supply-chain-impact
// finding to a runtime workload.
type WorkloadContext struct {
	FactID       string
	RepositoryID string
	WorkloadID   string
}

// ServiceContext is the reducer's internal projection of one
// repository-to-service-catalog correlation used to anchor a
// supply-chain-impact finding to a cataloged service.
type ServiceContext struct {
	FactID         string
	RepositoryID   string
	ServiceID      string
	WorkloadID     string
	EntityRef      string
	OwnerRef       string
	Outcome        string
	DriftStatus    string
	ProvenanceOnly bool
}

// RiskSignals is the reducer's internal projection of one advisory's
// exploit-prediction (EPSS) and known-exploited-vulnerability (KEV) evidence.
type RiskSignals struct {
	EPSSFactID      string
	EPSSProbability string
	EPSSPercentile  string
	KEVFactID       string
	KnownExploited  bool
}
