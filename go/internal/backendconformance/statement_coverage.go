// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// Statement coverage (issue #6783) joins the builder manifest against one
// backend's differential recordings: every inventoried builder must have
// executed, and every recorded read must have returned rows at least once,
// or the gate fails. Builders whose text is statically unknowable carry a
// manifest exemption with a reason; exemption excuses execution proof,
// never drift (the manifest validator still pins their text and digest).
const (
	// CoverageNeverExecuted fails an inventoried, unexempted builder with
	// no matching recording on the backend.
	CoverageNeverExecuted = "never-executed"
	// CoverageAlwaysEmptyRead fails a recorded read whose successful
	// executions all returned zero rows, unless exempted.
	CoverageAlwaysEmptyRead = "always-empty-read"
)

// StatementCoverageFailure is one gate failure: a backend, a rule kind,
// and the manifest key (file:symbol) or statement text that failed it.
type StatementCoverageFailure struct {
	Backend string
	Kind    string
	ID      string
}

// BackendStatementCoverage is one backend's coverage detail. Executed and
// NeverExecuted hold manifest keys; AlwaysEmptyReads and Unattributed hold
// statement texts. WritesWithoutCounters names executed builders whose
// executions never carried Bolt counters: advisory only, since a MERGE
// that matched-existing legitimately reports zeros — but a backend that
// never reports any counter anywhere is a counter-fidelity signal.
// DispatchMisses names same-parameter sibling member texts
// (labelDispatchFamilies) that never returned rows while their family did:
// advisory only, since a label tried before the owning label of a dispatch,
// or a non-owning label of a per-label fan-out, misses by construction, but
// it shows which anchor labels the corpus never positively exercised.
type BackendStatementCoverage struct {
	Backend               string
	Executed              []string
	NeverExecuted         []string
	Exempted              []string
	AlwaysEmptyReads      []string
	FailedReads           []string
	DispatchMisses        []string
	Unattributed          []string
	WritesWithoutCounters []string
}

// StatementCoverageReport is the per-backend coverage detail. Failures
// carries the gate verdict; everything else is the report the issue
// requires (executed, never-executed, and always-empty per backend).
type StatementCoverageReport struct {
	ByBackend map[string]BackendStatementCoverage
}

// Failures returns the gate verdict, sorted for deterministic output:
// unexempted never-executed builders and unexempted always-empty reads,
// per backend.
func (r StatementCoverageReport) Failures() []StatementCoverageFailure {
	out := make([]StatementCoverageFailure, 0)
	for backend, coverage := range r.ByBackend {
		for _, key := range coverage.NeverExecuted {
			out = append(out, StatementCoverageFailure{Backend: backend, Kind: CoverageNeverExecuted, ID: key})
		}
		for _, text := range coverage.AlwaysEmptyReads {
			out = append(out, StatementCoverageFailure{Backend: backend, Kind: CoverageAlwaysEmptyRead, ID: text})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Backend != out[j].Backend {
			return out[i].Backend < out[j].Backend
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ComputeStatementCoverage joins manifest builders against each backend's
// recordings. A builder matches the backend when any of its variants
// matches any recording's statement text: templates by equality,
// fragments by ordered containment. Records match even when failed:
// presence proves the statement ran; persistent failure is slice-3's
// failures-kind signal, not a coverage gap.
func ComputeStatementCoverage(manifest queryplan.BuilderManifest, recordsByBackend map[string][]DifferentialRecord) StatementCoverageReport {
	report := StatementCoverageReport{ByBackend: make(map[string]BackendStatementCoverage, len(recordsByBackend))}
	exemptions := make(map[string]string, len(manifest.ReadExemptions))
	for _, exemption := range manifest.ReadExemptions {
		exemptions[normalizeCoverageText(exemption.Statement)] = exemption.Reason
	}
	for backend, records := range recordsByBackend {
		coverage := BackendStatementCoverage{Backend: backend}
		matched := make([]bool, len(records))
		for _, file := range manifest.Builders {
			for _, builder := range file.Builders {
				key := file.File + ":" + builder.Symbol
				if builder.Exempt != "" {
					coverage.Exempted = append(coverage.Exempted, key)
					continue
				}
				executions, withCounters := matchBuilder(builder, records, matched)
				if executions == 0 {
					coverage.NeverExecuted = append(coverage.NeverExecuted, key)
					continue
				}
				coverage.Executed = append(coverage.Executed, key)
				if withCounters == 0 {
					coverage.WritesWithoutCounters = append(coverage.WritesWithoutCounters, key)
				}
			}
		}
		coverage.AlwaysEmptyReads, coverage.FailedReads, coverage.DispatchMisses = emptyReads(records, exemptions)
		coverage.Unattributed = unattributedTexts(records, matched)
		sort.Strings(coverage.Executed)
		sort.Strings(coverage.NeverExecuted)
		sort.Strings(coverage.Exempted)
		sort.Strings(coverage.AlwaysEmptyReads)
		sort.Strings(coverage.FailedReads)
		sort.Strings(coverage.DispatchMisses)
		sort.Strings(coverage.Unattributed)
		sort.Strings(coverage.WritesWithoutCounters)
		report.ByBackend[backend] = coverage
	}
	return report
}

// matchBuilder counts a builder's executions on the backend and how many
// carried Bolt counters, marking every matched record so the unattributed
// pass can tell leftovers apart. A record matches when any variant
// matches its statement text.
func matchBuilder(builder queryplan.StatementBuilder, records []DifferentialRecord, matched []bool) (executions, withCounters int) {
	for i, record := range records {
		text := record.Fingerprint.Statement
		for _, variant := range builder.Variants {
			if !variantMatches(variant, text) {
				continue
			}
			matched[i] = true
			executions++
			if record.Counters != (sourcecypher.WriteCounters{}) {
				withCounters++
			}
			break
		}
	}
	return executions, withCounters
}

// minCoverageAnchorLength is the shortest fragment run that can anchor a
// fragment-set match. Glue runs (")", "AS", "AND") appear in nearly every
// statement, so a set of only short runs would match siblings and prove
// the wrong builder executed. A set needs one anchor run at least this
// long; shorter-only sets match nothing (fail closed: the entry reports
// never-executed while its executions, if any, surface as unattributed,
// telling the author to strengthen the rule or exempt with reason).
const minCoverageAnchorLength = 16

// variantMatches reports whether a recording's statement text came from
// the variant: template equality, or every fragment contained in order
// with at least one anchor run. A variant with neither matches nothing.
func variantMatches(variant queryplan.StatementVariant, text string) bool {
	if variant.Template != "" {
		return text == variant.Template
	}
	anchored := false
	for _, fragment := range variant.Fragments {
		if len(fragment) >= minCoverageAnchorLength {
			anchored = true
			break
		}
	}
	if !anchored {
		return false
	}
	rest := text
	for _, fragment := range variant.Fragments {
		index := strings.Index(rest, fragment)
		if index < 0 {
			return false
		}
		rest = rest[index+len(fragment):]
	}
	return true
}

// emptyReads groups successful read executions by read: its statement
// text, or for a proven same-parameter sibling member
// (labelDispatchFamilies: a label dispatch or a per-label fan-out) its
// family's unlabeled text, so one logical read split across per-label
// statements is judged once. A read whose executions all returned zero
// rows is always-empty (failing unless exempted); a text with only failed
// executions is failed-only (reported, never failing: transients must not
// red the gate, and slice-3's failures kind owns persistent failure). A
// family member that never returned rows while its family did is a
// dispatch miss (reported, never failing: a non-owning label misses by
// construction). Writes (no digest) never qualify.
func emptyReads(records []DifferentialRecord, exemptions map[string]string) (alwaysEmpty, failedOnly, dispatchMisses []string) {
	type readStats struct {
		succeeded bool
		maxRows   int
	}
	stats := make(map[string]*readStats)
	for _, record := range records {
		if record.Digest == "" {
			continue
		}
		text := normalizeCoverageText(record.Fingerprint.Statement)
		entry, ok := stats[text]
		if !ok {
			entry = &readStats{}
			stats[text] = entry
		}
		if record.Failed {
			continue
		}
		entry.succeeded = true
		if record.RowCount > entry.maxRows {
			entry.maxRows = record.RowCount
		}
	}
	families := labelDispatchFamilies(records)
	membersByRead := make(map[string][]string)
	for text, entry := range stats {
		if !entry.succeeded {
			failedOnly = append(failedOnly, text)
			continue
		}
		read := text
		if family, ok := families[text]; ok {
			read = family
		}
		membersByRead[read] = append(membersByRead[read], text)
	}
	for read, members := range membersByRead {
		maxRows := 0
		for _, member := range members {
			maxRows = max(maxRows, stats[member].maxRows)
		}
		if maxRows == 0 {
			if !readFamilyExempt(read, members, exemptions) {
				alwaysEmpty = append(alwaysEmpty, read)
			}
			continue
		}
		for _, member := range members {
			if stats[member].maxRows == 0 {
				dispatchMisses = append(dispatchMisses, member)
			}
		}
	}
	return alwaysEmpty, failedOnly, dispatchMisses
}

// unattributedTexts lists recorded statement texts no manifest variant
// matched: forwarding-adapter executions, derived probe texts, and future
// drift surface here. Presence proves execution, so this never fails —
// but every entry deserves a manifest rule or an exemption reason.
func unattributedTexts(records []DifferentialRecord, matched []bool) []string {
	seen := make(map[string]struct{})
	var out []string
	for i, record := range records {
		if matched[i] {
			continue
		}
		text := normalizeCoverageText(record.Fingerprint.Statement)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
}

// normalizeCoverageText collapses whitespace runs so manifest exemption
// texts compare equal to fingerprinted recording texts, which normalize
// the same way.
func normalizeCoverageText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
