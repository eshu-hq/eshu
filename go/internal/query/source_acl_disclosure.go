// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Source-ACL disclosure (#2164 enforcement/disclosure tail, USER-APPROVED policy).
//
// The additive surfacing slice (#2177) exposed the bounded source_acl_state
// (allowed|denied|partial|missing|stale) verbatim on readback rows without
// changing any returned row or content. This module is the enforcement and
// disclosure layer the maintainer approved on top of it: it maps the bounded
// access-posture axis onto an honest, bounded access disposition and, for any
// posture that is not cleanly readable, withholds the protected content while
// still disclosing that evidence exists.
//
// Design invariants:
//   - Fail-closed: a denied or partial source never has its protected content,
//     excerpt, or text surfaced. The disclosure exposes only the bounded STATE
//     (the disposition + the bounded source_acl_state), never the content.
//   - Distinct axis (#2138): source_acl_state and access_disposition are kept
//     separate from the freshness/truth taxonomy. The freshness_state,
//     truth_level, truth_basis, status, missing_evidence, unsupported_reason,
//     admission_state, and the other #2138 labels are preserved on a withheld
//     row and never collapsed into a permission error.
//   - Conservative: a non-allowed observation is never upgraded to allowed and
//     no disposition is synthesized for a source the collector did not observe.
const (
	// accessDispositionResponseKey is the wire field carrying the bounded
	// access disposition computed from the source-ACL posture. It is a distinct
	// axis kept separate from freshness_state/truth_level (#2138).
	accessDispositionResponseKey = "access_disposition"
	// permissionDeniedResponseKey is the wire flag set when the caller is
	// authenticated-but-not-authorized for the source (denied posture).
	permissionDeniedResponseKey = "permission_denied"
	// contentWithheldResponseKey is the wire flag set when protected content was
	// stripped from the row because the access posture is not cleanly readable.
	contentWithheldResponseKey = "content_withheld"
)

// Bounded access dispositions. These are low-cardinality, content-free labels
// safe to surface on the wire and to use as bounded telemetry dimensions.
const (
	// accessDispositionVisible marks a cleanly readable row: content intact.
	accessDispositionVisible = "visible"
	// accessDispositionDenied marks an access-denied row: content withheld,
	// permission_denied set. Evidence existence is disclosed; content is not.
	accessDispositionDenied = "access_denied"
	// accessDispositionPartial marks a partial-ACL row: content withheld behind
	// the bounded partial marker; the row is visible but its protected content
	// boundaries are respected.
	accessDispositionPartial = "partial"
	// accessDispositionStale marks a permitted-but-stale source revision. Stale
	// is a permitted read, so content stays visible; the row is surfaced as
	// stale on the distinct ACL axis (kept separate from freshness_state).
	accessDispositionStale = "stale"
	// accessDispositionMissing marks a source that was not found/deleted at the
	// origin. The row carries no content and is treated as empty by callers.
	accessDispositionMissing = "missing"
)

// sourceACLContentDisposition is the bounded outcome of evaluating a readback
// row's access posture. It drives both wire disclosure and bounded telemetry.
type sourceACLContentDisposition struct {
	// disposition is the bounded access disposition (visible/access_denied/
	// partial/stale/missing).
	disposition string
	// permissionDenied reports the caller is authenticated-but-not-authorized.
	permissionDenied bool
	// contentWithheld reports protected content must be stripped from the row.
	contentWithheld bool
}

// sourceACLDisclosureKeptKeys is the bounded allowlist of identity and bounded
// state/posture fields that survive on a content-withheld row. Everything not
// in this set is treated as protected content and dropped. An allowlist (rather
// than a denylist) guarantees that a content field added to a payload later can
// never leak through a denied/partial disclosure by default.
//
// The set deliberately includes the #2138 truth/freshness labels so a withheld
// row still reports those distinct axes honestly and never collapses them into
// the permission error.
var sourceACLDisclosureKeptKeys = map[string]struct{}{
	// Identity / addressing (no protected content).
	"fact_id":             {},
	"fact_kind":           {},
	"finding_id":          {},
	"finding_version":     {},
	"finding_type":        {},
	"packet_id":           {},
	"packet_version":      {},
	"source_id":           {},
	"document_id":         {},
	"section_id":          {},
	"scope_id":            {},
	"generation_id":       {},
	"repo":                {},
	"observed_at":         {},
	"observation_id":      {},
	"observation_type":    {},
	"hint_id":             {},
	"hint_type":           {},
	"relationship_kind":   {},
	"source_system":       {},
	"evidence_packet_url": {},
	// Bounded posture / truth axes (kept distinct, #2138 preserved).
	sourceACLStateResponseKey:    {},
	accessDispositionResponseKey: {},
	permissionDeniedResponseKey:  {},
	contentWithheldResponseKey:   {},
	"status":                     {},
	"truth_level":                {},
	"truth_basis":                {},
	"freshness_state":            {},
	"policy_state":               {},
	"redaction_state":            {},
	"admission_state":            {},
	"corroboration_state":        {},
	"promotion_policy":           {},
	"missing_evidence":           {},
	"unsupported_reason":         {},
}

// sourceACLDispositionFor maps a bounded source_acl_state and the binary
// per-caller read decision onto a bounded content disposition.
//
//   - binary-denied (the existing per-caller authorization gate said no) or a
//     bounded "denied" posture -> access_denied, permission_denied, content
//     withheld.
//   - partial -> partial marker, content withheld (boundaries respected).
//   - stale -> stale on the ACL axis; stale is a permitted read so content is
//     not withheld.
//   - missing -> missing; the row carries no content.
//   - allowed / no bounded claim with a clean binary decision -> visible.
//
// binaryReadable reports the existing per-caller visibility decision
// (viewer_can_read_source / permission_decision / source_acl_evaluated). It is
// the authoritative authorization axis; a bounded "allowed" observation never
// upgrades a binary-denied caller to readable.
func sourceACLDispositionFor(boundedState string, binaryReadable bool) sourceACLContentDisposition {
	if !binaryReadable {
		return sourceACLContentDisposition{
			disposition:      accessDispositionDenied,
			permissionDenied: true,
			contentWithheld:  true,
		}
	}
	switch boundedState {
	case facts.SourceACLStateDenied:
		return sourceACLContentDisposition{
			disposition:      accessDispositionDenied,
			permissionDenied: true,
			contentWithheld:  true,
		}
	case facts.SourceACLStatePartial:
		return sourceACLContentDisposition{
			disposition:     accessDispositionPartial,
			contentWithheld: true,
		}
	case facts.SourceACLStateStale:
		return sourceACLContentDisposition{disposition: accessDispositionStale}
	case facts.SourceACLStateMissing:
		return sourceACLContentDisposition{disposition: accessDispositionMissing}
	default:
		return sourceACLContentDisposition{disposition: accessDispositionVisible}
	}
}

// applySourceACLDisclosure enforces the approved disclosure policy on a single
// readback row in place. It surfaces the bounded source_acl_state (additive),
// computes the bounded access disposition from that posture and the binary
// per-caller read decision, and—when the posture is not cleanly readable—strips
// every field outside sourceACLDisclosureKeptKeys so no protected content is
// returned. It returns the computed disposition for bounded telemetry.
//
// It never drops the row: a denied/partial/stale source is disclosed as existing
// with its bounded state, honoring the issue requirement to not present a
// denied or out-of-grant result as clean "nothing found." A missing source is
// disclosed as missing (callers treat it as empty).
func applySourceACLDisclosure(row map[string]any, binaryReadable bool) sourceACLContentDisposition {
	state := rowBoundedSourceACLState(row)
	if state != "" {
		row[sourceACLStateResponseKey] = state
	}
	disp := sourceACLDispositionFor(state, binaryReadable)
	row[accessDispositionResponseKey] = disp.disposition
	if disp.permissionDenied {
		row[permissionDeniedResponseKey] = true
	}
	if disp.contentWithheld {
		row[contentWithheldResponseKey] = true
		withholdProtectedContent(row)
	}
	return disp
}

// withholdProtectedContent removes every field outside the bounded allowlist
// from the row in place, guaranteeing the protected content/excerpt is never
// returned for a denied or partial source. Nested objects (e.g. bounded_excerpt,
// payload, source) are dropped wholesale because they carry content.
func withholdProtectedContent(row map[string]any) {
	for key := range row {
		if _, kept := sourceACLDisclosureKeptKeys[key]; kept {
			continue
		}
		delete(row, key)
	}
}

// rowBoundedSourceACLState resolves the bounded source_acl_state for a readback
// row from any of the three shapes the readbacks produce, failing closed to the
// empty string ("no ACL claim") when none carries a bounded value:
//
//   - a top-level source_acl_state already surfaced onto a projected row
//     (semantic-evidence public rows project fields out of the raw payload);
//   - acl_summary.source_acl_state on the row itself (documentation findings and
//     packets carry the fact body at the top level);
//   - acl_summary.source_acl_state on a nested "payload" body (fact-wrapped
//     readbacks).
func rowBoundedSourceACLState(row map[string]any) string {
	if state := stringFromMap(row, sourceACLStateResponseKey); facts.ValidSourceACLState(state) {
		return state
	}
	if state := boundedSourceACLState(row); state != "" {
		return state
	}
	return boundedSourceACLState(payloadSubmap(row))
}

// payloadSubmap returns the nested "payload" object on a row, or nil. Some
// readbacks (semantic evidence, documentation facts) wrap the fact body under a
// "payload" key; the bounded ACL claim lives on the body's acl_summary.
func payloadSubmap(row map[string]any) map[string]any {
	nested, _ := row["payload"].(map[string]any)
	return nested
}

// binaryReadableFromPermissions reports the existing per-caller documentation
// read decision for a payload, reusing the same visibility predicate the SQL
// fail-closed filter encodes (viewer_can_read_source true, source_acl_evaluated
// not false, permission_decision not denied). It is the authorization axis the
// disclosure layer must honor; the bounded source_acl_state never overrides it.
func binaryReadableFromPermissions(payload map[string]any) bool {
	return documentationVisibilityDecision(payload).allowed
}

// boundedAccessDisposition reports the bounded disposition string for a posture,
// without mutating any row. It is used where a handler needs the disposition for
// an HTTP/MCP status decision (e.g. the single-packet readback).
func boundedAccessDisposition(boundedState string, binaryReadable bool) string {
	return strings.TrimSpace(sourceACLDispositionFor(boundedState, binaryReadable).disposition)
}

// Bounded span-attribute keys for source-ACL disclosure observability. These
// record per-readback counts by disposition only; they carry no source id,
// path, title, url, or user identity, satisfying the low-cardinality contract.
const (
	spanAttrACLDisclosureVisible  = "eshu.query.source_acl.visible_count"
	spanAttrACLDisclosureDenied   = "eshu.query.source_acl.access_denied_count"
	spanAttrACLDisclosurePartial  = "eshu.query.source_acl.partial_count"
	spanAttrACLDisclosureStale    = "eshu.query.source_acl.stale_count"
	spanAttrACLDisclosureMissing  = "eshu.query.source_acl.missing_count"
	spanAttrACLDisclosureWithheld = "eshu.query.source_acl.content_withheld_count"
)

// sourceACLDisclosureCounts accumulates bounded per-disposition counts for one
// readback so an operator can see, at 3 AM, how many rows were withheld or
// surfaced as denied/partial/stale without any high-cardinality dimension. It
// is recorded as span attributes (high-cardinality-safe location per the
// telemetry contract), never as metric labels.
type sourceACLDisclosureCounts struct {
	visible  int
	denied   int
	partial  int
	stale    int
	missing  int
	withheld int
}

// record folds one row's disposition into the bounded counts.
func (c *sourceACLDisclosureCounts) record(disposition string) {
	switch disposition {
	case accessDispositionVisible:
		c.visible++
	case accessDispositionDenied:
		c.denied++
		c.withheld++
	case accessDispositionPartial:
		c.partial++
		c.withheld++
	case accessDispositionStale:
		c.stale++
	case accessDispositionMissing:
		c.missing++
	}
}

// annotateSpan writes the bounded disposition counts onto the readback span.
func (c sourceACLDisclosureCounts) annotateSpan(span trace.Span) {
	if span == nil {
		return
	}
	span.SetAttributes(
		attribute.Int(spanAttrACLDisclosureVisible, c.visible),
		attribute.Int(spanAttrACLDisclosureDenied, c.denied),
		attribute.Int(spanAttrACLDisclosurePartial, c.partial),
		attribute.Int(spanAttrACLDisclosureStale, c.stale),
		attribute.Int(spanAttrACLDisclosureMissing, c.missing),
		attribute.Int(spanAttrACLDisclosureWithheld, c.withheld),
	)
}
