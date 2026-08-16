// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package playbooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// EnvelopeClient is the part of the CLI's API client this package needs: the
// two calls that ask for Eshu's canonical envelope MIME type. It is declared
// here, where it is consumed, so the package does not depend on the concrete
// client — go/cmd/eshu is package main and nothing can import it.
type EnvelopeClient interface {
	GetEnvelope(path string, result any) error
	PostEnvelope(path string, body, result any) error
}

// errNilClient reports a caller wiring bug: the wrapper handed the Run
// functions no transport. Failing loudly here keeps an unwired seam from
// looking like a healthy command that printed nothing.
var errNilClient = errors.New("playbooks: no API client configured")

// ResolveOptions carries the resolve command's inputs, already read off their
// flags and parsed by the caller. PlaybookID may arrive with surrounding
// whitespace; RunResolve trims it before the request.
type ResolveOptions struct {
	// PlaybookID names the playbook to resolve, as typed by the operator.
	PlaybookID string
	// Inputs are the parsed --input key=value pairs. Use ParseInputs to build
	// them from the raw flag values.
	Inputs map[string]string
}

// ListEnvelope is the canonical Eshu response for the playbook list. Data and
// Truth stay generic maps: the CLI prints the envelope verbatim, so a
// server-side field addition reaches operators without a CLI release.
type ListEnvelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// ResolveEnvelope is the canonical Eshu response for a resolved playbook. The
// resolved member is typed because its shape is the command's contract; Truth
// stays generic for the same pass-through reason as ListEnvelope.
type ResolveEnvelope struct {
	Data struct {
		Resolved struct {
			PlaybookID   string           `json:"playbook_id"`
			Version      string           `json:"version"`
			PromptFamily string           `json:"prompt_family"`
			Calls        []map[string]any `json:"calls"`
			FailureModes []map[string]any `json:"failure_modes"`
		} `json:"resolved"`
	} `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the envelope's error member, printed verbatim as part of
// the JSON output rather than mapped to an exit code: the playbook commands
// report capability and query failures in-band.
type EnvelopeError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// RunList fetches the query-playbook list and writes the envelope to w as
// indented JSON. Nothing is written when the transport fails, so a failing
// command never emits a partial document.
func RunList(w io.Writer, client EnvelopeClient) error {
	if client == nil {
		return errNilClient
	}
	var envelope ListEnvelope
	if err := client.GetEnvelope("/api/v0/query-playbooks", &envelope); err != nil {
		return err //nolint:wrapcheck // the client's error text is the operator-visible contract; cmd/eshu prints it verbatim
	}
	return writeJSON(w, envelope)
}

// RunResolve resolves one playbook into bounded calls and writes the envelope
// to w as indented JSON. The playbook ID is trimmed before the request;
// nothing is written when the transport fails.
func RunResolve(w io.Writer, client EnvelopeClient, opts ResolveOptions) error {
	if client == nil {
		return errNilClient
	}
	envelope, err := fetchResolve(client, opts)
	if err != nil {
		return err
	}
	return writeJSON(w, envelope)
}

// fetchResolve issues the resolve POST and decodes the canonical envelope. It
// is split from RunResolve so the request contract (path, trimmed ID, body
// shape) is testable without an output writer.
func fetchResolve(client EnvelopeClient, opts ResolveOptions) (ResolveEnvelope, error) {
	var envelope ResolveEnvelope
	if err := client.PostEnvelope("/api/v0/query-playbooks/resolve", map[string]any{
		"playbook_id": strings.TrimSpace(opts.PlaybookID),
		"inputs":      opts.Inputs,
	}, &envelope); err != nil {
		return ResolveEnvelope{}, err //nolint:wrapcheck // the client's error text is the operator-visible contract; cmd/eshu prints it verbatim
	}
	return envelope, nil
}

// ParseInputs converts raw --input flag values into the resolve request's
// input map. Each entry must be key=value; keys and values are trimmed, an
// empty value is allowed, and an empty key or a missing separator is an
// error because silently dropping an operator's input would change the
// resolved playbook.
func ParseInputs(raw []string) (map[string]string, error) {
	inputs := make(map[string]string, len(raw))
	for _, item := range raw {
		key, value, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("input must use key=value form")
		}
		inputs[key] = strings.TrimSpace(value)
	}
	return inputs, nil
}

// writeJSON prints the envelope the way the playbook commands always have:
// two-space indent, HTML escaping off so URLs and Cypher fragments survive
// verbatim, and a trailing newline from Encode.
//
//nolint:wrapcheck // a failed write's error is the operator-facing text; wrapping would change what the CLI has always printed
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
