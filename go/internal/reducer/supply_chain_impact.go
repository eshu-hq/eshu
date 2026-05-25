package reducer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SupplyChainImpactStatus names the reducer decision for one vulnerability
// impact finding.
type SupplyChainImpactStatus string

const (
	// SupplyChainImpactAffectedExact means package identity and observed
	// version match the affected package evidence exactly.
	SupplyChainImpactAffectedExact SupplyChainImpactStatus = "affected_exact"
	// SupplyChainImpactAffectedDerived means impact follows from SBOM, image,
	// repository, or runtime joins after package identity is established.
	SupplyChainImpactAffectedDerived SupplyChainImpactStatus = "affected_derived"
	// SupplyChainImpactPossiblyAffected means advisory evidence exists but
	// package identity or version precision is incomplete.
	SupplyChainImpactPossiblyAffected SupplyChainImpactStatus = "possibly_affected"
	// SupplyChainImpactNotAffectedKnownFixed means the observed version is at
	// or beyond a source-reported fixed version under Eshu's conservative
	// numeric version comparison.
	SupplyChainImpactNotAffectedKnownFixed SupplyChainImpactStatus = "not_affected_known_fixed"
	// SupplyChainImpactUnknown means vulnerability source truth exists but Eshu
	// lacks enough package or runtime evidence to decide impact.
	SupplyChainImpactUnknown SupplyChainImpactStatus = "unknown_impact"
)

// SupplyChainImpactFactFilter bounds active evidence loading for one impact
// reducer intent.
type SupplyChainImpactFactFilter struct {
	PackageIDs      []string
	PURLs           []string
	CVEIDs          []string
	SubjectDigests  []string
	DocumentIDs     []string
	ProductCriteria []string
	RepositoryIDs   []string
	ImageRefs       []string
}

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
//
// Severity, fixed-version, and vulnerable-range fields carry per-source
// provenance so admission preserves which advisory source supplied each
// selected value and what alternates other sources reported. Reducers select
// one value per field using documented ecosystem-aware source priority.
type SupplyChainImpactFinding struct {
	CVEID                string
	AdvisoryID           string
	PackageID            string
	Ecosystem            string
	PackageName          string
	PURL                 string
	ProductCriteria      string
	MatchCriteriaID      string
	ObservedVersion      string
	RequestedRange       string
	FixedVersion         string
	MatchReason          string
	Status               SupplyChainImpactStatus
	Confidence           string
	CVSSScore            float64
	SeveritySource       string
	SeverityVector       string
	SeverityLabel        string
	AlternateSeverities  []AlternateSeverity
	FixedVersionSource   string
	FixedVersionBranches []FixedVersionBranch
	RangeSource          string
	AdvisorySources      []AdvisorySourceObservation
	EPSSProbability      string
	EPSSPercentile       string
	KnownExploited       bool
	PriorityReason       string
	RuntimeReachability  string
	RepositoryID         string
	SubjectDigest        string
	ImageRef             string
	WorkloadIDs          []string
	ServiceIDs           []string
	Environments         []string
	DependencyPath       []string
	DependencyDepth      int
	DirectDependency     *bool
	MissingEvidence      []string
	EvidencePath         []string
	EvidenceFactIDs      []string
	CanonicalWrites      int
	// DetectionProfile records which tier this finding meets: precise for
	// exact installed-version anchors, comprehensive for range-only,
	// SBOM-derived, product-derived, malformed, unsupported-ecosystem, or
	// missing-version evidence. Always set by BuildSupplyChainImpactFindings
	// before the writer persists the finding.
	DetectionProfile DetectionProfile
}

// SupplyChainImpactWrite carries findings for durable publication.
type SupplyChainImpactWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Findings     []SupplyChainImpactFinding
}

// SupplyChainImpactWriteResult summarizes durable impact publication.
type SupplyChainImpactWriteResult struct {
	CanonicalWrites int
	FactsWritten    int
	EvidenceSummary string
}

// SupplyChainImpactWriter persists reducer-owned impact findings.
type SupplyChainImpactWriter interface {
	WriteSupplyChainImpactFindings(context.Context, SupplyChainImpactWrite) (SupplyChainImpactWriteResult, error)
}

type activeSupplyChainImpactFactLoader interface {
	ListActiveSupplyChainImpactFacts(context.Context, SupplyChainImpactFactFilter) ([]facts.Envelope, error)
}

// SupplyChainImpactHandler publishes vulnerability impact findings without
// turning CVSS, EPSS, or KEV signals into reachability proof.
type SupplyChainImpactHandler struct {
	FactLoader  FactLoader
	Writer      SupplyChainImpactWriter
	Instruments *telemetry.Instruments
}

// Handle executes one supply-chain impact reducer intent.
func (h SupplyChainImpactHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSupplyChainImpact {
		return Result{}, fmt.Errorf("supply_chain_impact handler does not accept domain %q", intent.Domain)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("supply chain impact fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("supply chain impact writer is required")
	}

	envelopes, err := loadFactsForKinds(ctx, h.FactLoader, intent.ScopeID, intent.GenerationID, supplyChainImpactFactKinds())
	if err != nil {
		return Result{}, fmt.Errorf("load supply chain impact facts: %w", err)
	}
	envelopes, err = h.loadActiveSupplyChainImpactFactsUntilStable(ctx, envelopes)
	if err != nil {
		return Result{}, fmt.Errorf("load active supply chain impact facts: %w", err)
	}

	findings := BuildSupplyChainImpactFindings(envelopes)
	counts := supplyChainImpactCounts(findings)
	writeResult, err := h.Writer.WriteSupplyChainImpactFindings(ctx, SupplyChainImpactWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Findings:     findings,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write supply chain impact findings: %w", err)
	}
	h.emitCounters(ctx, counts)

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSupplyChainImpact,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: supplyChainImpactSummary(len(findings), counts, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeSupplyChainImpactFactLoader)
	if !ok || filter.empty() {
		return nil, nil
	}
	envelopes, err := loader.ListActiveSupplyChainImpactFacts(ctx, filter)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

const maxSupplyChainImpactActiveEvidenceLoads = 8

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFactsUntilStable(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	requested := SupplyChainImpactFactFilter{}
	next := supplyChainImpactFilter(envelopes)
	for loads := 0; !next.empty(); loads++ {
		if loads >= maxSupplyChainImpactActiveEvidenceLoads {
			return nil, fmt.Errorf(
				"active evidence expansion exceeded %d bounded loads",
				maxSupplyChainImpactActiveEvidenceLoads,
			)
		}
		active, err := h.loadActiveSupplyChainImpactFacts(ctx, next)
		if err != nil {
			return nil, err
		}
		requested = mergeSupplyChainImpactFactFilters(requested, next)
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, active...)
		next = supplyChainImpactFollowUpFilter(requested, supplyChainImpactFilter(envelopes))
	}
	return envelopes, nil
}

func (h SupplyChainImpactHandler) emitCounters(ctx context.Context, counts map[SupplyChainImpactStatus]int) {
	if h.Instruments == nil {
		return
	}
	for _, status := range supplyChainImpactStatuses() {
		if counts[status] == 0 {
			continue
		}
		h.Instruments.SupplyChainImpactFindings.Add(ctx, int64(counts[status]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainSupplyChainImpact)),
			telemetry.AttrOutcome(string(status)),
		))
	}
}

// BuildSupplyChainImpactFindings classifies vulnerability source facts against
// explicit package, SBOM, image, and repository evidence.
//
// Multi-source CVE and affected_package observations for the same advisory
// identity are consolidated into one finding so callers see a single row per
// (cve_id, package_id) anchor with full per-source provenance, instead of one
// row per advisory source overwriting the others at the writer.
func BuildSupplyChainImpactFindings(envelopes []facts.Envelope) []SupplyChainImpactFinding {
	index := buildSupplyChainImpactIndex(envelopes)
	cveGroups := groupSupplyChainCVEsByID(index.cves)
	findings := make([]SupplyChainImpactFinding, 0, len(index.affectedPackages)+len(index.affectedProducts))
	for _, cveID := range sortedCVEKeys(cveGroups) {
		group := cveGroups[cveID]
		affected := index.affectedPackages[cveID]
		if len(affected) > 0 {
			pkgGroups := groupSupplyChainAffectedByPackage(affected)
			for _, packageID := range sortedPackageKeys(pkgGroups) {
				pkgs := pkgGroups[packageID]
				findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactPackage(group, pkgs, index))
			}
			continue
		}
		products := index.affectedProducts[cveID]
		if len(products) > 0 {
			for _, product := range products {
				findings = appendSupplyChainImpactFinding(findings, classifySupplyChainImpactProduct(group.representative(), product, index))
			}
			continue
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].CVEID != findings[j].CVEID {
			return findings[i].CVEID < findings[j].CVEID
		}
		if findings[i].PackageID != findings[j].PackageID {
			return findings[i].PackageID < findings[j].PackageID
		}
		return findings[i].ProductCriteria < findings[j].ProductCriteria
	})
	return findings
}

func appendSupplyChainImpactFinding(
	findings []SupplyChainImpactFinding,
	finding SupplyChainImpactFinding,
) []SupplyChainImpactFinding {
	if !supplyChainImpactFindingHasOwnedAnchor(finding) {
		return findings
	}
	finding.DetectionProfile = classifySupplyChainImpactDetectionProfile(finding)
	return append(findings, finding)
}

func supplyChainImpactFindingHasOwnedAnchor(finding SupplyChainImpactFinding) bool {
	return strings.TrimSpace(finding.RepositoryID) != "" || strings.TrimSpace(finding.SubjectDigest) != ""
}

func supplyChainImpactFactKinds() []string {
	return []string{
		facts.VulnerabilityCVEFactKind,
		facts.VulnerabilityAffectedPackageFactKind,
		facts.VulnerabilityAffectedProductFactKind,
		facts.VulnerabilityEPSSScoreFactKind,
		facts.VulnerabilityKnownExploitedFactKind,
		facts.PackageRegistryPackageFactKind,
		facts.SBOMComponentFactKind,
		sbomAttestationAttachmentFactKind,
		containerImageIdentityFactKind,
		packageConsumptionCorrelationFactKind,
		cicdRunCorrelationFactKind,
		serviceCatalogCorrelationFactKind,
	}
}

func supplyChainImpactStatuses() []SupplyChainImpactStatus {
	return []SupplyChainImpactStatus{
		SupplyChainImpactAffectedExact,
		SupplyChainImpactAffectedDerived,
		SupplyChainImpactPossiblyAffected,
		SupplyChainImpactNotAffectedKnownFixed,
		SupplyChainImpactUnknown,
	}
}

func supplyChainImpactCounts(findings []SupplyChainImpactFinding) map[SupplyChainImpactStatus]int {
	counts := make(map[SupplyChainImpactStatus]int, len(supplyChainImpactStatuses()))
	for _, finding := range findings {
		counts[finding.Status]++
	}
	return counts
}

func supplyChainImpactSummary(
	evaluated int,
	counts map[SupplyChainImpactStatus]int,
	canonicalWrites int,
) string {
	return fmt.Sprintf(
		"supply chain impact evaluated=%d affected_exact=%d affected_derived=%d possibly_affected=%d not_affected_known_fixed=%d unknown_impact=%d canonical_writes=%d",
		evaluated,
		counts[SupplyChainImpactAffectedExact],
		counts[SupplyChainImpactAffectedDerived],
		counts[SupplyChainImpactPossiblyAffected],
		counts[SupplyChainImpactNotAffectedKnownFixed],
		counts[SupplyChainImpactUnknown],
		canonicalWrites,
	)
}

func supplyChainImpactCanonicalWrites(findings []SupplyChainImpactFinding) int {
	total := 0
	for _, finding := range findings {
		total += finding.CanonicalWrites
	}
	return total
}

func supplyChainCVEID(payload map[string]any) string {
	return firstNonBlank(payloadStr(payload, "cve_id"), payloadStr(payload, "advisory_id"))
}

func supplyChainFloat(payload map[string]any, key string) float64 {
	raw := strings.TrimSpace(fmt.Sprint(payload[key]))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}
