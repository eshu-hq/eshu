// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

// EntityPosition captures the real start and end line of one CloudFormation
// entity, measured directly from a parsed source tree by a caller that has
// one (currently only the YAML adapter, via gopkg.in/yaml.v3 Node.Line
// values -- see internal/parser/yaml/cloudformation_positions.go).
// StartLine is the entity's own key line; EndLine is the highest line number
// touched by the entity's value subtree. A zero EntityPosition (StartLine
// <= 0) means "unmeasured": SectionPositions.linesFor falls back instead of
// using it.
type EntityPosition struct {
	StartLine int
	EndLine   int
}

// SectionPositions groups per-entity EntityPosition overrides for one
// CloudFormation section (Parameters, Conditions, Resources, or Outputs).
// FallbackLine is the section header's own physical line (the line of the
// literal "Resources:" key, for example). ParseWithPositions uses
// FallbackLine -- instead of the document-root lineNumber -- for any entity
// in the section whose own EntityPosition the caller could not compute: a
// structural fallback such as an unresolvable alias/merge shape, or an
// entity name present in the decoded document but absent from Entries. A
// zero SectionPositions (nil Entries, FallbackLine 0) reproduces Parse's
// original behavior of stamping every entity in the section with the
// document-root lineNumber.
type SectionPositions struct {
	Entries      map[string]EntityPosition
	FallbackLine int
}

// Positions groups per-section SectionPositions for one CloudFormation
// document. JSON callers never populate Positions -- JSON decoding does not
// preserve per-key positions, a gap tracked in issue #5348 -- so calling
// Parse (which passes a zero Positions) keeps their documented,
// single-document-root-line behavior for line_number and omits end_line
// entirely, unchanged from before this type existed.
type Positions struct {
	Parameters SectionPositions
	Conditions SectionPositions
	Resources  SectionPositions
	Outputs    SectionPositions
}

// linesFor resolves the (start, end, known) triple ParseWithPositions stamps
// on entity name in this section: the entity's own measured EntityPosition
// when present, else the section's FallbackLine (the section header's own
// line), else documentFallback (the document-root lineNumber the caller
// passed to ParseWithPositions) with known=false. known is false only when
// neither a per-entity measurement nor a section-header fallback exists --
// the JSON contract (a zero SectionPositions, tracked separately in issue
// #5348) and a YAML document-level walk failure (root node unavailable) both
// look like this. ParseWithPositions uses known to decide whether to emit an
// end_line field at all: Parse's zero-Positions callers must keep their
// original line_number-only row shape verbatim, and a total-failure fallback
// must not fabricate a same-as-start end_line that would collapse
// materialize.go's snippet window to one line when its own
// next-entity/+24 heuristic would do better. The returned end is never lower
// than the returned start.
func (s SectionPositions) linesFor(name string, documentFallback int) (start int, end int, known bool) {
	if pos, ok := s.Entries[name]; ok && pos.StartLine > 0 {
		end = pos.EndLine
		if end < pos.StartLine {
			end = pos.StartLine
		}
		return pos.StartLine, end, true
	}
	if s.FallbackLine > 0 {
		return s.FallbackLine, s.FallbackLine, true
	}
	return documentFallback, documentFallback, false
}
