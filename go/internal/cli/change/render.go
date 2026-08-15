// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"encoding/json"
	"fmt"
	"io"
)

// FinishImpact writes the pre-change impact result to w and returns cmdErr
// unchanged, so the caller keeps ownership of the exit code.
//
// The three renderings are not interchangeable. With opts.JSON the whole
// envelope is serialized and nothing else is printed, because a caller piping
// the output into jq must not receive a human summary mixed in. Without it, a
// failed call prints the error line, and a failed call that still carried data
// prints the summary anyway -- an operator who asked what a change touches is
// better served by a truncated answer plus a non-zero exit than by an exit code
// alone.
//
// A rendering error replaces cmdErr rather than being swallowed: output that
// did not reach the terminal is a worse failure than the one being reported.
func FinishImpact(w io.Writer, opts Options, envelope Envelope, cmdErr error) error {
	return finish(w, opts, envelope, cmdErr, renderImpactSummary)
}

// FinishPlan is FinishImpact for the developer change plan. It differs only in
// which summary it renders.
func FinishPlan(w io.Writer, opts Options, envelope Envelope, cmdErr error) error {
	return finish(w, opts, envelope, cmdErr, renderPlanSummary)
}

// finish holds the branch structure both commands share, with renderSummary as
// the one difference between them.
func finish(w io.Writer, opts Options, envelope Envelope, cmdErr error, renderSummary func(io.Writer, Envelope) error) error {
	if opts.JSON {
		if writeErr := writeJSON(w, envelope); writeErr != nil {
			return writeErr
		}
		return cmdErr
	}
	if cmdErr != nil {
		if envelope.Error != nil {
			if renderErr := renderError(w, envelope); renderErr != nil {
				return renderErr
			}
		} else if envelope.Data != nil {
			if renderErr := renderSummary(w, envelope); renderErr != nil {
				return renderErr
			}
		}
		return cmdErr
	}
	return renderSummary(w, envelope)
}

// writeJSON serializes v as indented JSON with HTML escaping off, so a path or
// URL in the response keeps its literal &, < and > instead of arriving as \u
// escapes an operator then has to decode.
//
//nolint:wrapcheck // the encoder error reaches the operator as cobra's "Error: ..." line; wrapping it would change that text and break CLI output parity.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// renderError prints the one-line form of an envelope error. It is a no-op for
// an envelope with no error member.
//
//nolint:wrapcheck // a write error here is the caller's io.Writer failing; wrapping adds a prefix to text the operator reads as-is.
func renderError(w io.Writer, envelope Envelope) error {
	if envelope.Error == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Pre-change impact error (%s): %s\n", envelope.Error.Code, envelope.Error.Message)
	return err
}

// renderImpactSummary prints the human view of a pre-change impact answer:
// freshness, the counts, and the bounded follow-up calls the API recommends.
//
// The freshness line is printed only when truth carries one, so an envelope
// that never claimed a freshness state does not get labelled with an empty one.
//
//nolint:wrapcheck // every error returned here is the caller's io.Writer failing mid-line; wrapping changes what the operator sees.
func renderImpactSummary(w io.Writer, envelope Envelope) error {
	data := envelope.Data
	if freshness := freshnessState(envelope); freshness != "" {
		if _, err := fmt.Fprintf(w, "Truth freshness: %s\n", freshness); err != nil {
			return err
		}
	}
	codeSurface := mapValue(data, "code_surface")
	impactSummary := mapValue(data, "impact_summary")
	if _, err := fmt.Fprintf(
		w,
		"Pre-change impact: %d changed files (coverage=%s truncated=%t)\n",
		intValue(data, "changed_file_count"),
		stringValue(mapValue(data, "coverage"), "state"),
		boolValue(data, "truncated"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"  symbols=%d direct=%d transitive=%d missing_evidence=%d\n",
		intValue(codeSurface, "symbol_count"),
		intValue(impactSummary, "direct_count"),
		intValue(impactSummary, "transitive_count"),
		len(sliceValue(data, "missing_evidence")),
	); err != nil {
		return err
	}
	for _, raw := range sliceValue(data, "recommended_next_calls") {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  next=%s reason=%s\n", stringValue(call, "tool"), stringValue(call, "reason")); err != nil {
			return err
		}
	}
	return nil
}

// renderPlanSummary prints the human view of a developer change plan: the
// action count, then one line per action and per bounded follow-up call.
//
// Unlike the impact summary it prints no freshness line. That is the shipped
// behavior, not an omission -- the plan's freshness still fails the command
// closed through ClassifyPlan.
//
//nolint:wrapcheck // same reason as renderImpactSummary: the operator reads these bytes directly.
func renderPlanSummary(w io.Writer, envelope Envelope) error {
	data := envelope.Data
	if _, err := fmt.Fprintf(
		w,
		"Developer change plan: %d actions for %d changed files (blocked=%t truncated=%t)\n",
		len(sliceValue(data, "actions")),
		intValue(data, "changed_file_count"),
		boolValue(data, "blocked"),
		boolValue(data, "truncated"),
	); err != nil {
		return err
	}
	for _, raw := range sliceValue(data, "actions") {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  action=%s risk=%s title=%s\n", stringValue(action, "kind"), stringValue(action, "risk"), stringValue(action, "title")); err != nil {
			return err
		}
	}
	for _, raw := range sliceValue(data, "bounded_next_calls") {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "  next=%s target=%s\n", stringValue(call, "kind"), stringValue(call, "target")); err != nil {
			return err
		}
	}
	return nil
}
