// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package trace

import (
	"net/url"
	"strings"
)

// EnvelopeFetcher is the transport `eshu trace service` needs: one GET that
// decodes a canonical Eshu envelope into result. It is declared here, where it
// is consumed, because the concrete API client lives in go/cmd/eshu, which is
// package main and cannot be imported.
type EnvelopeFetcher interface {
	GetEnvelope(path string, result any) error
}

// ServiceEnvelope is the canonical Eshu response shape as `eshu trace service`
// consumes it. Data and Truth stay generic maps: the command renders a handful
// of named keys and passes everything else through to --json untouched, so a
// server-side field addition reaches operators without a CLI release.
type ServiceEnvelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *ServiceError  `json:"error"`
}

// ServiceError is the envelope's error member. Code is what go/cmd/eshu maps to
// a process exit code, and what selects the ambiguous-candidates rendering;
// Message is rendered verbatim.
type ServiceError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// ServiceQuery carries the selectors `eshu trace service` sends with the
// request, and nothing else -- the output mode (--json) is the command
// wrapper's concern and never reaches this package. go/cmd/eshu fills it from
// the command's flags; every field is already trimmed by the time it arrives,
// and an empty field is omitted from the query string rather than sent blank.
type ServiceQuery struct {
	Repo        string
	Environment string
	ServiceID   string
}

// FetchServiceStory requests the canonical service story envelope for selector.
//
// The selector is path-escaped, so a service name containing a slash or a space
// reaches the API intact. Only the non-empty query fields become query
// parameters, which keeps the request URL -- and the error text that embeds it
// on a transport failure -- to what the operator actually asked for.
//
// A transport failure returns the zero envelope alongside the error. Callers
// synthesize their own envelope from it rather than rendering a half-filled one.
//
//nolint:wrapcheck // The transport error text is operator-facing: go/cmd/eshu renders it verbatim and matches its substrings to classify the failure. Wrapping would change both.
func FetchServiceStory(client EnvelopeFetcher, selector string, opts ServiceQuery) (ServiceEnvelope, error) {
	path := "/api/v0/services/" + url.PathEscape(strings.TrimSpace(selector)) + "/story"
	query := url.Values{}
	if opts.Repo != "" {
		query.Set("repo", opts.Repo)
	}
	if opts.Environment != "" {
		query.Set("environment", opts.Environment)
	}
	if opts.ServiceID != "" {
		query.Set("service_id", opts.ServiceID)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var envelope ServiceEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return ServiceEnvelope{}, err
	}
	return envelope, nil
}

// ServiceFreshnessState reads truth.freshness.state. go/cmd/eshu exits 4 when it
// reports "stale" or "building", so a caller acting on a trace knows the graph
// behind it was not current.
func ServiceFreshnessState(envelope ServiceEnvelope) string {
	return stringValue(mapValue(envelope.Truth, "freshness"), "state")
}

// ServiceStatus reads data.code_to_runtime_trace.status. go/cmd/eshu exits 5 when
// it reports "partial", meaning the trace rendered is missing segments rather
// than being wrong.
func ServiceStatus(envelope ServiceEnvelope) string {
	return stringValue(mapValue(envelope.Data, "code_to_runtime_trace"), "status")
}
