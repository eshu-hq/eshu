// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"encoding/json"
	"fmt"
	"io"
)

// sections lists the entity-map summary sections in the order they print. The
// order is part of the command's output contract, so it is a fixed list rather
// than a range over the response map.
var sections = []struct {
	key   string
	title string
}{
	{"defined_by", "Defined by"},
	{"deployed_by", "Deployed by"},
	{"runs_as", "Runs as"},
	{"depends_on", "Depends on"},
	{"consumed_by", "Consumed by"},
}

// Write renders envelope to w in the form opts and failure select: the
// canonical envelope as JSON when jsonOutput is set, the ambiguity guidance
// when the map failed, and the grouped summary when it succeeded.
//
// A render error on the failing path is dropped: the caller is about to report
// failure with a more useful message, and replacing it with "short write"
// would lose the reason the command failed.
func Write(w io.Writer, jsonOutput bool, envelope Envelope, failure *Failure) error {
	if jsonOutput {
		return WriteJSON(w, envelope)
	}
	if failure != nil {
		_ = RenderError(w, envelope)
		return nil
	}
	return RenderSummary(w, envelope)
}

// WriteJSON writes the canonical envelope to w as indented JSON with HTML
// escaping off, so selectors containing &, <, or > read back as typed.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v) //nolint:wrapcheck // the caller prints this verbatim; a wrap would prefix operator-visible output
}

// RenderError prints the operator guidance for a failed entity map. Only an
// ambiguous resolution has anything useful to add -- the candidate list the
// operator must choose from -- so every other failure prints nothing and lets
// the caller's message stand alone.
//
//nolint:wrapcheck // a write error surfaces as the command's error text; a wrap would prefix it
func RenderError(w io.Writer, envelope Envelope) error {
	if stringField(envelope.Data, "status") != "ambiguous" {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Map selector is ambiguous. Add --type, --repo, or --env."); err != nil {
		return err
	}
	resolution := mapField(envelope.Data, "resolution")
	for _, candidate := range sliceField(resolution, "candidates") {
		row, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		id := stringField(row, "id")
		name := stringField(row, "name")
		repoID := stringField(row, "repo_id")
		if _, err := fmt.Fprintf(w, "- %s", firstNonEmpty(id, name, "<unknown>")); err != nil {
			return err
		}
		if name != "" && name != id {
			if _, err := fmt.Fprintf(w, " name=%s", name); err != nil {
				return err
			}
		}
		if repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// RenderSummary prints the grouped entity-map summary: what the selector
// resolved to, the five relationship sections in fixed order, and the evidence
// count with its truncation flag.
//
//nolint:wrapcheck // a write error surfaces as the command's error text; a wrap would prefix it
func RenderSummary(w io.Writer, envelope Envelope) error {
	data := envelope.Data
	resolution := mapField(data, "resolution")
	selected := mapField(resolution, "selected")
	if _, err := fmt.Fprintf(w, "Map: %s\n", stringField(data, "from")); err != nil {
		return err
	}
	if len(selected) > 0 {
		if _, err := fmt.Fprintf(
			w,
			"Resolved: %s %s",
			displayLabel(selected),
			firstNonEmpty(stringField(selected, "id"), stringField(selected, "name")),
		); err != nil {
			return err
		}
		if name := stringField(selected, "name"); name != "" {
			if _, err := fmt.Fprintf(w, " (%s)", name); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	responseSections := mapField(data, "sections")
	for _, section := range sections {
		if err := RenderSection(w, section.title, sliceField(responseSections, section.key)); err != nil {
			return err
		}
	}
	evidence := mapField(data, "evidence")
	if _, err := fmt.Fprintf(w, "Evidence: %d relationships\n", intField(evidence, "relationship_count")); err != nil {
		return err
	}
	if truncated, ok := evidence["truncated"].(bool); ok && truncated {
		if _, err := fmt.Fprintln(w, "Truncated: true"); err != nil {
			return err
		}
	}
	return nil
}

// RenderSection prints one titled relationship section. An empty section
// prints nothing at all, including its title, so a map with two populated
// sections does not read as five sections that mostly failed.
//
//nolint:wrapcheck // a write error surfaces as the command's error text; a wrap would prefix it
func RenderSection(w io.Writer, title string, rows []any) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s:\n", title); err != nil {
		return err
	}
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(
			w,
			"- %s %s",
			stringField(row, "relationship_type"),
			firstNonEmpty(stringField(row, "entity_name"), stringField(row, "entity_id")),
		); err != nil {
			return err
		}
		if repoID := stringField(row, "repo_id"); repoID != "" {
			if _, err := fmt.Fprintf(w, " repo=%s", repoID); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	return nil
}

// displayLabel returns the graph label to print for the resolved entity, or
// "Entity" when the response carried no labels.
func displayLabel(selected map[string]any) string {
	labels := stringList(selected["labels"])
	if len(labels) == 0 {
		return "Entity"
	}
	return labels[0]
}
