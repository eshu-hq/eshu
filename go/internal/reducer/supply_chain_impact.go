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
	PackageIDs     []string
	PURLs          []string
	CVEIDs         []string
	SubjectDigests []string
}

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
type SupplyChainImpactFinding struct {
	CVEID               string
	AdvisoryID          string
	PackageID           string
	Ecosystem           string
	PackageName         string
	PURL                string
	ObservedVersion     string
	FixedVersion        string
	Status              SupplyChainImpactStatus
	Confidence          string
	CVSSScore           float64
	EPSSProbability     string
	EPSSPercentile      string
	KnownExploited      bool
	PriorityReason      string
	RuntimeReachability string
	RepositoryID        string
	SubjectDigest       string
	MissingEvidence     []string
	EvidencePath        []string
	EvidenceFactIDs     []string
	CanonicalWrites     int
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
	active, err := h.loadActiveSupplyChainImpactFacts(ctx, supplyChainImpactFilter(envelopes))
	if err != nil {
		return Result{}, fmt.Errorf("load active supply chain impact facts: %w", err)
	}
	envelopes = append(envelopes, active...)

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
func BuildSupplyChainImpactFindings(envelopes []facts.Envelope) []SupplyChainImpactFinding {
	index := buildSupplyChainImpactIndex(envelopes)
	findings := make([]SupplyChainImpactFinding, 0, len(index.affectedPackages))
	for _, cve := range index.cves {
		affected := index.affectedPackages[cve.cveID]
		if len(affected) == 0 {
			findings = append(findings, unknownSupplyChainImpactFinding(cve, index))
			continue
		}
		for _, pkg := range affected {
			findings = append(findings, classifySupplyChainImpactPackage(cve, pkg, index))
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].CVEID != findings[j].CVEID {
			return findings[i].CVEID < findings[j].CVEID
		}
		return findings[i].PackageID < findings[j].PackageID
	})
	return findings
}

func supplyChainImpactFactKinds() []string {
	return []string{
		facts.VulnerabilityCVEFactKind,
		facts.VulnerabilityAffectedPackageFactKind,
		facts.VulnerabilityAffectedProductFactKind,
		facts.VulnerabilityEPSSScoreFactKind,
		facts.VulnerabilityKnownExploitedFactKind,
		facts.PackageRegistryPackageVersionFactKind,
		facts.PackageRegistryVulnerabilityHintFactKind,
		facts.SBOMComponentFactKind,
		sbomAttestationAttachmentFactKind,
		containerImageIdentityFactKind,
		packageConsumptionCorrelationFactKind,
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

func supplyChainImpactFilter(envelopes []facts.Envelope) SupplyChainImpactFactFilter {
	var packageIDs, purls, cveIDs, digests []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.VulnerabilityCVEFactKind:
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.VulnerabilityAffectedPackageFactKind:
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			purls = append(purls, payloadStr(envelope.Payload, "purl"))
			cveIDs = append(cveIDs, supplyChainCVEID(envelope.Payload))
		case facts.SBOMComponentFactKind:
			purls = append(purls, payloadStr(envelope.Payload, "purl"))
		case sbomAttestationAttachmentFactKind:
			digests = append(digests, payloadStr(envelope.Payload, "subject_digest"))
		}
	}
	return SupplyChainImpactFactFilter{
		PackageIDs:     uniqueSortedStrings(packageIDs),
		PURLs:          uniqueSortedStrings(purls),
		CVEIDs:         uniqueSortedStrings(cveIDs),
		SubjectDigests: uniqueSortedStrings(digests),
	}
}

func (f SupplyChainImpactFactFilter) empty() bool {
	return len(f.PackageIDs) == 0 && len(f.PURLs) == 0 && len(f.CVEIDs) == 0 && len(f.SubjectDigests) == 0
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
