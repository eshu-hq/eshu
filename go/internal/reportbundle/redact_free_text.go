// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// This file owns which FIELDS of a bundle are free text. The scan itself lives
// in internal/urlredact beside the query-string walk, because the two have to
// agree on where a "key=value" pair ends and twice did not: this package had
// "?", "&" and ";" while cmd/eshu's endpoint walk had only "&", and three
// credentials shipped verbatim through the difference. Splitting the domain
// question (which fields) from the mechanism (how a pair is found) is what
// keeps a boundary change reaching both walks.
//
// Everything the domains here share — the isRedactedKeyName predicate and
// collector.IsSensitiveKeyName underneath it — still lives in redact.go, so
// nothing can drift on what a sensitive key is either.

// The Redaction.Rules entries recorded when the free-text scan removes
// something. Each names the FIELD, not the embedded key that was found:
// "api_key" in the rules list would be indistinguishable from a dropped
// parameter of that name, and the field is what a maintainer accounting for a
// shortened note or a truncated error message needs to look at.
const (
	reporterNoteRule       = "reporter_note"
	errorMessageRule       = "response_error_message"
	errorCorrelationIDRule = "response_error_correlation_id"
)

// The text that replaces a removed span is urlredact.FreeTextMarker, and this
// package deliberately keeps no alias for it. Validate re-scans what Capture
// wrote, so a marker that tripped the scan would make Capture reject its own
// bundle — that constraint belongs to the walk, and a second name for it here
// would be one more thing that can drift.

// redactFreeText is the scan for the FREE-TEXT domain: Bundle.ReporterNote,
// Response.Error.Message, and Response.Error.CorrelationID. It returns the
// cleaned text and whether anything was removed.
//
// The domain is "prose that can contain reporter-typed bytes", not "fields the
// reporter typed directly". Response.Error.Message is composed SERVER-side and
// is still in it, because composing is exactly how the reporter's own string
// gets in: query/service_workload_resolution.go:39 formats the caller's service
// selector into "service selector %q matched multiple services", and
// query/service_story_seam.go:129 puts that sentence in the envelope beside a
// details.selector holding the identical value. The structured half was
// redacted and the sentence was not, so a selector of
// "checkout?token=<credential>" shipped inside a bundle stamped
// profile=public / validation=passed. More than twenty sites in the query
// package assign a non-literal Message — some interpolating err.Error()
// directly, most passing a variable composed further up — so the rule belongs
// at this egress boundary rather than at each composer.
//
// CorrelationID is in the domain for the same reason one level over: it is not
// server-minted when the caller sends one. query/documentation.go:470 returns
// the request's own X-Correlation-ID or X-Request-ID header verbatim, and
// query/auth.go:430 puts that straight into an error envelope without the
// character allowlist safeAuditCorrelationID applies on the audit path.
//
// One field elsewhere in the bundle is free text and is deliberately NOT in
// this domain, recorded here rather than left silent because the package rule
// is that an exemption needs a reason beside it: Evidence.FactRefsReason. It is
// a CaptureInput field, so a programmatic caller could put anything in it, but
// no production caller sets it — `eshu report capture` never touches it, so it
// is always defaultFactRefsUnavailableReason, a package constant. Give it a
// reporter-typed route and it belongs in this domain.
//
// Code, Capability and Profiles are NOT scanned. Each is a server-side constant
// — an ErrorCode enum value, a capability name declared as a package const, and
// a QueryProfile pair — with no route from caller input, so there is nothing in
// them for a scan to find.
//
// Why it exists: ReporterNote used to be assigned verbatim, and the
// documentation asks reporters to describe how they reproduced the wrong
// answer (docs/public/guides/report-wrong-answer.md), so the realistic note is
// a pasted curl carrying the reporter's own credential. The bytes
// "next=/api/v0/x?api_key=sk-live" were REJECTED in Query.Target and ACCEPTED
// in ReporterNote — the same text the same person typed, two different
// verdicts, decided by which field held it. The note is reporter-typed input,
// so by this package's provenance rule it belongs in the same domain as
// Query.Target and Query.Params; only the shape of the text differs.
//
// Free text is not a query string, so this cannot reuse embeddedSensitiveKey:
// that function splits on query separators and skips any candidate key holding
// whitespace, which is right for a parsed parameter value and wrong for prose.
// urlredact.FreeText scans line by line for the two shapes a pasted command
// actually uses, and its doc comment carries the rules and the deliberate
// limits.
//
// The extra predicate widens the walk to this package's inline-content keys, so
// an "excerpt" pair in a note is removed here even though it is not a
// credential. urlredact ORs it with collector.IsSensitiveKeyName rather than
// letting it replace the predicate, so this package can add a name and can
// never drop one.
//
// A credential carried as a QUERY PARAMETER is found here. The "key=value" rule
// matches anywhere on the line, so a message or note holding
// "https://host/mcp?token=<credential>" keeps the URL and loses the pair.
// evidredact.Endpoint does the same for a structured endpoint field.
func redactFreeText(text string) (string, bool) {
	return urlredact.FreeText(text, isInlineContentKey)
}
