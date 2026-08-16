// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrun

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/evidredact"
)

// EvidenceIndexingState names whether the first-run proved indexing reached a
// trustworthy state. It is derived from the readiness verdict and the
// first-run completeness label, never invented from process health.
type EvidenceIndexingState string

const (
	// EvidenceIndexingComplete means indexing drained to a queryable, complete
	// state proven by the readiness verdict.
	EvidenceIndexingComplete EvidenceIndexingState = "complete"
	// EvidenceIndexingPartial means indexing is still building or only partially
	// drained; a query cannot yet be trusted.
	EvidenceIndexingPartial EvidenceIndexingState = "partial"
	// EvidenceIndexingStale means an index exists but its freshness is not
	// proven current.
	EvidenceIndexingStale EvidenceIndexingState = "stale"
	// EvidenceIndexingFailed means indexing did not reach a queryable state, or no
	// index was proven at all (verify/runtime failure before indexing).
	EvidenceIndexingFailed EvidenceIndexingState = "failed"
)

// EvidenceOutcome is the truthful top-level outcome of the first-run the report
// describes. It mirrors Result.succeeded(): only a returned bounded
// query counts as succeeded.
type EvidenceOutcome string

const (
	// EvidenceOutcomeSucceeded means the bounded query returned an answer.
	EvidenceOutcomeSucceeded EvidenceOutcome = "succeeded"
	// EvidenceOutcomeIncomplete means the first-run did not reach a returned
	// bounded query.
	EvidenceOutcomeIncomplete EvidenceOutcome = "incomplete"
)

// EvidenceInputs carries optional, redaction-sensitive context that the
// first-run result does not itself record. The MCP endpoint is resolved by the
// caller (from env/config) so the report can show the configured tool transport
// without the evidence builder reaching into process state.
type EvidenceInputs struct {
	// MCPEndpoint is the configured MCP transport URL, if any. It is redacted
	// before it ever enters the report model.
	MCPEndpoint string
	// Profile is the runtime profile label the run was scoped to, if any.
	Profile string
}

// EvidenceReport is the compact, human-readable first-run evidence
// packet. It is a presentation/serialization layer over Result: it
// never recomputes readiness or re-runs queries. Every endpoint and free-form
// field is redacted before it lands here, so the model itself is safe to
// serialize to disk or paste into a support thread.
type EvidenceReport struct {
	// Command identifies the artifact producer.
	Command string `json:"command"`
	// Outcome is the truthful top-level result.
	Outcome EvidenceOutcome `json:"outcome"`
	// RuntimeShape names the runtime topology the run walked.
	RuntimeShape RuntimeShape `json:"runtime_shape"`
	// ServiceEndpoint is the redacted API endpoint the run targeted.
	ServiceEndpoint string `json:"service_endpoint"`
	// MCPEndpoint is the redacted MCP transport endpoint, when configured.
	MCPEndpoint string `json:"mcp_endpoint,omitempty"`
	// IndexingState is the derived complete/partial/stale/failed label.
	IndexingState EvidenceIndexingState `json:"indexing_state"`
	// IndexedRepositories lists the repositories the run observed as indexed.
	IndexedRepositories []string `json:"indexed_repositories,omitempty"`
	// SelectedTarget is the redacted first repository target the run chose.
	SelectedTarget string `json:"selected_target,omitempty"`
	// Readiness is the readiness/queue verdict string from the run.
	Readiness string `json:"readiness"`
	// QueryAnswered reports whether the bounded query returned.
	QueryAnswered bool `json:"query_answered"`
	// QuerySummary is the concise first-query answer summary.
	QuerySummary string `json:"query_summary,omitempty"`
	// Truth is the freshness/completeness truth metadata for the run.
	Truth map[string]any `json:"truth,omitempty"`
	// Diagnosis is the classified onboarding failure (or advisory), when present.
	Diagnosis *Diagnostic `json:"diagnosis,omitempty"`
	// MissingEvidence lists the proofs the run did not collect.
	MissingEvidence []string `json:"missing_evidence,omitempty"`
	// NextCommands lists the recommended follow-up commands.
	NextCommands []string `json:"next_commands,omitempty"`
	// DocsLinks lists repo-relative docs an operator can open for context.
	DocsLinks []string `json:"docs_links,omitempty"`
}

// evidenceDocsLink is the standing docs page that explains how to read a
// first-run evidence artifact. It is always included so a support packet is
// self-describing.
const evidenceDocsLink = "docs/public/reference/first-run-evidence.md"

// BuildEvidence projects a Result into the evidence report. It
// is pure presentation: it reads only fields the first-run already computed and
// redacts every endpoint and target before returning. Inputs may be nil.
func BuildEvidence(result Result, inputs *EvidenceInputs) EvidenceReport {
	if inputs == nil {
		inputs = &EvidenceInputs{}
	}
	// Every raw value the run knows about, so a composed string that interpolated
	// one is rewritten to the same redacted form the matching field carries.
	rawValues := []string{result.ServiceURL, inputs.MCPEndpoint, result.RepoTarget}
	report := EvidenceReport{
		Command:         "first-run-evidence",
		Outcome:         evidenceOutcomeFor(result),
		RuntimeShape:    result.RuntimeShape,
		ServiceEndpoint: redactEndpoint(result.ServiceURL),
		MCPEndpoint:     redactEndpoint(inputs.MCPEndpoint),
		IndexingState:   evidenceIndexingStateFor(result),
		SelectedTarget:  redactPath(result.RepoTarget),
		Readiness:       scrubEvidenceText(strings.TrimSpace(result.Readiness), rawValues),
		QueryAnswered:   result.QueryAnswered,
		QuerySummary:    scrubEvidenceText(strings.TrimSpace(result.QuerySummary), rawValues),
		Truth:           scrubEvidenceTruth(result.Truth, rawValues),
		Diagnosis:       scrubEvidenceDiagnostic(result.Diagnostic, rawValues),
	}
	if target := report.SelectedTarget; target != "" {
		report.IndexedRepositories = evidenceIndexedRepositories(result, target)
	}
	report.MissingEvidence = scrubEvidenceTexts(evidenceMissing(result), rawValues)
	report.NextCommands = scrubEvidenceTexts(evidenceNextCommands(result), rawValues)
	report.DocsLinks = scrubEvidenceTexts(evidenceDocsLinks(result), rawValues)
	return report
}

// evidenceOutcomeFor mirrors the run's truthful success: only a returned bounded
// query is a success.
func evidenceOutcomeFor(result Result) EvidenceOutcome {
	if result.succeeded() {
		return EvidenceOutcomeSucceeded
	}
	return EvidenceOutcomeIncomplete
}

// evidenceIndexingStateFor derives the indexing state from the first-run
// completeness label. It never reports complete unless the run itself proved a
// complete index, and it collapses unknown/empty into failed so a support packet
// never overstates indexing truth.
func evidenceIndexingStateFor(result Result) EvidenceIndexingState {
	switch strings.ToLower(strings.TrimSpace(result.RepoIndexed)) {
	case "complete":
		return EvidenceIndexingComplete
	case "partial":
		return EvidenceIndexingPartial
	case "stale":
		return EvidenceIndexingStale
	default:
		return EvidenceIndexingFailed
	}
}

// evidenceIndexedRepositories reports the indexed repositories the run observed.
// The run records only the selected target, so a complete index lists that
// target; a non-complete index lists nothing because no repository was proven
// queryable.
func evidenceIndexedRepositories(result Result, target string) []string {
	if evidenceIndexingStateFor(result) != EvidenceIndexingComplete {
		return nil
	}
	return []string{target}
}

// evidenceMissing lists the proofs the run did not collect, so an operator and a
// support reader can see exactly what is absent rather than inferring it.
func evidenceMissing(result Result) []string {
	var missing []string
	if state := evidenceIndexingStateFor(result); state != EvidenceIndexingComplete {
		missing = append(missing, "indexing is "+string(state)+", not a complete queryable index")
	}
	if !result.QueryAnswered {
		missing = append(missing, "no bounded query answer was returned")
	} else if isEmptyRepositoriesAnswer(result.QuerySummary) {
		missing = append(missing, "the query returned zero repositories; nothing is indexed to query")
	}
	return missing
}

// evidenceNextCommands reuses the run's next steps and any classified recovery
// steps so the report's actionable guidance matches what first-run already
// computed. Recovery steps lead because they target the specific failure.
func evidenceNextCommands(result Result) []string {
	var commands []string
	if result.Diagnostic != nil {
		commands = append(commands, result.Diagnostic.RecoverySteps...)
	}
	commands = append(commands, result.NextSteps...)
	return dedupeStrings(commands)
}

// evidenceDocsLinks collects the standing evidence docs page plus any docs link
// the classified diagnostic attached, deduplicated and order-stable.
func evidenceDocsLinks(result Result) []string {
	links := []string{evidenceDocsLink}
	if result.Diagnostic != nil {
		if link := strings.TrimSpace(result.Diagnostic.DocsLink); link != "" {
			links = append(links, link)
		}
	}
	return dedupeStrings(links)
}

// dedupeStrings returns the input with empty and duplicate values removed while
// preserving first-seen order.
func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// evidenceRedactedMarker is the placeholder substituted for a removed
// credential inside an endpoint URL. The rule lives in internal/cli/evidredact;
// this alias is what the tests in this package and the endpoint callers here
// read.
const evidenceRedactedMarker = evidredact.Marker

// The redaction rules below moved to internal/cli/evidredact so they can be
// unit-tested on their own and so this file stays under the repo's 500-line
// cap. The gap that made the move urgent survived precisely because nothing
// exercised the scrub in isolation: every test reached it through
// `first-run report`, and a credential carried in URL-free text was never one
// of the inputs anybody wrote a case for.
//
// The wrappers stay because the boundary corpus in internal/urlredact is
// driven through redactEndpoint by this package's own differential test, so a
// drift between what this package calls and what evidredact does goes red
// here. go/cmd/eshu/hosted_onboard.go redacts a hosted endpoint through its
// own equivalent one-line wrapper over evidredact.Endpoint, kept there because
// package main cannot be imported and the hosted family has not extracted yet.

// redactEndpoint returns a display-safe form of an endpoint URL: userinfo, any
// credential-named query value, and the whole fragment are removed. See
// evidredact.Endpoint for the rule and the three carriers it closes.
func redactEndpoint(raw string) string { return evidredact.Endpoint(raw) }

// redactPath returns a display-safe form of a filesystem path target, keeping
// only its final element. See evidredact.Path.
func redactPath(raw string) string { return evidredact.Path(raw) }

// scrubEvidenceText makes a composed, free-form string safe for an operator
// artifact: embedded URLs go through redactEndpoint, and the text between them
// is scanned for bare credential pairs and for the known raw values the report
// already redacts into structured fields. See evidredact.Text for the three
// carriers and the limits.
func scrubEvidenceText(text string, rawValues []string) string {
	return evidredact.Text(text, rawValues)
}

// scrubEvidenceTexts applies scrubEvidenceText to every element of a slice,
// returning a new slice so the caller's data is never mutated in place.
func scrubEvidenceTexts(values []string, rawValues []string) []string {
	return evidredact.Texts(values, rawValues)
}

// scrubEvidenceDiagnostic returns a redacted copy of the diagnostic for the
// report. It copies rather than mutating because the diagnostic is shared with
// the caller's Result, which stays raw so the run's own error reporting
// keeps its full detail.
//
// The preserved underlying error is replaced by a scrubbed error value: the
// report renders it as the "cause" line, so leaving the original chain in place
// would put the raw endpoint straight into the artifact.
func scrubEvidenceDiagnostic(d *Diagnostic, rawValues []string) *Diagnostic {
	if d == nil {
		return nil
	}
	scrubbed := *d
	scrubbed.Summary = scrubEvidenceText(d.Summary, rawValues)
	scrubbed.RecoverySteps = scrubEvidenceTexts(d.RecoverySteps, rawValues)
	scrubbed.DocsLink = scrubEvidenceText(d.DocsLink, rawValues)
	if cause := d.rootCause(); cause != "" {
		scrubbed.Underlying = errors.New(scrubEvidenceText(cause, rawValues))
	}
	return &scrubbed
}

// scrubEvidenceTruth returns a copy of the truth metadata with every reachable
// string scrubbed, at any depth. firstRunResultFromEnvelope decodes an
// operator-supplied envelope into map[string]any, so the nesting is whatever
// that JSON carried. See evidredact.Truth.
func scrubEvidenceTruth(truth map[string]any, rawValues []string) map[string]any {
	return evidredact.Truth(truth, rawValues)
}
