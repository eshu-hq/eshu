// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"io"
	"net/url"
)

// ChangedSinceRoute is the API route `eshu freshness changed-since` reads.
const ChangedSinceRoute = "/api/v0/freshness/changed-since"

// ChangedSinceOptions is the selector set for `eshu freshness changed-since`,
// holding flag values exactly as cobra parsed them. String selectors are
// trimmed when the request path is built.
type ChangedSinceOptions struct {
	// JSON writes the canonical envelope instead of the human summary.
	JSON bool
	// ScopeID names the ingestion scope to diff.
	ScopeID string
	// Repository names the scope by canonical repository id instead.
	Repository string
	// SinceGenerationID is the prior generation to diff from.
	SinceGenerationID string
	// SinceObservedAt is an RFC3339 instant; the server picks the generation
	// observed at or before it. The CLI does not parse or validate the value,
	// so a malformed instant is the server's error to report.
	SinceObservedAt string
	// SampleLimit caps sample handles per classification per category. Values
	// of zero or less are omitted, leaving the server's default in charge.
	SampleLimit int
}

// ChangedSincePath builds the request path for opts.
func ChangedSincePath(opts ChangedSinceOptions) string {
	query := url.Values{}
	setSelector(query, "scope_id", opts.ScopeID)
	setSelector(query, "repository", opts.Repository)
	setSelector(query, "since_generation_id", opts.SinceGenerationID)
	setSelector(query, "since_observed_at", opts.SinceObservedAt)
	setLimit(query, "sample_limit", opts.SampleLimit)
	return joinPath(ChangedSinceRoute, query)
}

// FetchChangedSince reads the changed-since envelope for opts.
func FetchChangedSince(client EnvelopeFetcher, opts ChangedSinceOptions) (Envelope, error) {
	return fetch(client, ChangedSincePath(opts))
}

// RunChangedSince fetches the changed-since envelope, writes either the
// canonical JSON or the human summary to w, and returns a *Failure when the
// command must exit non-zero.
func RunChangedSince(w io.Writer, client EnvelopeFetcher, opts ChangedSinceOptions) error {
	env, err := FetchChangedSince(client, opts)
	if err != nil {
		env = transportEnvelope(err)
	}
	return finish(w, opts.JSON, env, RenderChangedSinceSummary, failureOf(env))
}

// RenderChangedSinceSummary writes the human view of a scope changed-since
// envelope: the truth freshness state, the baseline-to-current generation
// line, then one line per category. A scope with no current active generation
// renders an explicit "diff unavailable" notice instead of zeroed counts, so
// an operator is never shown a confident empty delta.
func RenderChangedSinceSummary(w io.Writer, env Envelope) error {
	if err := renderFreshnessLine(w, env); err != nil {
		return err
	}
	data := env.Data
	if err := writef(
		w,
		"Changed since %s -> %s (scope=%s)\n",
		ChangedSinceBaselineLabel(data),
		stringValue(data, "current_active_generation_id"),
		stringValue(data, "scope_id"),
	); err != nil {
		return err
	}
	if boolValue(data, "unavailable") {
		return writef(w, "  diff unavailable: scope has no current active generation\n")
	}
	return renderCategories(w, data)
}

// renderCategories writes one line per category delta, skipping any element
// that is not a JSON object.
func renderCategories(w io.Writer, data map[string]any) error {
	for _, raw := range sliceValue(data, "categories") {
		category, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := renderCategory(w, category); err != nil {
			return err
		}
	}
	return nil
}

// renderCategory writes one category delta line. A category the server could
// not compute renders as "unavailable" rather than as five zeroes.
func renderCategory(w io.Writer, category map[string]any) error {
	name := stringValue(category, "category")
	if boolValue(category, "unavailable") {
		return writef(w, "  %-16s unavailable\n", name)
	}
	counts := mapValue(category, "counts")
	return writef(
		w,
		"  %-16s added=%d updated=%d unchanged=%d retired=%d superseded=%d\n",
		name,
		intValue(counts, "added"),
		intValue(counts, "updated"),
		intValue(counts, "unchanged"),
		intValue(counts, "retired"),
		intValue(counts, "superseded"),
	)
}

// ChangedSinceBaselineLabel names what the diff is measured from: the prior
// generation id when the server echoed one, else the observed-at instant, else
// a literal "(unknown)" so the rendered line never has a hole in it.
func ChangedSinceBaselineLabel(data map[string]any) string {
	if gen := stringValue(data, "since_generation_id"); gen != "" {
		return gen
	}
	if at := stringValue(data, "since_observed_at"); at != "" {
		return at
	}
	return "(unknown)"
}
