// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// SchemaVersion is the stable schema identifier for wrong-answer report
// bundles. A Bundle whose SchemaVersion does not match this constant fails
// Validate closed, mirroring evidencebundle's schema check
// (evidencebundle/validate.go:14-15).
const SchemaVersion = "wrong_answer_report.v1"

// Redaction profile names. ProfilePublic is the default, share-safe posture:
// no fact payload bytes, no citation excerpts, sensitive-named keys removed.
// ProfilePrivateTriage is the --include-payloads opt-in posture: the
// rest of the bundle stays redacted, but a PayloadAttachment carrying raw
// excerpts and resolved fact envelopes is appended for local, non-public
// triage only.
const (
	ProfilePublic        = "public"
	ProfilePrivateTriage = "private-triage"
)

// payloadAttachmentWarning is the fixed, loud sentence every PayloadAttachment
// carries as its first serialized field, so a bundle viewed out of context
// (for example pasted into a chat) still states its own private-triage-only
// posture.
const payloadAttachmentWarning = "PRIVATE TRIAGE ONLY: this section contains raw fact payloads and citation excerpts. Do not attach a bundle with this section to a public issue or share it outside your own triage workflow."

// Bundle is the wrong_answer_report.v1 artifact `eshu report capture` writes
// and `eshu report validate` checks. It packages a redacted, share-safe
// snapshot of one query/response pair plus the evidence trail a maintainer
// needs to reproduce and later convert the report into an Ifá Odù
// conformance case (Slice 2), without ever requiring fact payload bytes to
// leave the reporter's machine by default.
type Bundle struct {
	SchemaVersion string           `json:"schema_version"`
	BundleID      string           `json:"bundle_id"`
	CreatedAt     string           `json:"created_at"`
	ReporterNote  string           `json:"reporter_note"`
	Query         CapturedQuery    `json:"query"`
	Response      CapturedResponse `json:"response"`
	Evidence      EvidenceContext  `json:"evidence"`
	Redaction     RedactionProfile `json:"redaction"`
	// Payloads is nil unless the bundle was captured with --include-payloads.
	// It is the ONLY part of a Bundle allowed to carry raw fact payload bytes
	// or citation excerpts; see PayloadAttachment.
	Payloads   *PayloadAttachment `json:"payloads,omitempty"`
	Validation Validation         `json:"validation"`
}

// CapturedQuery records what was asked: the surface, target, and parameters
// of the query that produced the wrong answer. Params is redacted by
// key-name — see redact.go — before it is ever assigned onto a Bundle.
type CapturedQuery struct {
	// Surface is "api" or "mcp".
	Surface string `json:"surface"`
	// Target is the endpoint path (no query string) or MCP tool name.
	Target string `json:"target"`
	Method string `json:"method,omitempty"`
	// Params is the query/body parameters as issued, with every
	// sensitive-named key removed entirely (see redact.go's design note on
	// why removal, not a same-key masked-value marker, is what keeps this
	// bundle from tripping its own Validate gate). Redaction.Rules records
	// which key names were stripped.
	Params map[string]any `json:"params"`
	// Profile is the query profile in effect at capture time (for example
	// local_authoritative), when known.
	Profile string `json:"profile,omitempty"`
}

// CapturedResponse records what came back: the verbatim truth envelope, any
// error envelope, observed truncation, and the redacted response data plus its
// replay-equality digest.
type CapturedResponse struct {
	// Truth is the query.TruthEnvelope returned by the query, stored
	// verbatim (contract.go:97-105) — never summarized or re-derived.
	Truth *query.TruthEnvelope `json:"truth"`
	Error *query.ErrorEnvelope `json:"error,omitempty"`
	// Truncated is the observed read-model truncation flag found in the
	// captured data (for example AnswerPacket.Truncated); it is not part of
	// the envelope contract itself.
	Truncated bool `json:"truncated"`
	// Data is the response data AFTER the key-name redaction walk.
	Data json.RawMessage `json:"data"`
	// DataDigest is the sha256 of replay.CanonicalizeValue applied to Data
	// (the same, already-redacted value) — the replay-equality anchor a
	// Slice 2 converter/replay proof compares against.
	DataDigest string `json:"data_digest"`
}

// EvidenceContext carries the citation and fact-reference trail for the
// captured response. FactRefs are references only — fact_id, stable key,
// kind, scope, and generation — never inline payload bytes.
type EvidenceContext struct {
	Citations []CitationRef `json:"citations"`
	FactRefs  []FactRef     `json:"fact_refs"`
	// FactRefsState is "resolved" when FactRefs were hydrated at capture time,
	// or "unavailable" when no public fact-record read surface let the CLI
	// resolve them (the common remote-capture case in Slice 1); see
	// FactRefsReason.
	FactRefsState  string `json:"fact_refs_state"`
	FactRefsReason string `json:"fact_refs_reason,omitempty"`
}

// CitationRef is a share-safe citation reference: the query.EvidenceCitationHandle
// handle fields (evidence_citation.go:33-43) plus resolved citation_id,
// content_hash, and commit_sha. Excerpt — inline content bytes
// (evidence_citation.go:75) — is deliberately NOT a field here; it is dropped
// from every public-profile bundle and only ever appears, verbatim, inside
// PayloadAttachment.Excerpts under --include-payloads.
type CitationRef struct {
	Kind           string  `json:"kind,omitempty"`
	RepoID         string  `json:"repo_id,omitempty"`
	RelativePath   string  `json:"relative_path,omitempty"`
	EntityID       string  `json:"entity_id,omitempty"`
	EvidenceFamily string  `json:"evidence_family,omitempty"`
	Reason         string  `json:"reason,omitempty"`
	StartLine      int     `json:"start_line,omitempty"`
	EndLine        int     `json:"end_line,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`
	CitationID     string  `json:"citation_id,omitempty"`
	ContentHash    string  `json:"content_hash,omitempty"`
	CommitSHA      string  `json:"commit_sha,omitempty"`
}

// FactRef is a reference to one durable fact envelope, mirroring the field
// names ifa.renderFact uses for the Odù fact render (odu.go:118-134) so a
// Slice 2 converter can resolve these refs through the same ifa.FactLoader
// seam without a field-name translation layer. It never carries Payload.
type FactRef struct {
	FactID        string `json:"fact_id"`
	StableFactKey string `json:"stable_fact_key"`
	FactKind      string `json:"fact_kind"`
	SchemaVersion string `json:"schema_version,omitempty"`
	ScopeID       string `json:"scope_id"`
	GenerationID  string `json:"generation_id"`
}

// RedactionProfile records the share-safe policy applied before
// serialization: ProfilePublic or ProfilePrivateTriage, plus the sorted,
// de-duplicated list of sensitive key names the redaction walk actually
// removed (empty when nothing needed redaction).
type RedactionProfile struct {
	Profile string   `json:"profile"`
	Rules   []string `json:"rules"`
}

// PayloadAttachment is the PRIVATE-TRIAGE-ONLY section a Bundle carries when
// captured with --include-payloads. Warning is always the first field
// serialized and always carries payloadAttachmentWarning, so the section is
// self-describing even out of context.
type PayloadAttachment struct {
	Warning  string            `json:"warning"`
	Excerpts []CitationExcerpt `json:"excerpts,omitempty"`
	Facts    []facts.Envelope  `json:"facts,omitempty"`
}

// CitationExcerpt pairs a CitationRef with the raw excerpt bytes it
// identifies. It exists ONLY inside PayloadAttachment; a public-profile
// Bundle never constructs one.
type CitationExcerpt struct {
	CitationRef
	Excerpt string `json:"excerpt"`
}

// Validation records the deterministic checks Validate ran against the
// finished bundle and their outcome.
type Validation struct {
	Status string   `json:"status"`
	Checks []string `json:"checks"`
}
