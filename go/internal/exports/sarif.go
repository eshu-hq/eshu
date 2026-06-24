// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exports //nolint:filelength // 548 lines: SARIF v2.1.0 reference writer. Per internal/exports/AGENTS.md, the SARIF writer owns the full wire contract (rules, results, locations, partialFingerprints, properties) so the golden fixtures lock a single byte-stable file.

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	sarifSchemaURI = "https://json.schemastore.org/sarif-2.1.0.json"
	sarifVersion   = "2.1.0"
)

// SARIFExporter renders a [Snapshot] as SARIF v2.1.0.
//
// The exporter is stateless; one instance is safe to share across
// goroutines. Findings are filtered to the snapshot scope, paths are passed
// through opts.Redactor when set, and findings + rules are sorted before
// emit so the same input produces byte-identical output.
type SARIFExporter struct{}

// NewSARIFExporter constructs a [SARIFExporter].
func NewSARIFExporter() SARIFExporter { return SARIFExporter{} }

// Format reports [FormatSARIF].
func (SARIFExporter) Format() Format { return FormatSARIF }

// Export renders snapshot as SARIF and writes it to w.
//
// The output is pretty-printed with a fixed two-space indent so golden
// fixtures stay readable in diffs, and ends with a single trailing newline
// produced by [encoding/json.Encoder.Encode]; callers do not need to add
// one. Both the indentation and the trailing newline are part of the wire
// contract that golden fixtures lock in `testdata/sarif/`.
func (e SARIFExporter) Export(w io.Writer, snapshot Snapshot, opts Options) error {
	if err := snapshot.Scope.Validate(); err != nil {
		return fmt.Errorf("sarif export: %w", err)
	}
	log, err := buildSARIFLog(snapshot, opts)
	if err != nil {
		return fmt.Errorf("sarif export: %w", err)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(log); err != nil {
		return fmt.Errorf("sarif export: encode: %w", err)
	}
	return nil
}

func buildSARIFLog(snapshot Snapshot, opts Options) (sarifLog, error) {
	driverName := strings.TrimSpace(opts.Tool.Name)
	if driverName == "" {
		driverName = "eshu"
	}
	driverURI := strings.TrimSpace(opts.Tool.URI)
	if driverURI == "" {
		driverURI = "https://eshu.dev"
	}

	scoped, dropped := filterFindingsToScope(snapshot.Findings, snapshot.Scope)
	redactedPaths := 0
	if opts.Redactor != nil {
		scoped, redactedPaths = applyPathRedaction(scoped, opts.Redactor)
	}

	sortFindings(scoped)
	rules, ruleIndex := buildRules(scoped)
	results := buildResults(scoped, ruleIndex)
	rules, results = appendStatusResults(rules, ruleIndex, results, snapshot.Status)

	run := sarifRun{
		Tool: sarifTool{
			Driver: sarifDriver{
				Name:           driverName,
				Version:        strings.TrimSpace(opts.Tool.Version),
				InformationURI: driverURI,
				Rules:          rules,
			},
		},
		Results: results,
		Properties: &sarifProperties{
			Scope:           snapshot.Scope.Target(),
			GeneratedAt:     formatSARIFTime(snapshot.GeneratedAt),
			DroppedFindings: dropped,
			RedactedPaths:   redactedPaths,
			FormatVersion:   sarifVersion,
		},
	}
	applySARIFStatusProperties(run.Properties, snapshot.Status)
	if !snapshot.GeneratedAt.IsZero() {
		run.Invocations = []sarifInvocation{{
			ExecutionSuccessful: true,
			StartTimeUTC:        formatSARIFTime(snapshot.GeneratedAt),
			EndTimeUTC:          formatSARIFTime(snapshot.GeneratedAt),
		}}
	}
	return sarifLog{
		Schema:  sarifSchemaURI,
		Version: sarifVersion,
		Runs:    []sarifRun{run},
	}, nil
}

// filterFindingsToScope drops findings whose own RepositoryID or
// SubjectDigest disagrees with the snapshot scope. PackageID and AdvisoryID
// scopes filter by the same fields on the finding. This is defense in depth:
// the caller already authorized scope when it built the snapshot, and this
// drop catches a handler bug that mixes evidence from a second target.
func filterFindingsToScope(findings []Finding, scope Scope) ([]Finding, int) {
	kept := make([]Finding, 0, len(findings))
	dropped := 0
	for _, finding := range findings {
		if !findingMatchesScope(finding, scope) {
			dropped++
			continue
		}
		kept = append(kept, cloneFinding(finding))
	}
	return kept, dropped
}

func cloneFinding(in Finding) Finding {
	out := in
	if len(in.Locations) > 0 {
		out.Locations = make([]Location, len(in.Locations))
		copy(out.Locations, in.Locations)
	}
	if len(in.AdvisorySources) > 0 {
		out.AdvisorySources = make([]AdvisorySource, len(in.AdvisorySources))
		copy(out.AdvisorySources, in.AdvisorySources)
	}
	return out
}

func findingMatchesScope(finding Finding, scope Scope) bool {
	switch scope.Kind {
	case ScopeKindRepository:
		return finding.RepositoryID == "" || finding.RepositoryID == scope.RepositoryID
	case ScopeKindImageDigest:
		return finding.SubjectDigest == "" || finding.SubjectDigest == scope.SubjectDigest
	case ScopeKindPackage:
		return finding.PackageID == "" || finding.PackageID == scope.PackageID
	case ScopeKindAdvisory:
		matchesAdvisory := scope.AdvisoryID == finding.AdvisoryID || scope.AdvisoryID == finding.CVEID
		return matchesAdvisory
	default:
		return false
	}
}

func applyPathRedaction(findings []Finding, redactor FieldRedactor) ([]Finding, int) {
	redacted := 0
	for findingIdx := range findings {
		locations := findings[findingIdx].Locations
		for locationIdx := range locations {
			raw := locations[locationIdx].ManifestPath
			if raw == "" {
				continue
			}
			redactedPath := redactor.RedactPath(raw)
			if redactedPath != raw {
				redacted++
			}
			locations[locationIdx].ManifestPath = redactedPath
		}
	}
	return findings, redacted
}

func sortFindings(findings []Finding) {
	for i := range findings {
		sortLocations(findings[i].Locations)
		sortAdvisorySources(findings[i].AdvisorySources)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].FindingID < findings[j].FindingID
	})
}

func sortLocations(locations []Location) {
	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].ManifestPath != locations[j].ManifestPath {
			return locations[i].ManifestPath < locations[j].ManifestPath
		}
		if locations[i].StartLine != locations[j].StartLine {
			return locations[i].StartLine < locations[j].StartLine
		}
		return locations[i].EndLine < locations[j].EndLine
	})
}

func sortAdvisorySources(sources []AdvisorySource) {
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].Source != sources[j].Source {
			return sources[i].Source < sources[j].Source
		}
		return sources[i].AdvisoryID < sources[j].AdvisoryID
	})
}

func buildRules(findings []Finding) ([]sarifRule, map[string]int) {
	byID := make(map[string]Finding)
	for _, finding := range findings {
		id := finding.RuleID()
		if _, exists := byID[id]; exists {
			continue
		}
		byID[id] = finding
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	rules := make([]sarifRule, 0, len(ids))
	index := make(map[string]int, len(ids))
	for position, id := range ids {
		rules = append(rules, buildRule(byID[id]))
		index[id] = position
	}
	return rules, index
}

func buildRule(finding Finding) sarifRule {
	rule := sarifRule{
		ID:                   finding.RuleID(),
		Name:                 ruleNameFor(finding),
		HelpURI:              finding.HelpURI,
		DefaultConfiguration: &sarifConfiguration{Level: severityToLevel(finding.Severity)},
		Properties:           buildRuleProperties(finding),
	}
	if summary := strings.TrimSpace(finding.Summary); summary != "" {
		rule.ShortDescription = &sarifMessage{Text: summary}
	}
	if description := strings.TrimSpace(finding.Description); description != "" {
		rule.FullDescription = &sarifMessage{Text: description}
	}
	return rule
}

func ruleNameFor(finding Finding) string {
	if finding.PackageName != "" {
		return finding.PackageName
	}
	if finding.PURL != "" {
		return finding.PURL
	}
	return finding.RuleID()
}

func buildRuleProperties(finding Finding) *sarifRuleProps {
	props := &sarifRuleProps{
		Severity:        string(finding.Severity),
		CVSSScore:       finding.CVSSScore,
		CVSSVector:      finding.CVSSVector,
		EPSSProbability: finding.EPSSProbability,
		KnownExploited:  finding.KnownExploited,
		Ecosystem:       finding.Ecosystem,
		PURL:            finding.PURL,
		AdvisorySources: copyAdvisorySources(finding.AdvisorySources),
		Tags:            ruleTags(finding),
	}
	if props.isEmpty() {
		return nil
	}
	return props
}

func (p sarifRuleProps) isEmpty() bool {
	return p.Severity == "" && p.CVSSScore == 0 && p.CVSSVector == "" &&
		p.EPSSProbability == "" && !p.KnownExploited && p.Ecosystem == "" &&
		p.PURL == "" && len(p.AdvisorySources) == 0 && len(p.Tags) == 0
}

func copyAdvisorySources(in []AdvisorySource) []sarifAdvisorySourceJS {
	if len(in) == 0 {
		return nil
	}
	out := make([]sarifAdvisorySourceJS, 0, len(in))
	for _, source := range in {
		out = append(out, sarifAdvisorySourceJS(source))
	}
	return out
}

func ruleTags(finding Finding) []string {
	tags := make([]string, 0, 3)
	tags = append(tags, "security", "vulnerability")
	if finding.KnownExploited {
		tags = append(tags, "kev")
	}
	if finding.Ecosystem != "" {
		tags = append(tags, "ecosystem:"+finding.Ecosystem)
	}
	sort.Strings(tags)
	return tags
}

func buildResults(findings []Finding, ruleIndex map[string]int) []sarifResult {
	results := make([]sarifResult, 0, len(findings))
	for _, finding := range findings {
		ruleID := finding.RuleID()
		results = append(results, sarifResult{
			RuleID:              ruleID,
			RuleIndex:           ruleIndex[ruleID],
			Level:               severityToLevel(finding.Severity),
			Message:             sarifMessage{Text: resultMessage(finding)},
			Locations:           buildSARIFLocations(finding.Locations),
			PartialFingerprints: buildFingerprints(finding),
			Properties:          buildResultProperties(finding),
		})
	}
	return results
}

func severityToLevel(severity Severity) string {
	switch severity {
	case SeverityCritical, SeverityHigh:
		return "error"
	case SeverityMedium:
		return "warning"
	case SeverityLow:
		return "note"
	case SeverityNone:
		return "none"
	default:
		return "none"
	}
}

func resultMessage(finding Finding) string {
	var builder strings.Builder
	builder.WriteString(finding.RuleID())
	if finding.PackageName != "" {
		builder.WriteString(" in ")
		builder.WriteString(finding.PackageName)
		if finding.ObservedVersion != "" {
			builder.WriteString("@")
			builder.WriteString(finding.ObservedVersion)
		}
	}
	if finding.FixedVersion != "" {
		builder.WriteString("; fixed in ")
		builder.WriteString(finding.FixedVersion)
	}
	if summary := strings.TrimSpace(finding.Summary); summary != "" {
		builder.WriteString(" — ")
		builder.WriteString(summary)
	}
	return builder.String()
}

func buildSARIFLocations(in []Location) []sarifLocation {
	if len(in) == 0 {
		return nil
	}
	out := make([]sarifLocation, 0, len(in))
	for _, location := range in {
		if location.ManifestPath == "" {
			continue
		}
		physical := sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: location.ManifestPath},
		}
		if location.StartLine > 0 || location.EndLine > 0 {
			physical.Region = &sarifRegion{
				StartLine: location.StartLine,
				EndLine:   location.EndLine,
			}
		}
		out = append(out, sarifLocation{PhysicalLocation: physical})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildFingerprints(finding Finding) map[string]string {
	fingerprints := map[string]string{
		"eshu/findingId/v1": finding.FindingID,
	}
	if finding.CVEID != "" {
		fingerprints["eshu/cveId/v1"] = finding.CVEID
	}
	if finding.AdvisoryID != "" {
		fingerprints["eshu/advisoryId/v1"] = finding.AdvisoryID
	}
	if finding.PackageID != "" {
		fingerprints["eshu/packageId/v1"] = finding.PackageID
	}
	if finding.ObservedVersion != "" {
		fingerprints["eshu/observedVersion/v1"] = finding.ObservedVersion
	}
	return fingerprints
}

func buildResultProperties(finding Finding) *sarifResultProps {
	props := &sarifResultProps{
		FindingID:                    finding.FindingID,
		PackageID:                    finding.PackageID,
		PackageName:                  finding.PackageName,
		ObservedVersion:              finding.ObservedVersion,
		FixedVersion:                 finding.FixedVersion,
		RepositoryID:                 finding.RepositoryID,
		SubjectDigest:                finding.SubjectDigest,
		ImageRef:                     finding.ImageRef,
		RuntimeReachability:          finding.RuntimeReachability,
		ReachabilityState:            reachabilityState(finding.Reachability),
		ReachabilityConfidence:       reachabilityConfidence(finding.Reachability),
		ReachabilitySource:           reachabilitySource(finding.Reachability),
		ReachabilityEvidence:         reachabilityEvidence(finding.Reachability),
		ReachabilityReason:           reachabilityReason(finding.Reachability),
		ReachabilityLanguageMaturity: reachabilityLanguageMaturity(finding.Reachability),
		ReachabilityMissingEvidence:  reachabilityMissingEvidence(finding.Reachability),
		ImpactStatus:                 finding.ImpactStatus,
		Confidence:                   finding.Confidence,
		RequestedRange:               finding.RequestedRange,
		VulnerableRange:              finding.VulnerableRange,
		MatchReason:                  finding.MatchReason,
		WorkloadIDs:                  cloneStrings(finding.WorkloadIDs),
		ServiceIDs:                   cloneStrings(finding.ServiceIDs),
		Environments:                 cloneStrings(finding.Environments),
		DependencyScope:              finding.DependencyScope,
		DependencyPath:               cloneStrings(finding.DependencyPath),
		DirectDependency:             finding.DirectDependency,
		MissingEvidence:              cloneStrings(finding.MissingEvidence),
		EvidenceFactIDs:              cloneStrings(finding.EvidenceFactIDs),
		SourceFreshness:              finding.SourceFreshness,
		Remediation:                  buildSARIFRemediation(finding.Remediation),
	}
	if props.FindingID == "" && props.PackageID == "" && props.PackageName == "" &&
		props.ObservedVersion == "" && props.FixedVersion == "" &&
		props.RepositoryID == "" && props.SubjectDigest == "" && props.ImageRef == "" &&
		props.RuntimeReachability == "" && props.ReachabilityState == "" &&
		props.ReachabilityConfidence == "" && props.ReachabilitySource == "" &&
		props.ReachabilityEvidence == "" && props.ReachabilityReason == "" &&
		props.ReachabilityLanguageMaturity == "" && len(props.ReachabilityMissingEvidence) == 0 &&
		props.ImpactStatus == "" && props.Confidence == "" &&
		props.RequestedRange == "" && props.VulnerableRange == "" && props.MatchReason == "" &&
		len(props.WorkloadIDs) == 0 && len(props.ServiceIDs) == 0 &&
		len(props.Environments) == 0 && props.DependencyScope == "" &&
		len(props.DependencyPath) == 0 && props.DirectDependency == nil &&
		len(props.MissingEvidence) == 0 && len(props.EvidenceFactIDs) == 0 &&
		props.SourceFreshness == "" && props.Remediation == nil {
		return nil
	}
	return props
}

func reachabilityState(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.State
}

func reachabilityConfidence(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.Confidence
}

func reachabilitySource(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.Source
}

func reachabilityEvidence(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.Evidence
}

func reachabilityReason(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.Reason
}

func reachabilityLanguageMaturity(reachability *Reachability) string {
	if reachability == nil {
		return ""
	}
	return reachability.LanguageMaturity
}

func reachabilityMissingEvidence(reachability *Reachability) []string {
	if reachability == nil {
		return nil
	}
	return cloneStrings(reachability.MissingEvidence)
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func buildSARIFRemediation(remediation *Remediation) *sarifRemediationProps {
	if remediation == nil {
		return nil
	}
	props := &sarifRemediationProps{
		CurrentVersion:      remediation.CurrentVersion,
		VulnerableRange:     remediation.VulnerableRange,
		FixedVersionSource:  remediation.FixedVersionSource,
		MatchReason:         remediation.MatchReason,
		FirstPatchedVersion: remediation.FirstPatchedVersion,
		ManifestRange:       remediation.ManifestRange,
		ManifestAllowsFix:   remediation.ManifestAllowsFix,
		Confidence:          remediation.Confidence,
		Reason:              remediation.Reason,
		Direct:              remediation.Direct,
		MissingEvidence:     cloneStrings(remediation.MissingEvidence),
	}
	if props.CurrentVersion == "" && props.VulnerableRange == "" &&
		props.FixedVersionSource == "" && props.MatchReason == "" &&
		props.FirstPatchedVersion == "" && props.ManifestRange == "" &&
		props.ManifestAllowsFix == "" && props.Confidence == "" &&
		props.Reason == "" && props.Direct == nil &&
		len(props.MissingEvidence) == 0 {
		return nil
	}
	return props
}

func formatSARIFTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
