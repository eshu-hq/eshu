// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/mcpsetup"
)

// evidenceIndexingState names whether the first-run proved indexing reached a
// trustworthy state. It is derived from the readiness verdict and the
// first-run completeness label, never invented from process health.
type evidenceIndexingState string

const (
	// evidenceIndexingComplete means indexing drained to a queryable, complete
	// state proven by the readiness verdict.
	evidenceIndexingComplete evidenceIndexingState = "complete"
	// evidenceIndexingPartial means indexing is still building or only partially
	// drained; a query cannot yet be trusted.
	evidenceIndexingPartial evidenceIndexingState = "partial"
	// evidenceIndexingStale means an index exists but its freshness is not
	// proven current.
	evidenceIndexingStale evidenceIndexingState = "stale"
	// evidenceIndexingFailed means indexing did not reach a queryable state, or no
	// index was proven at all (verify/runtime failure before indexing).
	evidenceIndexingFailed evidenceIndexingState = "failed"
)

// evidenceOutcome is the truthful top-level outcome of the first-run the report
// describes. It mirrors firstRunResult.succeeded(): only a returned bounded
// query counts as succeeded.
type evidenceOutcome string

const (
	// evidenceOutcomeSucceeded means the bounded query returned an answer.
	evidenceOutcomeSucceeded evidenceOutcome = "succeeded"
	// evidenceOutcomeIncomplete means the first-run did not reach a returned
	// bounded query.
	evidenceOutcomeIncomplete evidenceOutcome = "incomplete"
)

// firstRunEvidenceInputs carries optional, redaction-sensitive context that the
// first-run result does not itself record. The MCP endpoint is resolved by the
// caller (from env/config) so the report can show the configured tool transport
// without the evidence builder reaching into process state.
type firstRunEvidenceInputs struct {
	// MCPEndpoint is the configured MCP transport URL, if any. It is redacted
	// before it ever enters the report model.
	MCPEndpoint string
	// Profile is the runtime profile label the run was scoped to, if any.
	Profile string
}

// firstRunEvidenceReport is the compact, human-readable first-run evidence
// packet. It is a presentation/serialization layer over firstRunResult: it
// never recomputes readiness or re-runs queries. Every endpoint and free-form
// field is redacted before it lands here, so the model itself is safe to
// serialize to disk or paste into a support thread.
type firstRunEvidenceReport struct {
	// Command identifies the artifact producer.
	Command string `json:"command"`
	// Outcome is the truthful top-level result.
	Outcome evidenceOutcome `json:"outcome"`
	// RuntimeShape names the runtime topology the run walked.
	RuntimeShape firstRunRuntimeShape `json:"runtime_shape"`
	// ServiceEndpoint is the redacted API endpoint the run targeted.
	ServiceEndpoint string `json:"service_endpoint"`
	// MCPEndpoint is the redacted MCP transport endpoint, when configured.
	MCPEndpoint string `json:"mcp_endpoint,omitempty"`
	// IndexingState is the derived complete/partial/stale/failed label.
	IndexingState evidenceIndexingState `json:"indexing_state"`
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
	Diagnosis *onboardingDiagnostic `json:"diagnosis,omitempty"`
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

// buildFirstRunEvidence projects a firstRunResult into the evidence report. It
// is pure presentation: it reads only fields the first-run already computed and
// redacts every endpoint and target before returning. Inputs may be nil.
func buildFirstRunEvidence(result firstRunResult, inputs *firstRunEvidenceInputs) firstRunEvidenceReport {
	if inputs == nil {
		inputs = &firstRunEvidenceInputs{}
	}
	// Every raw value the run knows about, so a composed string that interpolated
	// one is rewritten to the same redacted form the matching field carries.
	rawValues := []string{result.ServiceURL, inputs.MCPEndpoint, result.RepoTarget}
	report := firstRunEvidenceReport{
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
func evidenceOutcomeFor(result firstRunResult) evidenceOutcome {
	if result.succeeded() {
		return evidenceOutcomeSucceeded
	}
	return evidenceOutcomeIncomplete
}

// evidenceIndexingStateFor derives the indexing state from the first-run
// completeness label. It never reports complete unless the run itself proved a
// complete index, and it collapses unknown/empty into failed so a support packet
// never overstates indexing truth.
func evidenceIndexingStateFor(result firstRunResult) evidenceIndexingState {
	switch strings.ToLower(strings.TrimSpace(result.RepoIndexed)) {
	case "complete":
		return evidenceIndexingComplete
	case "partial":
		return evidenceIndexingPartial
	case "stale":
		return evidenceIndexingStale
	default:
		return evidenceIndexingFailed
	}
}

// evidenceIndexedRepositories reports the indexed repositories the run observed.
// The run records only the selected target, so a complete index lists that
// target; a non-complete index lists nothing because no repository was proven
// queryable.
func evidenceIndexedRepositories(result firstRunResult, target string) []string {
	if evidenceIndexingStateFor(result) != evidenceIndexingComplete {
		return nil
	}
	return []string{target}
}

// evidenceMissing lists the proofs the run did not collect, so an operator and a
// support reader can see exactly what is absent rather than inferring it.
func evidenceMissing(result firstRunResult) []string {
	var missing []string
	if state := evidenceIndexingStateFor(result); state != evidenceIndexingComplete {
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
func evidenceNextCommands(result firstRunResult) []string {
	var commands []string
	if result.Diagnostic != nil {
		commands = append(commands, result.Diagnostic.RecoverySteps...)
	}
	commands = append(commands, result.NextSteps...)
	return dedupeStrings(commands)
}

// evidenceDocsLinks collects the standing evidence docs page plus any docs link
// the classified diagnostic attached, deduplicated and order-stable.
func evidenceDocsLinks(result firstRunResult) []string {
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

// redactEndpoint returns a display-safe form of an endpoint URL. Any embedded
// userinfo (user:password@) is stripped because it can carry a token or
// password; the scheme, host, and path remain so the operator can still
// recognize the target. A value that does not parse as a URL is masked through
// mcpsetup.RedactToken so a credential-looking string never survives verbatim.
func redactEndpoint(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return mcpsetup.RedactToken(trimmed)
	}
	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}
	return parsed.String()
}

// redactPath returns a display-safe form of a filesystem path target. Absolute
// host paths can leak a username or private layout, so only the final path
// element is kept with a leading ellipsis. Relative paths and bare names are
// returned unchanged because they carry no host-specific secret.
func redactPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	base := trimmed[strings.LastIndex(trimmed, "/")+1:]
	if base == "" {
		return ".../"
	}
	return ".../" + base
}

// evidenceEmbeddedURLPattern matches an absolute URL embedded in free-form text.
// It stops at whitespace and at the quoting characters the evidence renderers
// use, so a URL at the end of a sentence or inside a Markdown code span is
// matched without swallowing the surrounding prose.
var evidenceEmbeddedURLPattern = regexp.MustCompile("[a-zA-Z][a-zA-Z0-9+.\\-]*://[^\\s<>\"'`]+")

// evidenceURLTrailingPunctuation are characters that commonly follow a URL in a
// sentence but are not part of it. They are trimmed before redaction and
// restored afterwards so "reachable at http://host:1/x." keeps its full stop.
const evidenceURLTrailingPunctuation = ".,;:!?)]}"

// evidenceMinReplaceableLen is the shortest non-URL raw value that stage one
// will substitute inside free-form text. Below this a path is both too short to
// be worth redacting and long enough to collide with unrelated substrings.
const evidenceMinReplaceableLen = 8

// scrubEvidenceText makes a composed, free-form string safe for an operator
// artifact.
//
// A name-keyed redactor is correct only until a value is composed from a raw
// one: the endpoint fields are redacted while a summary, hint, cause, or
// next-command built by interpolating the same endpoint is not. This helper
// closes that gap in two stages.
//
// Stage one replaces each known raw value with the same redacted form the
// corresponding report field carries, so a composed string and the field it was
// built from never disagree. Stage two strips the userinfo from any absolute URL
// still present, which catches endpoints this call site does not know about —
// for example one wrapped into a transport error, or one restored from a saved
// envelope where the original inputs are no longer available.
//
// Stage two is what makes the guard survive a new renderer or a new composed
// string: text reaching the report is cleaned on the way in, not at each
// rendering surface.
func scrubEvidenceText(text string, rawValues []string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	scrubbed := text
	for _, raw := range rawValues {
		raw = strings.TrimSpace(raw)
		if raw == "" || !strings.Contains(scrubbed, raw) {
			continue
		}
		redacted := redactEvidenceValue(raw)
		if redacted == raw {
			continue
		}
		// A short path substring would rewrite unrelated text (replacing "/a"
		// would corrupt "/api/v0"), so only substitute values distinctive enough
		// to identify. URLs are always distinctive; short paths carry nothing
		// worth redacting, and stage two still covers any URL inside them.
		if !strings.Contains(raw, "://") && len(raw) < evidenceMinReplaceableLen {
			continue
		}
		scrubbed = strings.ReplaceAll(scrubbed, raw, redacted)
	}
	return evidenceEmbeddedURLPattern.ReplaceAllStringFunc(scrubbed, redactEmbeddedURL)
}

// redactEvidenceValue picks the right redaction for a known raw value: URL-like
// values keep their scheme, host, and path with any userinfo removed, while
// filesystem targets collapse to their final element.
func redactEvidenceValue(raw string) string {
	if strings.Contains(raw, "://") {
		return redactEndpoint(raw)
	}
	return redactPath(raw)
}

// redactEmbeddedURL redacts a single URL matched inside free-form text,
// preserving any trailing sentence punctuation the match absorbed.
func redactEmbeddedURL(match string) string {
	trimmed := strings.TrimRight(match, evidenceURLTrailingPunctuation)
	suffix := match[len(trimmed):]
	if trimmed == "" {
		return match
	}
	return redactEndpoint(trimmed) + suffix
}

// scrubEvidenceTexts applies scrubEvidenceText to every element of a slice,
// returning a new slice so the caller's data is never mutated in place.
func scrubEvidenceTexts(values []string, rawValues []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, scrubEvidenceText(v, rawValues))
	}
	return out
}

// scrubEvidenceDiagnostic returns a redacted copy of the diagnostic for the
// report. It copies rather than mutating because the diagnostic is shared with
// the caller's firstRunResult, which stays raw so the run's own error reporting
// keeps its full detail.
//
// The preserved underlying error is replaced by a scrubbed error value: the
// report renders it as the "cause" line, so leaving the original chain in place
// would put the raw endpoint straight into the artifact.
func scrubEvidenceDiagnostic(d *onboardingDiagnostic, rawValues []string) *onboardingDiagnostic {
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

// scrubEvidenceTruth returns a copy of the truth metadata with every string
// value scrubbed. The truth vocabulary is a bounded label set today, so this is
// a guard against a future label that carries an endpoint rather than a fix for
// an observed leak.
func scrubEvidenceTruth(truth map[string]any, rawValues []string) map[string]any {
	if truth == nil {
		return nil
	}
	out := make(map[string]any, len(truth))
	for k, v := range truth {
		if s, ok := v.(string); ok {
			out[k] = scrubEvidenceText(s, rawValues)
			continue
		}
		out[k] = v
	}
	return out
}
