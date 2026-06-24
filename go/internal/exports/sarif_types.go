// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports

// SARIF v2.1.0 wire types.
//
// These are intentionally narrow: only the SARIF fields the Eshu writer
// actually emits today are modeled. Add a field here when you start
// populating it in sarif.go and remove it when it leaves the output. The
// `omitempty` strategy keeps the JSON shape stable; adding a new field with
// a zero value will not move existing golden fixtures.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations,omitempty"`
	Results     []sarifResult     `json:"results"`
	Properties  *sarifProperties  `json:"properties,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID                   string              `json:"id"`
	Name                 string              `json:"name,omitempty"`
	ShortDescription     *sarifMessage       `json:"shortDescription,omitempty"`
	FullDescription      *sarifMessage       `json:"fullDescription,omitempty"`
	HelpURI              string              `json:"helpUri,omitempty"`
	DefaultConfiguration *sarifConfiguration `json:"defaultConfiguration,omitempty"`
	Properties           *sarifRuleProps     `json:"properties,omitempty"`
}

type sarifConfiguration struct {
	Level string `json:"level,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level,omitempty"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          *sarifResultProps `json:"properties,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
}

type sarifInvocation struct {
	ExecutionSuccessful bool   `json:"executionSuccessful"`
	StartTimeUTC        string `json:"startTimeUtc,omitempty"`
	EndTimeUTC          string `json:"endTimeUtc,omitempty"`
}

// sarifProperties carries Eshu-vendor properties on a SARIF run. Every key
// is prefixed with `eshu.` so it does not collide with SARIF reserved
// property names.
type sarifProperties struct {
	Scope               string                     `json:"eshu.scope"`
	GeneratedAt         string                     `json:"eshu.generatedAt,omitempty"`
	TruncatedScope      bool                       `json:"eshu.truncatedScope,omitempty"`
	DroppedFindings     int                        `json:"eshu.droppedFindings,omitempty"`
	RedactedPaths       int                        `json:"eshu.redactedPaths,omitempty"`
	FormatVersion       string                     `json:"eshu.formatVersion"`
	SnapshotEvidence    string                     `json:"eshu.snapshotEvidence,omitempty"`
	ReportSchemaVersion string                     `json:"eshu.reportSchemaVersion,omitempty"`
	ReadinessState      string                     `json:"eshu.readinessState,omitempty"`
	ReadinessFreshness  string                     `json:"eshu.readinessFreshness,omitempty"`
	ExitCode            int                        `json:"eshu.exitCode,omitempty"`
	ExitReason          string                     `json:"eshu.exitReason,omitempty"`
	ScopeMode           string                     `json:"eshu.scopeMode,omitempty"`
	MissingEvidence     []string                   `json:"eshu.missingEvidence,omitempty"`
	IncompleteReasons   []string                   `json:"eshu.incompleteReasons,omitempty"`
	UnsupportedTargets  []sarifUnsupportedTargetJS `json:"eshu.unsupportedTargets,omitempty"`
}

// sarifRuleProps carries vendor-prefixed advisory metadata on a SARIF rule.
type sarifRuleProps struct {
	Severity        string                  `json:"eshu.severity,omitempty"`
	CVSSScore       float64                 `json:"eshu.cvssScore,omitempty"`
	CVSSVector      string                  `json:"eshu.cvssVector,omitempty"`
	EPSSProbability string                  `json:"eshu.epssProbability,omitempty"`
	KnownExploited  bool                    `json:"eshu.knownExploited,omitempty"`
	Ecosystem       string                  `json:"eshu.ecosystem,omitempty"`
	PURL            string                  `json:"eshu.purl,omitempty"`
	AdvisorySources []sarifAdvisorySourceJS `json:"eshu.advisorySources,omitempty"`
	Tags            []string                `json:"tags,omitempty"`
}

// sarifAdvisorySourceJS is the wire shape for one [AdvisorySource]. Field
// names are identical to [AdvisorySource] so [AdvisorySource] is convertible
// to this type without copying field-by-field.
type sarifAdvisorySourceJS struct {
	Source     string `json:"source"`
	AdvisoryID string `json:"advisoryId,omitempty"`
	URL        string `json:"url,omitempty"`
}

// sarifResultProps carries vendor-prefixed evidence metadata on a SARIF
// result.
type sarifResultProps struct {
	FindingID                    string                  `json:"eshu.findingId,omitempty"`
	PackageID                    string                  `json:"eshu.packageId,omitempty"`
	PackageName                  string                  `json:"eshu.packageName,omitempty"`
	ObservedVersion              string                  `json:"eshu.observedVersion,omitempty"`
	FixedVersion                 string                  `json:"eshu.fixedVersion,omitempty"`
	RepositoryID                 string                  `json:"eshu.repositoryId,omitempty"`
	SubjectDigest                string                  `json:"eshu.subjectDigest,omitempty"`
	ImageRef                     string                  `json:"eshu.imageRef,omitempty"`
	RuntimeReachability          string                  `json:"eshu.runtimeReachability,omitempty"`
	ReachabilityState            string                  `json:"eshu.reachabilityState,omitempty"`
	ReachabilityConfidence       string                  `json:"eshu.reachabilityConfidence,omitempty"`
	ReachabilitySource           string                  `json:"eshu.reachabilitySource,omitempty"`
	ReachabilityEvidence         string                  `json:"eshu.reachabilityEvidence,omitempty"`
	ReachabilityReason           string                  `json:"eshu.reachabilityReason,omitempty"`
	ReachabilityLanguageMaturity string                  `json:"eshu.reachabilityLanguageMaturity,omitempty"`
	ReachabilityMissingEvidence  []string                `json:"eshu.reachabilityMissingEvidence,omitempty"`
	ImpactStatus                 string                  `json:"eshu.impactStatus,omitempty"`
	Confidence                   string                  `json:"eshu.confidence,omitempty"`
	RequestedRange               string                  `json:"eshu.requestedRange,omitempty"`
	VulnerableRange              string                  `json:"eshu.vulnerableRange,omitempty"`
	MatchReason                  string                  `json:"eshu.matchReason,omitempty"`
	WorkloadIDs                  []string                `json:"eshu.workloadIds,omitempty"`
	ServiceIDs                   []string                `json:"eshu.serviceIds,omitempty"`
	Environments                 []string                `json:"eshu.environments,omitempty"`
	DependencyScope              string                  `json:"eshu.dependencyScope,omitempty"`
	DependencyPath               []string                `json:"eshu.dependencyPath,omitempty"`
	DirectDependency             *bool                   `json:"eshu.directDependency,omitempty"`
	MissingEvidence              []string                `json:"eshu.missingEvidence,omitempty"`
	EvidenceFactIDs              []string                `json:"eshu.evidenceFactIds,omitempty"`
	SourceFreshness              string                  `json:"eshu.sourceFreshness,omitempty"`
	Remediation                  *sarifRemediationProps  `json:"eshu.remediation,omitempty"`
	ScannerStatus                *sarifScannerStatusProp `json:"eshu.scannerStatus,omitempty"`
}

type sarifRemediationProps struct {
	CurrentVersion      string   `json:"currentVersion,omitempty"`
	VulnerableRange     string   `json:"vulnerableRange,omitempty"`
	FixedVersionSource  string   `json:"fixedVersionSource,omitempty"`
	MatchReason         string   `json:"matchReason,omitempty"`
	FirstPatchedVersion string   `json:"firstPatchedVersion,omitempty"`
	ManifestRange       string   `json:"manifestRange,omitempty"`
	ManifestAllowsFix   string   `json:"manifestAllowsFix,omitempty"`
	Confidence          string   `json:"confidence,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	Direct              *bool    `json:"direct,omitempty"`
	MissingEvidence     []string `json:"missingEvidence,omitempty"`
}

type sarifScannerStatusProp struct {
	ReadinessState    string   `json:"readinessState,omitempty"`
	MissingEvidence   []string `json:"missingEvidence,omitempty"`
	IncompleteReasons []string `json:"incompleteReasons,omitempty"`
}

type sarifUnsupportedTargetJS struct {
	TargetKind  string `json:"targetKind,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Ecosystem   string `json:"ecosystem,omitempty"`
	PackageID   string `json:"packageId,omitempty"`
	PackageName string `json:"packageName,omitempty"`
	Count       int    `json:"count,omitempty"`
}
