// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"io"
	"net/url"
)

// ServiceChangedSinceRoute is the API route
// `eshu freshness service-changed-since` reads.
const ServiceChangedSinceRoute = "/api/v0/freshness/services/changed-since"

// ServiceChangedSinceOptions is the selector set for
// `eshu freshness service-changed-since`, holding flag values exactly as cobra
// parsed them. String selectors are trimmed when the request path is built.
type ServiceChangedSinceOptions struct {
	// JSON writes the canonical envelope instead of the human summary.
	JSON bool
	// ServiceID names the service whose evidence lineage to diff.
	ServiceID string
	// SinceGenerationID is the prior service materialization generation to
	// diff from.
	SinceGenerationID string
	// SampleLimit caps sample handles per classification per family. Values of
	// zero or less are omitted, leaving the server's default in charge.
	SampleLimit int
}

// ServiceChangedSincePath builds the request path for opts.
func ServiceChangedSincePath(opts ServiceChangedSinceOptions) string {
	query := url.Values{}
	setSelector(query, "service_id", opts.ServiceID)
	setSelector(query, "since_generation_id", opts.SinceGenerationID)
	setLimit(query, "sample_limit", opts.SampleLimit)
	return joinPath(ServiceChangedSinceRoute, query)
}

// FetchServiceChangedSince reads the service changed-since envelope for opts.
func FetchServiceChangedSince(client EnvelopeFetcher, opts ServiceChangedSinceOptions) (Envelope, error) {
	return fetch(client, ServiceChangedSincePath(opts))
}

// RunServiceChangedSince fetches the service changed-since envelope, writes
// either the canonical JSON or the human summary to w, and returns a *Failure
// when the command must exit non-zero.
func RunServiceChangedSince(w io.Writer, client EnvelopeFetcher, opts ServiceChangedSinceOptions) error {
	env, err := FetchServiceChangedSince(client, opts)
	if err != nil {
		env = transportEnvelope(err)
	}
	return finish(w, opts.JSON, env, RenderServiceChangedSinceSummary, failureOf(env))
}

// RenderServiceChangedSinceSummary writes the human view of a service
// changed-since envelope: the truth freshness state, the baseline-to-current
// generation line, then one line per evidence family. A service with no
// current active generation renders an explicit notice instead of zeroed
// counts.
//
// The baseline is the raw since_generation_id, with no observed-at fallback:
// the service route takes a generation id only, so there is no instant to fall
// back to.
func RenderServiceChangedSinceSummary(w io.Writer, env Envelope) error {
	if err := renderFreshnessLine(w, env); err != nil {
		return err
	}
	data := env.Data
	if err := writef(
		w,
		"Service changed since %s -> %s (service=%s)\n",
		stringValue(data, "since_generation_id"),
		stringValue(data, "current_active_generation_id"),
		stringValue(data, "service_id"),
	); err != nil {
		return err
	}
	if boolValue(data, "unavailable") {
		return writef(w, "  diff unavailable: service has no current active generation\n")
	}
	return renderCategories(w, data)
}
