// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import (
	"fmt"
	"io"
	"strings"
)

// RenderServiceError writes the operator-facing rendering of an ambiguous
// selector: the disambiguation hint, then one line per candidate service.
//
// It writes nothing for any other error code. go/cmd/eshu renders every other
// failure from the envelope's message alone, so a code this function does not
// recognize must leave the writer untouched rather than emit a bare header.
//
// Candidates are read from error.details.candidates first and from
// data.candidates second, because the API has returned them under both.
//
//nolint:wrapcheck // Every error returned here is the caller's io.Writer failing mid-line; wrapping changes text the operator reads directly.
func RenderServiceError(w io.Writer, envelope ServiceEnvelope) error {
	if envelope.Error == nil || envelope.Error.Code != "ambiguous" {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Service selector is ambiguous. Add --service-id, --repo, or --env."); err != nil {
		return err
	}
	candidates := sliceValue(envelope.Error.Details, "candidates")
	if len(candidates) == 0 {
		candidates = sliceValue(envelope.Data, "candidates")
	}
	for _, candidate := range candidates {
		row, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		serviceID := firstString(stringValue(row, "service_id"), stringValue(row, "id"))
		serviceName := firstString(stringValue(row, "service_name"), stringValue(row, "name"))
		repoID := stringValue(row, "repo_id")
		environment := stringValue(row, "environment")
		if _, err := fmt.Fprintf(
			w,
			"- %s",
			firstString(serviceID, serviceName, "<unknown>"),
		); err != nil {
			return err
		}
		if serviceName != "" && serviceName != serviceID {
			if _, err := fmt.Fprintf(w, " name=%s", serviceName); err != nil {
				return err
			}
		}
		if repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if environment != "" {
			if _, err := fmt.Fprintf(w, " env=%s", environment); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// RenderServiceSummary writes the human-readable service trace: identity, truth
// freshness, the code-to-runtime segments, the four evidence counts, coverage,
// and the limitations the API attached.
//
// Identity fields fall back across key spellings the API has used, and a field
// that resolves to nothing is omitted rather than printed empty -- except the
// service and repository lines, which print "<unknown>" so the operator can see
// which selector produced nothing.
//
//nolint:wrapcheck // Every error returned here is the caller's io.Writer failing mid-line; wrapping changes text the operator reads directly.
func RenderServiceSummary(w io.Writer, envelope ServiceEnvelope) error {
	data := envelope.Data
	identity := mapValue(data, "service_identity")
	serviceName := firstString(
		stringValue(identity, "service_name"),
		stringValue(identity, "name"),
		stringValue(data, "service_name"),
		stringValue(identity, "service_id"),
	)
	repoID := stringValue(identity, "repo_id")
	repoName := stringValue(identity, "repo_name")
	if repoName == "" {
		repoName = "<unknown>"
	}
	coverage := mapValue(mapValue(data, "investigation"), "coverage_summary")
	coverageState := firstString(stringValue(coverage, "state"), "unknown")
	coverageReason := stringValue(coverage, "reason")
	limitations := stringsValue(identity["limitations"])
	if len(limitations) == 0 {
		limitations = stringsValue(data["limitations"])
	}

	if _, err := fmt.Fprintf(w, "Service: %s\n", firstString(serviceName, "<unknown>")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Repository: %s (%s)\n", firstString(repoID, "<unknown>"), repoName); err != nil {
		return err
	}
	if status := stringValue(identity, "materialization_status"); status != "" {
		if _, err := fmt.Fprintf(w, "Materialization: %s\n", status); err != nil {
			return err
		}
	}
	if basis := stringValue(identity, "query_basis"); basis != "" {
		if _, err := fmt.Fprintf(w, "Basis: %s\n", basis); err != nil {
			return err
		}
	}
	if err := renderTruthFreshness(w, envelope); err != nil {
		return err
	}
	if err := renderCodeToRuntime(w, data); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Deployment lanes: %d\n", len(sliceValue(data, "deployment_lanes"))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Runtime instances: %d\n", runtimeInstanceCount(data)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Upstream dependencies: %d\n", len(sliceValue(data, "upstream_dependencies"))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Downstream consumers: %d\n", downstreamConsumerCount(data)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Coverage: %s\n", coverageState); err != nil {
		return err
	}
	if coverageReason != "" {
		if _, err := fmt.Fprintf(w, "Coverage reason: %s\n", coverageReason); err != nil {
			return err
		}
	}
	if len(limitations) > 0 {
		if _, err := fmt.Fprintln(w, "What to worry about:"); err != nil {
			return err
		}
		for _, limitation := range limitations {
			if _, err := fmt.Fprintf(w, "- %s\n", limitation); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderTruthFreshness writes the truth freshness state and, when present, the
// detail behind it. It writes nothing when the envelope carried no state, so a
// response without a truth block renders no freshness lines at all.
//
//nolint:wrapcheck // Same reason as RenderServiceSummary: the operator reads these bytes directly.
func renderTruthFreshness(w io.Writer, envelope ServiceEnvelope) error {
	freshness := mapValue(envelope.Truth, "freshness")
	state := stringValue(freshness, "state")
	if state == "" {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", state); err != nil {
		return err
	}
	if detail := firstString(stringValue(freshness, "detail"), stringValue(envelope.Truth, "reason")); detail != "" {
		if _, err := fmt.Fprintf(w, "Freshness detail: %s\n", detail); err != nil {
			return err
		}
	}
	return nil
}

// renderCodeToRuntime writes the code-to-runtime segment list: the trace status,
// one line per named segment with its evidence count and basis, and the segments
// the API reported as missing.
//
// A trace with no segments renders nothing at all, header included: the section
// exists to show the path from source to runtime, and an empty one tells the
// operator less than its absence does.
//
//nolint:wrapcheck // Same reason as RenderServiceSummary: the operator reads these bytes directly.
func renderCodeToRuntime(w io.Writer, data map[string]any) error {
	trace := mapValue(data, "code_to_runtime_trace")
	if len(trace) == 0 {
		return nil
	}
	segments := sliceValue(trace, "segments")
	if len(segments) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Code to runtime:"); err != nil {
		return err
	}
	if status := stringValue(trace, "status"); status != "" {
		if _, err := fmt.Fprintf(w, "Trace status: %s\n", status); err != nil {
			return err
		}
	}
	for _, item := range segments {
		segment, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(segment, "name")
		status := stringValue(segment, "status")
		if name == "" || status == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s: %s", name, status); err != nil {
			return err
		}
		if count := intValue(segment, "evidence_count"); count > 0 {
			if _, err := fmt.Fprintf(w, " (%d evidence)", count); err != nil {
				return err
			}
		}
		if basis := stringValue(segment, "basis"); basis != "" {
			if _, err := fmt.Fprintf(w, " via %s", basis); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if missing := stringsValue(trace["missing_segments"]); len(missing) > 0 {
		if _, err := fmt.Fprintf(w, "Missing evidence: %s\n", strings.Join(missing, ", ")); err != nil {
			return err
		}
	}
	return nil
}

// runtimeInstanceCount counts the runtime instances behind a service, preferring
// the top-level data.runtime_instances list and falling back to the instances
// nested under service_identity, which is where an older API shape put them.
func runtimeInstanceCount(data map[string]any) int {
	if instances := sliceValue(data, "runtime_instances"); len(instances) > 0 {
		return len(instances)
	}
	return len(sliceValue(mapValue(data, "service_identity"), "instances"))
}

// downstreamConsumerCount counts what depends on a service. downstream_consumers
// arrives as a list in some responses and as an object in others; for the object
// form the two counted fields are summed, and their absence falls back to the
// length of its items list rather than reporting zero consumers.
func downstreamConsumerCount(data map[string]any) int {
	downstream := data["downstream_consumers"]
	switch typed := downstream.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	case map[string]any:
		total := intValue(typed, "graph_dependent_count") + intValue(typed, "content_consumer_count")
		if total > 0 {
			return total
		}
		return len(sliceValue(typed, "items"))
	default:
		return 0
	}
}
