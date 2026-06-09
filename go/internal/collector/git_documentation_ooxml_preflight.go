package collector

import "github.com/eshu-hq/eshu/go/internal/collector/ooxmlpreflight"

// ooxmlPreflightWarnings flattens preflight warning classes into the document
// warning slice shared by the XLSX, PPTX, DOCX, and diagram extractors.
func ooxmlPreflightWarnings(result ooxmlpreflight.Result) []string {
	warnings := make([]string, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		if warning.Class != "" {
			warnings = append(warnings, warning.Class)
		}
	}
	return warnings
}

// benignOOXMLPreflightWarnings are preflight warning classes that report
// intentionally skipped content rather than a safety, resource, or structural
// failure. They are surfaced on the document but must not stop metadata
// extraction: the document is parsed normally and the warning records what was
// deliberately not extracted (hidden sheets/slides/text, comment or
// tracked-change text).
var benignOOXMLPreflightWarnings = map[string]bool{
	ooxmlpreflight.WarningHiddenContentSkipped:  true,
	ooxmlpreflight.WarningAnnotationTextSkipped: true,
}

// ooxmlPreflightBlocked reports whether a preflight result must stop document
// extraction. A result is blocked when it carries any warning class outside the
// benign content-skipped set.
//
// Result.Safe is deliberately not used here. The ooxmlpreflight package sets
// Safe=false for ANY recorded warning, including the benign content-skipped
// notes above; gating extraction on Safe therefore drops every document that
// merely contains a hidden sheet, hidden slide, or comment, discarding all of
// its real metadata and sections. Only genuine safety, resource,
// malformed-container, or external-reference warnings block extraction.
func ooxmlPreflightBlocked(result ooxmlpreflight.Result) bool {
	for _, warning := range result.Warnings {
		if warning.Class == "" {
			continue
		}
		if !benignOOXMLPreflightWarnings[warning.Class] {
			return true
		}
	}
	return false
}
