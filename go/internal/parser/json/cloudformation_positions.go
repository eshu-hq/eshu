// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import (
	"github.com/eshu-hq/eshu/go/internal/parser/cloudformation"
)

// cloudformationSections are the top-level CloudFormation/SAM template keys
// this package walks for real per-entity line positions. Anchoring is strictly
// at the document root's own top-level pairs -- never by searching for a key
// name anywhere in the tree -- so a resource's Properties block that happens to
// contain a nested "Resources" or "Outputs" key (for example
// AWS::CloudFormation::Stack's nested template body) is never mistaken for a
// template section. This is the JSON twin of the YAML adapter's
// cloudformationSections (issue #5348 mirrors issue #5328).
var cloudformationSections = []string{"Parameters", "Conditions", "Resources", "Outputs"}

// cloudformationPositionFallback records one degraded-position event: a
// CloudFormation entity (or an entire section) whose real per-entity
// line_number/end_line the ordered JSON walk could not resolve, so
// cloudformation.ParseWithPositions fell back to the section header line or the
// document-root line instead. The collector layer turns these into the shared
// CloudFormationPositionFallbacks counter (issue #5328); the entity itself is
// never dropped -- only its precision degrades. It is the JSON twin of the YAML
// adapter's identically named type.
type cloudformationPositionFallback struct {
	Section string
	Reason  string
	Line    int
}

// cloudformationPositionsFromDocument walks the ordered JSON entries of a
// CloudFormation/SAM template and returns the real per-entity Positions plus
// any degraded-position fallback events.
//
// normalizedBytes MUST be the exact buffer the generic JSON path decodes (the
// BOM/banner/leading-blank-line-stripped output of normalizeJSONSource), and
// idx MUST be the matching translated newline index built via
// buildTranslatedNewlineIndex(source, translate) over the real on-disk source.
// That pairing is load-bearing (issue #5358, #5348): feeding raw on-disk bytes
// would fail to decode a BOM/banner file and silently degrade every entity to
// the document-root line, and feeding an untranslated index would bake a wrong
// on-disk line into every entity after a stripped prefix -- worse than an
// honest fallback, because line_number is part of the CloudFormation entity's
// canonical identity.
//
// document is the already-flattened stdjson value; it is consulted only to know
// which entity names actually survived stdjson's last-wins duplicate-key
// flattening, so a name present in the document but absent from the ordered
// walk records a fallback instead of silently keeping a stale position.
func cloudformationPositionsFromDocument(
	normalizedBytes []byte,
	idx *newlineIndex,
	document map[string]any,
) (cloudformation.Positions, []cloudformationPositionFallback) {
	topEntries, err := unmarshalOrderedJSONObjectAt(normalizedBytes, 0, idx)
	if err != nil {
		// Unreachable in practice: stdjson.Unmarshal already succeeded on the
		// same bytes in Parse before this walk runs. Degrade to zero Positions
		// (Parse's original document-root behavior) and record one fallback
		// rather than fabricate positions.
		return cloudformation.Positions{}, []cloudformationPositionFallback{
			{Section: "document", Reason: "ordered_walk_failed"},
		}
	}

	var positions cloudformation.Positions
	var fallbacks []cloudformationPositionFallback
	for _, section := range cloudformationSections {
		flatSection, present := document[section].(map[string]any)
		if !present {
			continue
		}

		// Anchor strictly at the LAST top-level pair named section: stdjson
		// flattens duplicate top-level keys with last-wins, so a malformed
		// template with two "Resources" blocks resolves entity names against
		// the same block the flattened document kept. This mirrors the YAML
		// adapter's cloudformationSectionNodes last-match rule.
		sectionEntry := lastOrderedJSONEntry(topEntries, section)
		sectionPositions := cloudformation.SectionPositions{}
		if sectionEntry != nil {
			sectionPositions.FallbackLine = sectionEntry.Line
		}

		childEntries, reason, ok := decodeCloudformationSectionEntries(sectionEntry, idx)
		if !ok {
			fallbacks = append(fallbacks, cloudformationPositionFallback{
				Section: section,
				Reason:  reason,
				Line:    sectionPositions.FallbackLine,
			})
		} else {
			entries := make(map[string]cloudformation.EntityPosition, len(flatSection))
			for _, child := range childEntries {
				// Unconditional overwrite (last-wins): stdjson flattening keeps
				// the LAST duplicate entity name, so the position map must
				// agree. First-wins would stamp the position of a definition no
				// longer present in the flattened document.
				entries[child.Key] = cloudformation.EntityPosition{
					StartLine: child.Line,
					EndLine:   idx.lineAt(orderedJSONEntryEndByte(child)),
				}
			}
			sectionPositions.Entries = entries
			for name := range flatSection {
				if _, resolved := entries[name]; !resolved {
					fallbacks = append(fallbacks, cloudformationPositionFallback{
						Section: section,
						Reason:  "entity_position_missing",
						Line:    sectionPositions.FallbackLine,
					})
				}
			}
		}

		switch section {
		case "Parameters":
			positions.Parameters = sectionPositions
		case "Conditions":
			positions.Conditions = sectionPositions
		case "Resources":
			positions.Resources = sectionPositions
		case "Outputs":
			positions.Outputs = sectionPositions
		}
	}
	return positions, fallbacks
}

// decodeCloudformationSectionEntries decodes sectionEntry's object value into
// its ordered child entries with real line numbers. It returns (nil, reason,
// false) when the section top-level pair is missing from the ordered walk or
// its value is not a decodable JSON object, so the caller records a fallback;
// otherwise (entries, "", true). sectionEntry.valueStartByte anchors the child
// offsets against the original buffer idx was built from, keeping nested lines
// correct at depth.
func decodeCloudformationSectionEntries(sectionEntry *orderedJSONEntry, idx *newlineIndex) ([]orderedJSONEntry, string, bool) {
	if sectionEntry == nil {
		return nil, "section_entry_missing", false
	}
	entries, err := unmarshalOrderedJSONObjectAt(sectionEntry.Value, sectionEntry.valueStartByte, idx)
	if err != nil {
		return nil, "unresolved_section_object", false
	}
	return entries, "", true
}

// lastOrderedJSONEntry returns a pointer to the LAST top-level entry named key
// (last-wins, matching stdjson's duplicate-key flattening), or nil when no
// entry has that key. The pointer aliases entries' backing array; callers must
// not retain it past the slice's lifetime.
func lastOrderedJSONEntry(entries []orderedJSONEntry, key string) *orderedJSONEntry {
	var found *orderedJSONEntry
	for index := range entries {
		if entries[index].Key == key {
			found = &entries[index]
		}
	}
	return found
}

// orderedJSONEntryEndByte returns the global byte offset of the FINAL byte of
// entry's value -- the closing brace/bracket for an object/array, or the last
// character of a scalar token. It is derived from the verbatim raw value bytes
// (valueStartByte + len(Value) - 1) rather than a decoder end offset, which can
// overshoot a bare scalar by trailing lookahead bytes (issue #5348). Callers
// resolve it through idx.lineAt to get the entity's end_line. This deliberately
// does not extend orderedJSONEntry with a stored end offset, so the shared
// #5329 ordered-walk struct and its generic-JSON consumers gain no new field.
func orderedJSONEntryEndByte(entry orderedJSONEntry) int64 {
	return entry.valueStartByte + int64(len(entry.Value)) - 1
}

// firstPositiveInt returns the first value greater than zero, or the last value
// when none are positive. It resolves the line_number recorded on a
// cloudformation_position_fallbacks row: the section header line when known,
// else the document-root line the JSON adapter always has (1). It is the JSON
// twin of the YAML adapter's identically named helper.
func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}
