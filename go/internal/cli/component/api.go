// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"io"
	"net/url"
	"strconv"
	"strings"
)

// EnvelopeFetcher is the transport the component inventory and diagnostics
// commands need: one GET that decodes a canonical Eshu envelope into result.
// It is declared here, where it is consumed, because the concrete client
// lives in go/cmd/eshu, which is package main and cannot be imported.
type EnvelopeFetcher interface {
	GetEnvelope(path string, result any) error
}

// Envelope is the canonical Eshu response shape as the component extension
// commands consume it. Data and Truth stay generic maps: the CLI renders a
// handful of named keys and passes everything else through to --json
// untouched, so a server-side field addition reaches operators without a CLI
// release.
type Envelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the envelope's error member. Code is what go/cmd/eshu
// maps onto the process exit code; Message is rendered verbatim.
type EnvelopeError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// Inventory row limits: the flag default and the ceiling the CLI enforces
// before it lets a request leave the machine.
const (
	InventoryDefaultLimit = 100
	InventoryMaxLimit     = 500
)

// FetchInventory reads the component extension inventory through client,
// requesting at most limit rows. A zero limit falls back to
// InventoryDefaultLimit.
func FetchInventory(client EnvelopeFetcher, limit int) (Envelope, error) {
	if limit == 0 {
		limit = InventoryDefaultLimit
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	var envelope Envelope
	if err := client.GetEnvelope("/api/v0/component-extensions?"+params.Encode(), &envelope); err != nil {
		return Envelope{}, err //nolint:wrapcheck // the transport error's text is what go/cmd/eshu classifies and prints; a wrap would change both
	}
	return envelope, nil
}

// FetchDiagnostics reads one component's extension diagnostics through
// client. componentID is path-escaped, so an operator-supplied id cannot
// change the request route.
func FetchDiagnostics(client EnvelopeFetcher, componentID string) (Envelope, error) {
	path := "/api/v0/component-extensions/" + url.PathEscape(componentID) + "/diagnostics"
	var envelope Envelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return Envelope{}, err //nolint:wrapcheck // same contract as FetchInventory: go/cmd/eshu classifies and prints this text verbatim
	}
	return envelope, nil
}

// FinishAPI writes a component extension command's terminal output and
// returns the error the command exits with. In JSON mode the envelope is
// always written, failure or not, and a write error outranks the command
// failure because a truncated envelope is the more urgent thing to tell the
// operator about. cmdErr carries the exit-code mapping go/cmd/eshu already
// chose; it passes through untouched.
func FinishAPI(w io.Writer, jsonOutput bool, envelope Envelope, cmdErr error) error {
	if jsonOutput {
		if writeErr := writeJSON(w, envelope); writeErr != nil {
			return writeErr
		}
		return cmdErr
	}
	if cmdErr != nil {
		if renderErr := renderAPIError(w, envelope); renderErr != nil {
			return renderErr
		}
		return cmdErr
	}
	return renderAPISummary(w, envelope)
}

// renderAPIError writes the one-line failure summary for an envelope that
// reported an error. It writes nothing when the envelope carries no error.
func renderAPIError(w io.Writer, envelope Envelope) error {
	if envelope.Error == nil {
		return nil
	}
	return writef(w, "Component extension error (%s): %s\n", envelope.Error.Code, envelope.Error.Message)
}

// renderAPISummary writes the human-readable inventory or diagnostics
// summary: one drilldown row when the envelope carries a single component,
// otherwise the truth freshness line, the count header, and one row per
// component.
func renderAPISummary(w io.Writer, envelope Envelope) error {
	if component := mapValue(envelope.Data, "component"); component != nil {
		return renderAPIRow(w, component)
	}
	if freshness := stringValue(mapValue(envelope.Truth, "freshness"), "state"); freshness != "" {
		if err := writef(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	count := intValue(envelope.Data, "count")
	totalCount := intValue(envelope.Data, "total_count")
	limit := intValue(envelope.Data, "limit")
	truncated := boolValue(envelope.Data, "truncated")
	if totalCount > 0 || limit > 0 || truncated {
		if err := writef(
			w,
			"Component extensions: %d of %d (limit=%d, truncated=%t)\n",
			count,
			totalCount,
			limit,
			truncated,
		); err != nil {
			return err
		}
	} else if err := writef(w, "Component extensions: %d\n", count); err != nil {
		return err
	}
	for _, raw := range sliceValue(envelope.Data, "components") {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if err := renderAPIRow(w, row); err != nil {
			return err
		}
	}
	return nil
}

// renderAPIRow writes one component's id, version, and states, plus the
// policy diagnostic line when the row carries one.
func renderAPIRow(w io.Writer, row map[string]any) error {
	states := stringsValue(row["states"])
	if err := writef(
		w,
		"%s@%s states=%s\n",
		stringValue(row, "id"),
		stringValue(row, "version"),
		strings.Join(states, ","),
	); err != nil {
		return err
	}
	if diagnostics := mapValue(row, "diagnostics"); diagnostics != nil {
		if reason := stringValue(diagnostics, "policy_reason"); reason != "" {
			return writef(w, "  policy=%s reason=%s\n", stringValue(diagnostics, "policy_code"), reason)
		}
	}
	return nil
}
