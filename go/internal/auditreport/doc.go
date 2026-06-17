// Package auditreport generates a deterministic, offline competitive-audit
// report. It reconciles an operator-authored audit input (competitor features
// and the source files inspected) against the capability catalog and an optional
// open-issues list, classifies each finding with the shared auditpreflight
// taxonomy, and recommends one of: no issue, link an existing issue, update an
// existing issue, draft a new issue, or review.
//
// The generator never creates issues and never scrapes competitor repositories;
// it is input-driven so evidence stays source-backed and the report stays
// deterministic for golden tests. Generate sorts entries by competitor then
// feature; RenderMarkdown and RenderJSON emit stable output.
package auditreport
