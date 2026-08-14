// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"io"
	"net/url"
	"strconv"
	"strings"
)

// GenerationsRoute is the API route `eshu freshness generations` reads.
const GenerationsRoute = "/api/v0/freshness/generations"

// GenerationsOptions is the selector set for `eshu freshness generations`,
// holding flag values exactly as cobra parsed them. Every string selector is
// trimmed when the request path is built, so a caller may pass raw flag values
// straight through.
type GenerationsOptions struct {
	// JSON writes the canonical envelope instead of the human summary.
	JSON bool
	// ScopeID drills into one ingestion scope.
	ScopeID string
	// Repository selects a repository-kind scope by canonical repository id.
	Repository string
	// CollectorKind filters by collector, for example git or terraform_state.
	CollectorKind string
	// SourceSystem filters by source system, for example github.
	SourceSystem string
	// GenerationID drills into a single generation row.
	GenerationID string
	// Status filters by generation status.
	Status string
	// Limit caps the rows returned. Values of zero or less are omitted from
	// the request, which leaves the server's own default in charge.
	Limit int
}

// GenerationsPath builds the request path for opts. Empty selectors are left
// out entirely rather than sent as empty query parameters, so the server sees
// the same request whether a flag was unset or set to whitespace.
func GenerationsPath(opts GenerationsOptions) string {
	query := url.Values{}
	setSelector(query, "scope_id", opts.ScopeID)
	setSelector(query, "repository", opts.Repository)
	setSelector(query, "collector_kind", opts.CollectorKind)
	setSelector(query, "source_system", opts.SourceSystem)
	setSelector(query, "generation_id", opts.GenerationID)
	setSelector(query, "status", opts.Status)
	setLimit(query, "limit", opts.Limit)
	return joinPath(GenerationsRoute, query)
}

// FetchGenerations reads the generation lifecycle envelope for opts. A
// transport failure returns the zero envelope alongside the error, so callers
// never render a half-decoded response.
func FetchGenerations(client EnvelopeFetcher, opts GenerationsOptions) (Envelope, error) {
	return fetch(client, GenerationsPath(opts))
}

// RunGenerations fetches the generation lifecycle envelope, writes either the
// canonical JSON or the human summary to w, and returns a *Failure when the
// command must exit non-zero. A transport failure is reported through the same
// envelope shape as a server-reported one.
func RunGenerations(w io.Writer, client EnvelopeFetcher, opts GenerationsOptions) error {
	env, err := FetchGenerations(client, opts)
	if err != nil {
		env = transportEnvelope(err)
	}
	return finish(w, opts.JSON, env, RenderGenerationsSummary, failureOf(env))
}

// RenderGenerationsSummary writes the human view of a generation lifecycle
// envelope: the truth freshness state, the row count and truncation flag, then
// one line per generation. The active generation is marked with a leading `*`.
// Queue and failure detail is appended only when the server reported it.
func RenderGenerationsSummary(w io.Writer, env Envelope) error {
	if err := renderFreshnessLine(w, env); err != nil {
		return err
	}
	data := env.Data
	if err := writef(
		w,
		"Generations: %d (truncated=%t)\n",
		intValue(data, "count"),
		boolValue(data, "truncated"),
	); err != nil {
		return err
	}
	for _, raw := range sliceValue(data, "generations") {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := renderGenerationRow(w, row); err != nil {
			return err
		}
	}
	return nil
}

// renderGenerationRow writes one generation lifecycle line.
func renderGenerationRow(w io.Writer, row map[string]any) error {
	marker := " "
	if boolValue(row, "is_active") {
		marker = "*"
	}
	if err := writef(
		w,
		"%s %s status=%s scope=%s trigger=%s",
		marker,
		stringValue(row, "generation_id"),
		stringValue(row, "status"),
		stringValue(row, "scope_id"),
		stringValue(row, "trigger_kind"),
	); err != nil {
		return err
	}
	if queue := mapValue(row, "queue_status"); queue != nil {
		if err := writef(
			w,
			" queue[outstanding=%d failed=%d dead_letter=%d]",
			intValue(queue, "outstanding"),
			intValue(queue, "failed"),
			intValue(queue, "dead_letter"),
		); err != nil {
			return err
		}
	}
	if failure := mapValue(row, "latest_failure"); failure != nil {
		if err := writef(w, " failure=%s", stringValue(failure, "failure_class")); err != nil {
			return err
		}
	}
	return writef(w, "\n")
}

// failureOf is the envelope-to-failure step every Run function shares.
func failureOf(env Envelope) error {
	if failure := EnvelopeFailure(env.Error); failure != nil {
		return failure
	}
	return nil
}

// setSelector adds key to query when value has non-whitespace content.
func setSelector(query url.Values, key, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		query.Set(key, trimmed)
	}
}

// setLimit adds a positive bound to query. Zero and negatives are omitted so
// the server applies its own default rather than being asked for no rows.
func setLimit(query url.Values, key string, value int) {
	if value > 0 {
		query.Set(key, strconv.Itoa(value))
	}
}

// joinPath appends the encoded query to route, omitting the "?" when there is
// nothing to encode.
func joinPath(route string, query url.Values) string {
	encoded := query.Encode()
	if encoded == "" {
		return route
	}
	return route + "?" + encoded
}

// fetch performs the envelope GET.
//
//nolint:wrapcheck // The transport error text is operator-facing: it is rendered verbatim and its substrings drive ErrorCodeFromTransport. Wrapping would change both.
func fetch(client EnvelopeFetcher, path string) (Envelope, error) {
	var env Envelope
	if err := client.GetEnvelope(path, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}
