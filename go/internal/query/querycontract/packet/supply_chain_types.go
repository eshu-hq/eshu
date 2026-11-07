package packet //nolint:filelength // 811-line JSON wire-shape vocabulary for the supply-chain explanation API (advisory, explanation, readiness, runtime context); splitting would scatter one golden-locked wire contract that moved as a single unit.

import "strings"

// Supply-chain impact read vocabulary, moved from
// go/internal/query/supply/chain/impact so the packet composers and eshu-cli
// read them without importing the handler package. These are the JSON wire
// shapes of the impact explanation API: moving them changes no tag, no
// value, and no method. The store, decode, and readiness-classifier logic
// stays in the impact package and now spells these names with the packet
// qualifier.

// ExplanationFilter is the bounded input accepted by the
// vulnerability impact explanation route.
type ExplanationFilter struct {
	FindingID     string `json:"finding_id,omitempty"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	CVEID         string `json:"cve_id,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
	ImageRef      string `json:"image_ref,omitempty"`
	WorkloadID    string `json:"workload_id,omitempty"`
	ServiceID     string `json:"service_id,omitempty"`
	// AllowedRepositoryIDs and AllowedScopeIDs carry scoped-token repository
	// and ingestion-scope grants (#5167 W5). When both are empty the read is
	// unrestricted (shared token, all-scope admin, or local dev mode). When
	// either is populated the query intersects the matched finding's
	// repository_id/scope_id with the granted set before a finding is ever
	// returned, so a scoped caller can never explain an out-of-grant finding
	// even when the request anchors on a non-repository selector such as
	// finding_id, CVE, advisory, package, image, service, workload, or
	// subject digest. These are excluded from JSON marshaling (Input echoes
	// filter back to the caller) since they are an authorization concern, not
	// caller-supplied input.
	AllowedRepositoryIDs []string `json:"-"`
	AllowedScopeIDs      []string `json:"-"`
}

// ExplanationResult is the public API and MCP data payload for
// explaining one vulnerability finding or a bounded no-evidence scope.
type ExplanationResult struct {
	Outcome              string                `json:"outcome"`
	EvidencePacketHandle string                `json:"evidence_packet_handle,omitempty"`
	Input                ExplanationFilter     `json:"input"`
	Finding              *FindingResult        `json:"finding,omitempty"`
	Advisory             AdvisoryExplanation   `json:"advisory"`
	Component            ComponentExplanation  `json:"component"`
	Version              VersionExplanation    `json:"version"`
	DependencyChain      *DependencyChain      `json:"dependency_chain,omitempty"`
	Anchors              ExplanationAnchors    `json:"anchors"`
	Path                 []PathHop             `json:"impact_path,omitempty"`
	Evidence             []EvidenceFactSummary `json:"evidence"`
	Readiness            ReadinessEnvelope     `json:"readiness"`
	MissingEvidence      []string              `json:"missing_evidence,omitempty"`
	Freshness            ExplanationFreshness  `json:"freshness"`
	// Remediation is the reducer-owned advisory-only safe-upgrade
	// recommendation enriched with vulnerable-range evidence from the
	// referenced advisory facts (issue #595). Nil when the finding is too
	// old to carry remediation metadata.
	Remediation *Remediation `json:"remediation,omitempty"`
}

// AdvisoryExplanation summarizes advisory evidence selected
// for a finding and the source-reported vulnerable range when available.
type AdvisoryExplanation struct {
	CVEID                      string           `json:"cve_id,omitempty"`
	AdvisoryID                 string           `json:"advisory_id,omitempty"`
	VulnerableRange            string           `json:"vulnerable_range,omitempty"`
	RangeSource                string           `json:"range_source,omitempty"`
	SelectedSeveritySource     string           `json:"selected_severity_source,omitempty"`
	SelectedFixedVersionSource string           `json:"selected_fixed_version_source,omitempty"`
	Sources                    []AdvisorySource `json:"sources,omitempty"`
	References                 []string         `json:"references,omitempty"`
}

// ComponentExplanation identifies the package or component
// version matched by reducer-owned impact evidence.
type ComponentExplanation struct {
	PackageID       string `json:"package_id,omitempty"`
	Ecosystem       string `json:"ecosystem,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	PURL            string `json:"purl,omitempty"`
	ProductCriteria string `json:"product_criteria,omitempty"`
	MatchCriteriaID string `json:"match_criteria_id,omitempty"`
	ObservedVersion string `json:"observed_version,omitempty"`
	ManifestRange   string `json:"manifest_range,omitempty"`
}

// VersionExplanation keeps version observations separate from
// advisory range and remediation metadata.
type VersionExplanation struct {
	ObservedVersion string `json:"observed_version,omitempty"`
	ManifestRange   string `json:"manifest_range,omitempty"`
	VulnerableRange string `json:"vulnerable_range,omitempty"`
	FixedVersion    string `json:"fixed_version,omitempty"`
	VersionEvidence string `json:"version_evidence"`
}

// DependencyChain explains direct versus transitive package
// evidence when a manifest, lockfile, or SBOM source provided it.
type DependencyChain struct {
	Path             []string `json:"path,omitempty"`
	Depth            int      `json:"depth,omitempty"`
	DirectDependency *bool    `json:"direct_dependency,omitempty"`
}

// ExplanationAnchors lists scoped evidence anchors. Empty
// anchor families remain omitted rather than inferred from names or tags.
type ExplanationAnchors struct {
	RepositoryID    string                `json:"repository_id,omitempty"`
	SubjectDigest   string                `json:"subject_digest,omitempty"`
	ManifestPaths   []string              `json:"manifest_paths,omitempty"`
	LockfilePaths   []string              `json:"lockfile_paths,omitempty"`
	SBOMDocuments   []string              `json:"sbom_documents,omitempty"`
	ImageDigests    []string              `json:"image_digests,omitempty"`
	ImageRefs       []string              `json:"image_refs,omitempty"`
	Workloads       []string              `json:"workloads,omitempty"`
	Deployments     []string              `json:"deployments,omitempty"`
	Services        []string              `json:"services,omitempty"`
	Environments    []string              `json:"environments,omitempty"`
	CatalogEntities []string              `json:"catalog_entities,omitempty"`
	CatalogOwners   []string              `json:"catalog_owners,omitempty"`
	ProviderAlerts  []ProviderAlertAnchor `json:"provider_alerts,omitempty"`
	EvidenceFactIDs []string              `json:"evidence_fact_ids,omitempty"`
}

// PathHop reports one present or missing hop in the
// reducer-owned vulnerability impact path.
type PathHop struct {
	Hop             string   `json:"hop"`
	Status          string   `json:"status"`
	EvidenceFactIDs []string `json:"evidence_fact_ids,omitempty"`
	MissingEvidence []string `json:"missing_evidence,omitempty"`
}

// ProviderAlertAnchor preserves provider alert evidence without
// promoting it into owned package, image, workload, or deployment truth.
type ProviderAlertAnchor struct {
	Provider     string `json:"provider,omitempty"`
	AlertID      string `json:"alert_id,omitempty"`
	State        string `json:"state,omitempty"`
	ManifestPath string `json:"manifest_path,omitempty"`
}

// EvidenceFactSummary is a compact source/reducer fact preview
// for one evidence id referenced by the finding.
type EvidenceFactSummary struct {
	FactID           string `json:"fact_id"`
	FactKind         string `json:"fact_kind"`
	SourceSystem     string `json:"source_system,omitempty"`
	SourceConfidence string `json:"source_confidence,omitempty"`
	ObservedAt       string `json:"observed_at,omitempty"`
}

// ExplanationFreshness exposes evidence observation freshness
// for the explanation payload itself.
type ExplanationFreshness struct {
	State             string `json:"state"`
	LatestObservedAt  string `json:"latest_observed_at,omitempty"`
	EvidenceFactCount int    `json:"evidence_fact_count"`
}

func (f ExplanationFilter) HasBoundedScope() bool {
	if strings.TrimSpace(f.FindingID) != "" {
		return true
	}
	if strings.TrimSpace(f.CVEID) != "" {
		return f.hasTargetScope()
	}
	if strings.TrimSpace(f.AdvisoryID) == "" {
		return false
	}
	return f.hasTargetScope()
}

func (f ExplanationFilter) hasTargetScope() bool {
	return strings.TrimSpace(f.PackageID) != "" ||
		strings.TrimSpace(f.RepositoryID) != "" ||
		strings.TrimSpace(f.SubjectDigest) != "" ||
		strings.TrimSpace(f.ImageRef) != "" ||
		strings.TrimSpace(f.WorkloadID) != "" ||
		strings.TrimSpace(f.ServiceID) != ""
}

func (f ExplanationFilter) ReadinessScope() TargetScope {
	return TargetScope{
		CVEID:         f.CVEID,
		AdvisoryID:    f.AdvisoryID,
		PackageID:     f.PackageID,
		RepositoryID:  f.RepositoryID,
		SubjectDigest: f.SubjectDigest,
		ImageRef:      f.ImageRef,
		WorkloadID:    f.WorkloadID,
		ServiceID:     f.ServiceID,
	}
}

// FindingResult is one reducer-owned vulnerability impact row
// returned by the public API.
//
// Priority fields are reducer-owned triage metadata. They explain urgency but
// never change impact_status, missing_evidence, or readiness truth.
const (
	// ServiceCatalogCorrelationMissingReason is the reason
	// BuildFindingResult records when no service-catalog correlation
	// evidence was found at all for a finding.
	ServiceCatalogCorrelationMissingReason = "service catalog correlation evidence missing"
	// ServiceCatalogAnchorMissingReason is the reason BuildFindingResult
	// records when correlation evidence exists but does not resolve to a
	// known service/workload catalog anchor.
	ServiceCatalogAnchorMissingReason = "service/workload catalog anchor missing"
)

type FindingResult struct {
	FindingID             string                 `json:"finding_id"`
	CVEID                 string                 `json:"cve_id,omitempty"`
	AdvisoryID            string                 `json:"advisory_id,omitempty"`
	PackageID             string                 `json:"package_id,omitempty"`
	Ecosystem             string                 `json:"ecosystem,omitempty"`
	PackageName           string                 `json:"package_name,omitempty"`
	PURL                  string                 `json:"purl,omitempty"`
	ProductCriteria       string                 `json:"product_criteria,omitempty"`
	MatchCriteriaID       string                 `json:"match_criteria_id,omitempty"`
	ObservedVersion       string                 `json:"observed_version,omitempty"`
	RequestedRange        string                 `json:"requested_range,omitempty"`
	FixedVersion          string                 `json:"fixed_version,omitempty"`
	VulnerableRange       string                 `json:"vulnerable_range,omitempty"`
	MatchReason           string                 `json:"match_reason,omitempty"`
	Status                string                 `json:"impact_status"`
	Confidence            string                 `json:"confidence,omitempty"`
	CVSSScore             float64                `json:"cvss_score,omitempty"`
	AdvisoryPublishedAt   string                 `json:"advisory_published_at,omitempty"`
	AdvisoryUpdatedAt     string                 `json:"advisory_updated_at,omitempty"`
	EPSSProbability       string                 `json:"epss_probability,omitempty"`
	EPSSPercentile        string                 `json:"epss_percentile,omitempty"`
	KnownExploited        bool                   `json:"known_exploited"`
	PriorityReason        string                 `json:"priority_reason,omitempty"`
	PriorityScore         int                    `json:"priority_score,omitempty"`
	PriorityBucket        string                 `json:"priority_bucket,omitempty"`
	PriorityReasonCodes   []string               `json:"priority_reason_codes,omitempty"`
	PriorityContributions []PriorityContribution `json:"priority_contributions,omitempty"`
	// ExploitedTier is the additive #9882 item-4.5 exploited rank (kev,
	// ssvc_active, epss, none). Omitted for rows written before the tier
	// existed. Like the score and bucket, it is triage metadata and never
	// changes impact_status, missing_evidence, or readiness truth.
	ExploitedTier       string              `json:"exploited_tier,omitempty"`
	RuntimeReachability string              `json:"runtime_reachability,omitempty"`
	Reachability        *ReachabilityResult `json:"reachability,omitempty"`
	RepositoryID        string              `json:"repository_id,omitempty"`
	SubjectDigest       string              `json:"subject_digest,omitempty"`
	ImageRef            string              `json:"image_ref,omitempty"`
	DependencyScope     string              `json:"dependency_scope,omitempty"`
	WorkloadIDs         []string            `json:"workload_ids,omitempty"`
	DeploymentIDs       []string            `json:"deployment_ids,omitempty"`
	ServiceIDs          []string            `json:"service_ids,omitempty"`
	Environments        []string            `json:"environments,omitempty"`
	// EnvironmentEvidence records, per environment name in Environments,
	// whether the strongest deployment evidence for that environment was
	// "deploy_event" or "declared" (issue #5426). Omitted when empty.
	EnvironmentEvidence map[string]string `json:"environment_evidence,omitempty"`
	// CIDeclaredArtifactDigest and CIDeclaredImageRef carry the matched
	// cicd_run_correlation deployment's OWN declared artifact identity
	// (issue #5469), persisted only when that deployment matched through a
	// strong artifact-identity branch (digest or image-ref equality), never
	// the weak repository+environment+operational-anchor branch. This is
	// the evidence version_resolution_corroboration's provenance_ci_declared
	// entry (when present) discloses as digest_or_version. Omitted when no
	// strong-branch CI/CD deployment evidence exists.
	CIDeclaredArtifactDigest string `json:"ci_declared_artifact_digest,omitempty"`
	CIDeclaredImageRef       string `json:"ci_declared_image_ref,omitempty"`
	// CloudRuntimeResourceRefs names the observed cloud compute resources
	// (running ECS task / image-package Lambda ARNs) whose running image digest
	// matches this finding's subject digest — runtime-observed deployment
	// evidence that drives the runtime_confirmed deployment_truth_tier, distinct
	// from the CI-declared deployment anchors (#5452).
	CloudRuntimeResourceRefs []string `json:"cloud_runtime_resource_refs,omitempty"`
	// KubernetesRuntimeWorkloadRefs carries exact digest-bound current workload
	// evidence. It is not the repository-level runtime_context workload list.
	KubernetesRuntimeWorkloadRefs []KubernetesRuntimeWorkloadRef `json:"kubernetes_runtime_workload_refs,omitempty"`
	// KubernetesRuntimeProbe reports the per-digest candidate cap and bounded
	// completeness state for the Kubernetes runtime graph probe.
	KubernetesRuntimeProbe *KubernetesRuntimeProbeMetadata `json:"kubernetes_runtime_probe,omitempty"`
	// RuntimeContext carries the read-time-resolved runtime context
	// (workloads, services, deployments, environments, catalog refs) resolved
	// from this finding's repository_id at query time (issue #5746). It is
	// labeled with truth_basis "read_time_resolved" so callers cannot mistake
	// it for baked workload_ids/service_ids/environments. The matching filters
	// resolve the same current repository mappings independently (#5747)
	// without mutating this reducer-owned payload.
	RuntimeContext    *RuntimeContextResult `json:"runtime_context,omitempty"`
	CatalogEntityRefs []string              `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs  []string              `json:"catalog_owner_refs,omitempty"`
	DependencyPath    []string              `json:"dependency_path,omitempty"`
	DependencyDepth   int                   `json:"dependency_depth,omitempty"`
	DirectDependency  *bool                 `json:"direct_dependency,omitempty"`
	MissingEvidence   []string              `json:"missing_evidence,omitempty"`
	EvidencePath      []string              `json:"evidence_path,omitempty"`
	EvidenceFactIDs   []string              `json:"evidence_fact_ids,omitempty"`
	SourceFreshness   string                `json:"source_freshness,omitempty"`
	SourceConfidence  string                `json:"source_confidence,omitempty"`
	Provenance        *Provenance           `json:"provenance,omitempty"`
	// Suppression carries the reducer VEX/operator-policy decision attached
	// to this finding. The reducer always populates a decision (state=active
	// when nothing matched) so callers can audit suppression provenance even
	// when the finding is hidden from the default view.
	Suppression *SuppressionDecisionRow `json:"suppression,omitempty"`
	// DetectionProfile names whether the row meets the precise exact-version
	// bar or only the broader comprehensive owned-anchor profile.
	DetectionProfile string `json:"detection_profile,omitempty"`
	// Remediation is the reducer-owned advisory-only safe-upgrade
	// recommendation for this finding (issue #595). Older rows that predate
	// remediation computation omit this block.
	Remediation *Remediation `json:"remediation,omitempty"`
	// DeploymentTruthTier classifies the strongest deployment evidence
	// available for this finding's repository or workload context, using the
	// shared truth.DeploymentTruthTier vocabulary (#5471). Omitted when the
	// finding has no deployment anchor at all.
	DeploymentTruthTier string `json:"deployment_truth_tier,omitempty"`
	// VersionResolutionTier discloses which deployment-truth tier the
	// judged version/digest for this finding was resolved from (#5469),
	// reusing the shared truth.DeploymentTruthTier vocabulary verbatim. It
	// can differ from DeploymentTruthTier: DeploymentTruthTier reports the
	// strongest tier with ANY deployment evidence, while
	// VersionResolutionTier requires that tier's evidence to also carry a
	// concrete version/digest claim. Omitted when the finding has no
	// version/digest evidence at all.
	VersionResolutionTier string `json:"version_resolution_tier,omitempty"`
	// VersionResolutionCorroboration lists every other deployment-truth tier
	// that also makes a version/digest claim for this finding -- including a
	// tier whose claim was ineligible to win (for example a CI-declared
	// digest that contradicts the finding's own subject_digest, review
	// finding R1) or whose claim disagrees with VersionResolutionTier's
	// judged value. Each entry's agreement field is a closed three-state
	// vocabulary (agrees/disagrees/not_comparable): a cross-axis comparison,
	// such as a config-materialized version against a digest-based winner,
	// is reported not_comparable rather than a misleading "disagrees"
	// (review finding R6). Omitted when no other tier makes a claim.
	VersionResolutionCorroboration []VersionResolutionCorroboration `json:"version_resolution_corroboration,omitempty"`
}

// PriorityContribution explains one reducer priority input.
type PriorityContribution struct {
	ReasonCode   string `json:"reason_code"`
	Input        string `json:"input"`
	Value        string `json:"value,omitempty"`
	Contribution int    `json:"contribution"`
}

// ReachabilityResult is the stable reachability enrichment envelope
// attached to one vulnerability finding. It is separate from impact_status and
// confidence so callers cannot treat reachability absence as a clean result.
type ReachabilityResult struct {
	State            string   `json:"state"`
	Confidence       string   `json:"confidence,omitempty"`
	Source           string   `json:"source,omitempty"`
	Evidence         string   `json:"evidence,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	LanguageMaturity string   `json:"language_maturity,omitempty"`
	MissingEvidence  []string `json:"missing_evidence,omitempty"`
}

// SuppressionDecisionRow is the API-shaped suppression decision
// attached to one impact finding. The reducer produces an "active" row when
// no suppression matched.
type SuppressionDecisionRow struct {
	State          string `json:"state"`
	SuppressionID  string `json:"suppression_id,omitempty"`
	Source         string `json:"source,omitempty"`
	Justification  string `json:"justification,omitempty"`
	Author         string `json:"author,omitempty"`
	AuthoredAt     string `json:"authored_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	Reason         string `json:"reason,omitempty"`
	EvidenceRef    string `json:"evidence_ref,omitempty"`
	VEXDocumentID  string `json:"vex_document_id,omitempty"`
	VEXStatementID string `json:"vex_statement_id,omitempty"`
}

// Provenance preserves per-source advisory provenance for one
// supply-chain impact finding. Reducers select one severity and one
// fixed-version using documented ecosystem-aware priority while keeping every
// other source observation as an alternate so callers can explain selection
// and detect vendor/upstream disagreement.
type Provenance struct {
	SelectedSeveritySource string  `json:"selected_severity_source,omitempty"`
	SelectedSeverityScore  float64 `json:"selected_severity_score,omitempty"`
	// SelectedSeverityScoreSource is "derived_from_vector" when the reducer
	// computed SelectedSeverityScore from SelectedSeverityVector, and absent
	// when the source reported the score (#9693).
	SelectedSeverityScoreSource string               `json:"selected_severity_score_source,omitempty"`
	SelectedSeverityVector      string               `json:"selected_severity_vector,omitempty"`
	SelectedSeverityLabel       string               `json:"selected_severity_label,omitempty"`
	SelectedFixedVersionSource  string               `json:"selected_fixed_version_source,omitempty"`
	SelectedRangeSource         string               `json:"selected_range_source,omitempty"`
	SelectedEPSSScoreDate       string               `json:"selected_epss_score_date,omitempty"`
	SelectedEPSSModelVersion    string               `json:"selected_epss_model_version,omitempty"`
	AlternateSeverities         []AlternateSeverity  `json:"alternate_severities,omitempty"`
	FixedVersionBranches        []FixedVersionBranch `json:"fixed_version_branches,omitempty"`
	AdvisorySources             []AdvisorySource     `json:"advisory_sources,omitempty"`
	// AdvisoryRetraction explains an advisory_retracted finding (#9695).
	AdvisoryRetraction *AdvisoryRetraction `json:"advisory_retraction,omitempty"`
}

// IsEmpty reports whether the provenance carries no selection at all.
// It lives on the type (not with the payload decoders) so the finding
// decoders can test emptiness without importing the handler package.
func (p Provenance) IsEmpty() bool {
	return p.SelectedSeveritySource == "" &&
		p.SelectedSeverityScore == 0 &&
		p.SelectedSeverityScoreSource == "" &&
		p.SelectedSeverityVector == "" &&
		p.SelectedSeverityLabel == "" &&
		p.SelectedFixedVersionSource == "" &&
		p.SelectedRangeSource == "" &&
		p.SelectedEPSSScoreDate == "" &&
		p.SelectedEPSSModelVersion == "" &&
		len(p.AlternateSeverities) == 0 &&
		len(p.FixedVersionBranches) == 0 &&
		len(p.AdvisorySources) == 0 &&
		p.AdvisoryRetraction == nil
}

// AlternateSeverity is one non-selected source severity preserved
// for explainability.
type AlternateSeverity struct {
	Source string  `json:"source"`
	Score  float64 `json:"score,omitempty"`
	// ScoreSource is "derived_from_vector" when Score was computed from Vector,
	// and absent when the source reported it.
	ScoreSource string `json:"score_source,omitempty"`
	Vector      string `json:"vector,omitempty"`
	Label       string `json:"label,omitempty"`
}

// FixedVersionBranch is one source-attributed fixed-version branch
// for one impact finding.
type FixedVersionBranch struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

// AdvisorySource is one source-attributed advisory observation
// behind a finding, including when the source last updated it and when it
// was withdrawn.
type AdvisorySource struct {
	Source          string `json:"source"`
	AdvisoryID      string `json:"advisory_id,omitempty"`
	SourceUpdatedAt string `json:"source_updated_at,omitempty"`
	WithdrawnAt     string `json:"withdrawn_at,omitempty"`
	// Status and ReplacedBy are what the source reported (#9695).
	Status     string   `json:"status,omitempty"`
	ReplacedBy []string `json:"replaced_by,omitempty"`
}

// AdvisoryRetraction is the explanation the reducer attaches to a finding whose
// every source record rejected or withdrew its advisory (#9695). ReplacedBy
// repeats only what a source stated; Eshu never infers a replacement.
type AdvisoryRetraction struct {
	ReasonCode string             `json:"reason_code"`
	Reason     string             `json:"reason"`
	ReplacedBy []string           `json:"replaced_by,omitempty"`
	Sources    []RetractionSource `json:"sources,omitempty"`
}

// RetractionSource is one source record that retracted its advisory.
type RetractionSource struct {
	Source      string   `json:"source"`
	AdvisoryID  string   `json:"advisory_id,omitempty"`
	ReasonCode  string   `json:"reason_code"`
	Status      string   `json:"status,omitempty"`
	WithdrawnAt string   `json:"withdrawn_at,omitempty"`
	ReplacedBy  []string `json:"replaced_by,omitempty"`
}

// Remediation is the advisory-only safe-upgrade recommendation
// the reducer attaches to one vulnerability impact finding (issue #595). It
// records the installed version, the source-reported vulnerable range, the
// first patched version Eshu can defend, every published fixed-version
// branch, the manifest range preserved from package consumption evidence,
// whether that manifest range admits the patched version, the direct or
// transitive designation, the parent package the caller would need to
// upgrade for transitive findings, the ecosystem the remediation was
// computed for, a stable machine-readable reason code, a confidence label,
// and structured missing-evidence reasons.
//
// The reducer never auto-applies remediation; this block is strictly
// advisory.
type Remediation struct {
	Ecosystem              string               `json:"ecosystem,omitempty"`
	CurrentVersion         string               `json:"current_version,omitempty"`
	VulnerableRange        string               `json:"vulnerable_range,omitempty"`
	FixedVersionSource     string               `json:"fixed_version_source,omitempty"`
	MatchReason            string               `json:"match_reason,omitempty"`
	FirstPatchedVersion    string               `json:"first_patched_version,omitempty"`
	PatchedVersionBranches []FixedVersionBranch `json:"patched_version_branches,omitempty"`
	ManifestRange          string               `json:"manifest_range,omitempty"`
	ManifestAllowsFix      string               `json:"manifest_allows_fix,omitempty"`
	Direct                 *bool                `json:"direct,omitempty"`
	ParentPackage          string               `json:"parent_package,omitempty"`
	Confidence             string               `json:"confidence,omitempty"`
	Reason                 string               `json:"reason,omitempty"`
	MissingEvidence        []string             `json:"missing_evidence,omitempty"`
}

// ReadinessState classifies one bounded vulnerability impact
// answer so callers can tell missing target coverage from a clean result.
type ReadinessState string

const (
	// ReadinessStateNotConfigured means Eshu has no advisory ingestion for the
	// requested scope, so a zero-finding answer cannot be interpreted as safe.
	ReadinessStateNotConfigured ReadinessState = "not_configured"
	// ReadinessStateTargetIncomplete means at least one required target source
	// reported partial or in-flight collection for the requested scope.
	ReadinessStateTargetIncomplete ReadinessState = "target_incomplete"
	// ReadinessStateTargetSkipped means every required target source settled
	// and at least one requested derived package carries planner skip
	// evidence: the system already knows it will never plan that package, so
	// nothing is in flight for it. The planner's skip reason rides in
	// incomplete_reasons as osv:skipped:<reason> and in the derived row's
	// skip_reasons. Callers must not interpret this as clean or affected.
	ReadinessStateTargetSkipped ReadinessState = "target_skipped"
	// ReadinessStateEvidenceIncomplete means some target evidence exists but a
	// required join family is missing for the requested scope.
	ReadinessStateEvidenceIncomplete ReadinessState = "evidence_incomplete"
	// ReadinessStateReadyZeroFindings means required evidence is present, the
	// reducer ran, and no impact finding matched the scope.
	ReadinessStateReadyZeroFindings ReadinessState = "ready_zero_findings"
	// ReadinessStateReadyWithFindings means required evidence is present and
	// reducer-owned impact findings exist for the scope.
	ReadinessStateReadyWithFindings ReadinessState = "ready_with_findings"
	// ReadinessStateAmbiguousScope means the bounded scope matched multiple
	// reducer-owned impact findings, so callers must narrow the request before
	// interpreting readiness as clean or affected.
	ReadinessStateAmbiguousScope ReadinessState = "ambiguous_scope"
	// ReadinessStateReadinessUnavailable means the readiness lookup itself
	// failed; the findings page is still returned but its coverage cannot be
	// classified. Callers must not interpret zero findings as safe in this
	// state.
	ReadinessStateReadinessUnavailable ReadinessState = "readiness_unavailable"
	// ReadinessStateUnsupported means Eshu observed real target evidence
	// (owned dependency in an unsupported ecosystem, package-manager file
	// flagged with an unsupported lockfile feature, VCS/path/URL/editable
	// dependency source, malformed/unsupported SBOM, oversized
	// package-registry metadata, or unsupported image manifest) the matcher
	// cannot resolve into a finding. Callers must not interpret this as clean
	// or affected.
	ReadinessStateUnsupported ReadinessState = "unsupported"
)

// ReadinessEnvelope is the readiness payload attached to a
// vulnerability impact response so a UI, MCP client, or operator can tell
// "nothing matched" from "Eshu has not collected the evidence yet."
type ReadinessEnvelope struct {
	State              ReadinessState      `json:"readiness_state"`
	TargetScope        TargetScope         `json:"target_scope"`
	EvidenceSources    []EvidenceFamily    `json:"evidence_sources"`
	SourceSnapshots    []SourceSnapshot    `json:"source_snapshots,omitempty"`
	SourceStates       []SourceState       `json:"source_states,omitempty"`
	UnsupportedTargets []UnsupportedTarget `json:"unsupported_targets,omitempty"`
	MissingEvidence    []string            `json:"missing_evidence,omitempty"`
	IncompleteReasons  []string            `json:"incomplete_reasons,omitempty"`
	Freshness          string              `json:"freshness"`
	Counts             ReadinessCounts     `json:"counts"`
}

// TargetScope echoes the bounded anchors the caller used so
// the readiness verdict is reproducible without re-deriving query parameters.
type TargetScope struct {
	CVEID         string `json:"cve_id,omitempty"`
	AdvisoryID    string `json:"advisory_id,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	RepositoryID  string `json:"repository_id,omitempty"`
	SubjectDigest string `json:"subject_digest,omitempty"`
	ImageRef      string `json:"image_ref,omitempty"`
	Ecosystem     string `json:"ecosystem,omitempty"`
	WorkloadID    string `json:"workload_id,omitempty"`
	ServiceID     string `json:"service_id,omitempty"`
	Environment   string `json:"environment,omitempty"`
	Severity      string `json:"severity,omitempty"`
	Status        string `json:"impact_status,omitempty"`
}

// EvidenceFamily summarizes one source-evidence family for the
// requested scope without leaking package names or advisory bodies.
const (
	// EvidenceFamilyVulnerabilityOSPackage groups installed OS-package
	// evidence from the OS-package vulnerability collector (apk/dpkg/rpm),
	// counted only for the scanned image the request's subject digest or
	// image reference resolves to. os_package facts carry no image identity
	// of their own (sdk/go/factschema/vulnerability/v1/os_package.go); the
	// readiness store resolves the digest through the sibling
	// scanner_worker.analysis fact, pair-keyed first (same scope and
	// generation, the reducer's SubjectDigest key:
	// go/internal/reducer/supplychain/core/impact.go) with a scope-level
	// fallback across committed generations for provably single-digest
	// scopes (issue #8878: sibling tiers supersede the analysis generation
	// under the active os_package generation). The fallback is withheld on
	// rescanned multi-digest scopes (issue #9685), where scope-level
	// attribution would count another image's packages; the pair-keyed
	// image_reference arm is withheld there too (issue #9691), since both
	// generations of a rescan share one reference. A present-but-zero
	// count for an image that DID scan (see
	// EvidenceFamilyScannerWorkerAnalysis) is therefore ambiguous on its
	// own: on a single-digest scope it is a valid "no installed packages
	// found" observation, while on a multi-digest scope with an older
	// subject digest it is a deliberately withheld attribution, not a
	// clean scan and not missing evidence. Tell the two apart by the
	// scope's digest cardinality (scope_with_multiple_digests), never by
	// the zero alone.
	EvidenceFamilyVulnerabilityOSPackage = "vulnerability.os_package"
	// EvidenceFamilyScannerWorkerAnalysis groups scanner-worker image
	// analysis facts, which the analyzer emits only for a completed image
	// scan (sdk/go/factschema/scannerworker/v1/analysis.go). This is the
	// signal that distinguishes "this image was never scanned" (family
	// absent) from "scanned and clean" (family present, zero findings): the
	// analysis fact is recorded whether or not the scan found any installed
	// OS packages to match.
	EvidenceFamilyScannerWorkerAnalysis = "scanner_worker.analysis"
)

type EvidenceFamily struct {
	Family           string `json:"family"`
	FactCount        int    `json:"fact_count"`
	LatestObservedAt string `json:"latest_observed_at,omitempty"`
	Freshness        string `json:"freshness,omitempty"`
}

// SourceSnapshot exposes source-cache and observation metadata
// for vulnerability source snapshots without returning raw advisory payloads.
type SourceSnapshot struct {
	Source               string `json:"source"`
	Ecosystem            string `json:"ecosystem,omitempty"`
	CacheArtifactVersion string `json:"cache_artifact_version,omitempty"`
	SnapshotDigest       string `json:"snapshot_digest,omitempty"`
	LastUpdatedAt        string `json:"last_updated_at,omitempty"`
	Freshness            string `json:"freshness,omitempty"`
	Complete             bool   `json:"complete"`
	WarningCode          string `json:"warning_code,omitempty"`
	WarningMessage       string `json:"warning_message,omitempty"`
}

// ReadinessCounts surfaces enough numeric coverage to diagnose
// a zero or partial answer without exposing raw payloads.
type ReadinessCounts struct {
	FindingsReturned   int            `json:"findings_returned"`
	FindingsTruncated  bool           `json:"findings_truncated"`
	FindingsByStatus   map[string]int `json:"findings_by_status,omitempty"`
	EvidenceFactsTotal int            `json:"evidence_facts_total"`
}

// SourceState exposes durable vulnerability source
// checkpoint, retry, and freshness state without raw advisory payloads.
type SourceState struct {
	CollectorInstanceID string            `json:"collector_instance_id"`
	ScopeID             string            `json:"scope_id"`
	Source              string            `json:"source"`
	Ecosystem           string            `json:"ecosystem,omitempty"`
	CollectionWindow    SourceStateWindow `json:"collection_window,omitempty"`
	LastAttemptAt       string            `json:"last_attempt_at,omitempty"`
	LastSuccessAt       string            `json:"last_success_at,omitempty"`
	NextRetryAt         string            `json:"next_retry_at,omitempty"`
	LastErrorClass      string            `json:"last_error_class,omitempty"`
	FreshnessState      string            `json:"freshness_state"`
	TerminalStatus      string            `json:"terminal_status"`
	ResultCount         int               `json:"result_count"`
	WarningCount        int               `json:"warning_count"`
	UpdatedAt           string            `json:"updated_at,omitempty"`
	// Derived marks one aggregate row standing for every derived
	// owned-package OSV scope that covers the request's packages in one
	// (collector instance, source, ecosystem). Its scope_id is a
	// derived:// name, not a collector scope. The fields below are set only
	// on derived rows. FreshnessState is the worst aged state of the kept
	// scopes, read as pending when any requested package has no scope.
	Derived bool `json:"derived,omitempty"`
	// ScopeCount is the number of distinct derived scopes kept: the newest
	// scope per requested package and release, then deduplicated. It includes a
	// planned release with no state yet, which reads pending.
	ScopeCount int `json:"scope_count,omitempty"`
	// PackageCount is the number of requested packages a scope covered or the
	// latest plan expects a scope for; it does not mean every release is fresh.
	PackageCount int `json:"package_count,omitempty"`
	// UnmatchedPackageCount is the number of requested packages no derived
	// scope covered. It includes packages the planner skipped and scopes
	// written before package keys were recorded.
	UnmatchedPackageCount int `json:"unmatched_package_count,omitempty"`
	// FreshCount through PendingCount count the kept scopes by aged state.
	FreshCount       int `json:"fresh_count,omitempty"`
	StaleCount       int `json:"stale_count,omitempty"`
	FailedCount      int `json:"failed_count,omitempty"`
	RateLimitedCount int `json:"rate_limited_count,omitempty"`
	PartialCount     int `json:"partial_count,omitempty"`
	PendingCount     int `json:"pending_count,omitempty"`
	// ScopeIDs lists up to 10 kept scope ids, worst state first and oldest
	// success first within a state.
	ScopeIDs []string `json:"scope_ids,omitempty"`
	// MultiCVETarget marks a configured NVD target that lists several CVEs:
	// absence stays per target, so the target certifies none of its CVEs
	// absent once NVD returned any record for it (#9322).
	MultiCVETarget bool `json:"multi_cve_target,omitempty"`
	// SkippedPackageCount is the number of requested packages no derived
	// scope covered that carry planner skip evidence on a derived aggregate
	// row (#9612). It is zero on configured rows.
	SkippedPackageCount int `json:"skipped_package_count,omitempty"`
	// SkipReasons lists the distinct planner skip reasons behind the skipped
	// packages, such as derived_target_non_exact_version. It carries reasons
	// only, never package names.
	SkipReasons []string `json:"skip_reasons,omitempty"`
	// RetiredReleases names the derived releases the fleet probe retired for
	// this aggregate, as source:distro:release (osv:alpine:v3.17): a release
	// no active installed package still carries stops counting even when its
	// last state is not fresh. A retired release never appears in
	// incomplete_reasons; this field is the operator-visible record that it
	// stopped counting (#10135).
	RetiredReleases  []string `json:"retired_releases,omitempty"`
	coverageEligible bool
	coverageZero     bool
}

// SourceStateWindow is the bounded source collection window.
type SourceStateWindow struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// UnsupportedTarget describes one observed vulnerability
// target Eshu cannot match precisely with the current matcher set. Targets
// carry the kind of evidence Eshu saw and a stable reason code so callers can
// tell "we observed something we cannot resolve" from "we collected nothing".
//
// Unsupported targets never imply the target is safe or affected. They are
// coverage-gap evidence: the producer observed a real artifact but the
// matcher could not turn that artifact into a finding.
type UnsupportedTarget struct {
	TargetKind     string `json:"target_kind"`
	Reason         string `json:"reason"`
	Count          int    `json:"count"`
	Ecosystem      string `json:"ecosystem,omitempty"`
	LockfileFlavor string `json:"lockfile_flavor,omitempty"`
	FeatureToken   string `json:"feature_token,omitempty"`
}

// VersionResolutionCorroboration is one weaker deployment-truth
// tier's own digest/version claim for a finding whose judged version/digest
// (version_resolution_tier) was resolved from a stronger tier. Every present
// weaker tier is disclosed here, including a tier whose claim disagrees with
// the winner -- Agreement is "disagrees" in that case rather than the entry
// being dropped, so an operator can see that two evidence sources disagree
// instead of only ever seeing confirmation. A cross-axis pair (for example a
// version compared against a digest) can never textually agree, so it is
// reported "not_comparable" rather than a misleading "disagrees".
type VersionResolutionCorroboration struct {
	Tier            string `json:"tier"`
	DigestOrVersion string `json:"digest_or_version,omitempty"`
	EvidenceKind    string `json:"evidence_kind,omitempty"`
	Agreement       string `json:"agreement"`
}

// RuntimeContextResult is the response-side envelope attached to
// one impact finding as `runtime_context` (#5746). TruthBasis labels the
// resolution path so a caller cannot mistake these IDs for baked payload
// fields. The workload_id/service_id/environment filters resolve the same
// current repository mappings independently (#5747).
type RuntimeContextResult struct {
	// TruthBasis is always "read_time_resolved": the context was resolved
	// from the repository's active runtime facts at query time, not baked
	// into the finding at reduce time. Empty lists are an honest "no runtime
	// facts landed yet" (fresh ingest) that self-heals on the next read.
	TruthBasis    string   `json:"truth_basis"`
	WorkloadIDs   []string `json:"workload_ids,omitempty"`
	ServiceIDs    []string `json:"service_ids,omitempty"`
	DeploymentIDs []string `json:"deployment_ids,omitempty"`
	Environments  []string `json:"environments,omitempty"`
	// EnvironmentEvidence records the strongest current corroboration state for
	// each admitted exact (subject_digest, environment) lookup. Repository
	// context contributes candidate names only; it cannot supply or default an
	// evidence value. Values use the existing deploy_event/declared vocabulary.
	EnvironmentEvidence map[string]string `json:"environment_evidence,omitempty"`
	// EnvironmentEvidenceProbe reports this finding's page-weighted current
	// confirmation budget. CandidatesTruncated means visible candidate names
	// exceeded that budget; it never reflects hidden or unauthorized facts.
	EnvironmentEvidenceProbe *RuntimeEnvironmentEvidenceProbe `json:"environment_evidence_probe,omitempty"`
	CatalogEntityRefs        []string                         `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs         []string                         `json:"catalog_owner_refs,omitempty"`
}

// RuntimeEnvironmentEvidenceProbe describes the bounded current
// confirmation work performed for one finding's environment candidates.
type RuntimeEnvironmentEvidenceProbe struct {
	CandidateLimit      int  `json:"candidate_limit"`
	CandidatesTruncated bool `json:"candidates_truncated"`
}

// KubernetesRuntimeWorkloadRef is one current, authorized Kubernetes workload
// observed running the parent finding's exact subject digest.
type KubernetesRuntimeWorkloadRef struct {
	UID       string `json:"workload_uid"`
	ClusterID string `json:"cluster_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

// KubernetesRuntimeProbeMetadata describes the bounded, page-weighted
// digest-local candidate budget. A nil WorkloadRefsTruncated means
// authorization prevents the caller from learning whether hidden candidates
// exist.
type KubernetesRuntimeProbeMetadata struct {
	CandidateLimit        int   `json:"candidate_limit"`
	WorkloadRefsTruncated *bool `json:"workload_refs_truncated"`
}
