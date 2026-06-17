package auditreport

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/auditpreflight"
	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// Recommendation is the suggested action for an audit finding. The generator
// never creates issues; it only recommends.
type Recommendation string

const (
	// RecNoIssue means no issue is needed (the foundation already exists).
	RecNoIssue Recommendation = "no_issue"
	// RecLinkExisting means link an existing issue rather than open a new one.
	RecLinkExisting Recommendation = "link_existing_issue"
	// RecUpdateExisting means update existing docs/issue (for example a stale claim).
	RecUpdateExisting Recommendation = "update_existing_issue"
	// RecCreateNew means draft a new issue (the gap is genuinely missing).
	RecCreateNew Recommendation = "create_new_issue_draft"
	// RecReview means the finding needs human review (invalid classification or a
	// missing-vs-exists conflict).
	RecReview Recommendation = "review"
)

// Report is the deterministic competitive-audit report.
type Report struct {
	Entries []ReportEntry `json:"entries"`
}

// ReportEntry is one reconciled audit finding.
type ReportEntry struct {
	Competitor           string                   `json:"competitor"`
	Feature              string                   `json:"feature"`
	GapClass             string                   `json:"gap_class"`
	OwnerSurface         string                   `json:"owner_surface"`
	EshuCapability       string                   `json:"eshu_capability,omitempty"`
	CapabilityFound      bool                     `json:"capability_found"`
	CapabilityMaturity   string                   `json:"capability_maturity,omitempty"`
	Recommendation       Recommendation           `json:"recommendation"`
	RecommendationDetail string                   `json:"recommendation_detail"`
	DuplicateIssues      []int                    `json:"duplicate_issues,omitempty"`
	Validation           []auditpreflight.Finding `json:"validation,omitempty"`
	CompetitorFiles      []string                 `json:"competitor_files,omitempty"`
	EvidenceFiles        []string                 `json:"evidence_files,omitempty"`
	Notes                string                   `json:"notes,omitempty"`
}

// Generate reconciles the audit input against the capability catalog and open
// issues, returning a deterministic report sorted by competitor then feature.
// issues may be nil; when present they drive duplicate detection.
func Generate(input AuditInput, catalog capabilitycatalog.Catalog, issues []OpenIssue) Report {
	byID := make(map[string]capabilitycatalog.Entry, len(catalog.Entries))
	for _, entry := range catalog.Entries {
		byID[entry.Capability] = entry
	}

	var entries []ReportEntry
	for _, competitor := range input.Competitors {
		for _, finding := range competitor.Findings {
			entries = append(entries, buildEntry(competitor, finding, byID, issues))
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Competitor != entries[j].Competitor {
			return entries[i].Competitor < entries[j].Competitor
		}
		return entries[i].Feature < entries[j].Feature
	})
	return Report{Entries: entries}
}

func buildEntry(competitor Competitor, finding AuditFinding, byID map[string]capabilitycatalog.Entry, issues []OpenIssue) ReportEntry {
	entry := ReportEntry{
		Competitor:      competitor.Name,
		Feature:         finding.Feature,
		GapClass:        finding.GapClass,
		OwnerSurface:    finding.OwnerSurface,
		EshuCapability:  finding.EshuCapability,
		CompetitorFiles: finding.CompetitorFiles,
		EvidenceFiles:   finding.EvidenceFiles,
		Notes:           finding.Notes,
		Validation:      validateTaxonomy(finding),
	}

	catalogEntry, found := byID[finding.EshuCapability]
	entry.CapabilityFound = finding.EshuCapability != "" && found
	if entry.CapabilityFound {
		entry.CapabilityMaturity = string(catalogEntry.Maturity)
	}
	// A named-but-missing capability is a likely typo; surface it for review
	// rather than silently dropping the catalog reconciliation.
	if finding.EshuCapability != "" && !found {
		entry.Validation = append(entry.Validation, auditpreflight.Finding{
			Kind: auditpreflight.FindingUnknownCapability, Field: "eshu_capability",
			Detail: fmt.Sprintf("capability %q is not in the catalog", finding.EshuCapability),
		})
	}

	entry.DuplicateIssues = dedupeIssues(catalogEntry.LinkedIssues, matchOpenIssues(finding.Feature, issues))
	entry.Recommendation, entry.RecommendationDetail = recommend(finding, entry)
	return entry
}

// validateTaxonomy checks the finding's gap class and owner surface against the
// shared preflight taxonomy.
func validateTaxonomy(finding AuditFinding) []auditpreflight.Finding {
	var findings []auditpreflight.Finding
	if _, ok := auditpreflight.NormalizeGapClass(finding.GapClass); !ok {
		findings = append(findings, auditpreflight.Finding{
			Kind: auditpreflight.FindingInvalidGapClass, Field: "gap_class",
			Detail: fmt.Sprintf("gap class %q is not in the taxonomy", finding.GapClass),
		})
	}
	if _, ok := auditpreflight.NormalizeOwnerSurface(finding.OwnerSurface); !ok {
		findings = append(findings, auditpreflight.Finding{
			Kind: auditpreflight.FindingInvalidOwnerSurface, Field: "owner_surface",
			Detail: fmt.Sprintf("owner surface %q is not in the taxonomy", finding.OwnerSurface),
		})
	}
	return findings
}

// recommend derives the suggested action and a human-readable detail.
func recommend(finding AuditFinding, entry ReportEntry) (Recommendation, string) {
	if len(entry.Validation) > 0 {
		return RecReview, "fix the gap classification before this becomes work"
	}
	gap, _ := auditpreflight.NormalizeGapClass(finding.GapClass)
	hasDup := len(entry.DuplicateIssues) > 0

	switch gap {
	case auditpreflight.GapAlreadyTracked:
		return RecLinkExisting, linkDetail("already tracked", entry.DuplicateIssues)
	case auditpreflight.GapFoundationExists:
		return RecNoIssue, "foundation exists; surface or document the existing capability instead of a new build"
	case auditpreflight.GapDocsStale:
		return RecUpdateExisting, "update the stale docs claim against the capability catalog"
	case auditpreflight.GapMissing:
		if entry.CapabilityFound {
			return RecReview, "marked missing but the capability exists in the catalog; reclassify"
		}
		if hasDup {
			return RecLinkExisting, linkDetail("missing but a similar issue exists", entry.DuplicateIssues)
		}
		return RecCreateNew, "genuinely missing; draft a new child issue with a verification plan"
	default: // ui missing, proof missing, quality gap
		if hasDup {
			return RecLinkExisting, linkDetail(string(gap), entry.DuplicateIssues)
		}
		return RecCreateNew, fmt.Sprintf("%s; draft a new issue scoped to the owner surface", gap)
	}
}

func linkDetail(reason string, issues []int) string {
	if len(issues) == 0 {
		return reason + "; link the existing issue"
	}
	refs := make([]string, len(issues))
	for i, n := range issues {
		refs[i] = fmt.Sprintf("#%d", n)
	}
	return fmt.Sprintf("%s; link %s", reason, strings.Join(refs, ", "))
}

// matchOpenIssues returns issue numbers whose titles share at least two
// significant tokens with the feature. Requiring two tokens avoids the broad
// false positives a single common word (for example "search") would produce; a
// false positive here would silently suppress a genuine gap, so the check is
// deliberately conservative. Features with fewer than two significant tokens are
// not matched and fall through to create_new_issue_draft.
func matchOpenIssues(feature string, issues []OpenIssue) []int {
	tokens := significantTokens(feature)
	if len(tokens) < 2 {
		return nil
	}
	var matched []int
	for _, issue := range issues {
		title := strings.ToLower(issue.Title)
		hits := 0
		for _, token := range tokens {
			if strings.Contains(title, token) {
				hits++
			}
		}
		if hits >= 2 {
			matched = append(matched, issue.Number)
		}
	}
	return matched
}

// significantTokens returns lowercased feature words longer than three runes,
// dropping common filler so duplicate matching stays meaningful.
func significantTokens(feature string) []string {
	stop := map[string]struct{}{"with": {}, "from": {}, "into": {}, "over": {}, "view": {}}
	var tokens []string
	for _, word := range strings.Fields(strings.ToLower(feature)) {
		word = strings.Trim(word, ".,:;()")
		if len(word) <= 3 {
			continue
		}
		if _, ok := stop[word]; ok {
			continue
		}
		tokens = append(tokens, word)
	}
	return tokens
}

func dedupeIssues(a, b []int) []int {
	seen := map[int]struct{}{}
	var out []int
	for _, list := range [][]int{a, b} {
		for _, n := range list {
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out
}
