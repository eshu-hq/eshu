// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/replay"
)

// defaultFactRefsUnavailableReason is recorded when a caller does not supply
// resolved fact references. Per the Slice 1 plan, no public fact-record read
// route exists today (checked against go/internal/query/openapi_paths_*.go),
// so a remote capture cannot resolve FactRefs itself; resolution then happens
// maintainer-side in a later slice via the ifa.FactLoader seam.
const defaultFactRefsUnavailableReason = "no public fact-record read surface"

// CaptureInput is the caller-supplied material Capture composes into a
// Bundle. Capture does not perform network calls, MCP invocations, or fact
// store reads itself — callers (the `eshu report capture` CLI command in
// Slice 1) own resolving the query.ResponseEnvelope and any evidence
// hydration; Capture's job is exactly composition, redaction, digesting, and
// the fail-closed validation gate.
type CaptureInput struct {
	// Surface is "api" or "mcp".
	Surface string
	// Target is the endpoint path (no query string) or MCP tool name.
	Target string
	Method string
	// Params is the query/body parameters AS ISSUED — Capture redacts them;
	// callers must not pre-redact (redaction rules must run exactly once).
	Params map[string]any
	// Profile is the query profile in effect at capture time, when known.
	Profile string

	ReporterNote string

	// Envelope is the query.ResponseEnvelope returned by the query. Truth is
	// stored verbatim; Data is redacted before it is stored.
	Envelope query.ResponseEnvelope
	// Truncated is the observed read-model truncation flag found in the
	// response data (for example AnswerPacket.Truncated), supplied by the
	// caller since truncation is not part of the envelope contract itself.
	Truncated bool

	// Citations are share-safe citation references. Capture does not strip
	// an Excerpt field from these because CitationRef has none by
	// construction — see PayloadExcerpts for the private-triage path.
	Citations []CitationRef
	// FactRefs are resolved fact references, when the caller could hydrate
	// them (for example a local capture with durable-store access). When
	// empty, Capture records FactRefsState "unavailable" with
	// FactRefsReason defaulting to defaultFactRefsUnavailableReason unless
	// FactRefsReason is set explicitly.
	FactRefs       []FactRef
	FactRefsReason string

	// IncludePayloads flips the bundle's redaction profile to
	// ProfilePrivateTriage and attaches PayloadExcerpts/PayloadFacts
	// verbatim (unredacted) under PayloadAttachment. Every other section of
	// the bundle is still redacted and re-validated.
	IncludePayloads bool
	PayloadExcerpts []CitationExcerpt
	PayloadFacts    []facts.Envelope
}

// nowRFC3339UTC is a seam for deterministic tests; production callers use the
// default which reads the real clock.
var nowRFC3339UTC = func() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// Capture builds, redacts, digests, and validates a wrong_answer_report.v1
// Bundle from a CaptureInput. It returns an error — refusing to produce a
// bundle — if the finished artifact fails Validate, which is the fail-closed
// posture the Slice 1 plan requires: a capture tool must not silently emit a
// bundle that trips its own share-safe gate.
func Capture(input CaptureInput) (Bundle, error) {
	redactedParams, paramRules := redactValue(copyParams(input.Params))
	// copyParams always yields a map[string]any and redactValue's map branch
	// returns a map[string]any, so this holds by construction; the checked
	// assertion fails loudly rather than silently substituting nil params if a
	// future change to either function ever breaks that invariant.
	redactedParamsMap, ok := redactedParams.(map[string]any)
	if !ok {
		return Bundle{}, fmt.Errorf("internal: redacted params are %T, want map[string]any", redactedParams)
	}

	redactedData, dataRules := redactValue(input.Envelope.Data)
	dataRaw, err := json.Marshal(redactedData)
	if err != nil {
		return Bundle{}, fmt.Errorf("marshal redacted response data: %w", err)
	}
	digest, err := canonicalDigest(redactedData)
	if err != nil {
		return Bundle{}, fmt.Errorf("digest redacted response data: %w", err)
	}

	rules := dedupeSorted(append(append([]string{}, paramRules...), dataRules...))

	factRefsState := "unavailable"
	factRefsReason := input.FactRefsReason
	if len(input.FactRefs) > 0 {
		factRefsState = "resolved"
		factRefsReason = ""
	} else if factRefsReason == "" {
		factRefsReason = defaultFactRefsUnavailableReason
	}

	profile := ProfilePublic
	var payloads *PayloadAttachment
	if input.IncludePayloads {
		profile = ProfilePrivateTriage
		payloads = &PayloadAttachment{
			Warning:  payloadAttachmentWarning,
			Excerpts: input.PayloadExcerpts,
			Facts:    input.PayloadFacts,
		}
	}

	citations := input.Citations
	if citations == nil {
		citations = []CitationRef{}
	}
	factRefs := input.FactRefs
	if factRefs == nil {
		factRefs = []FactRef{}
	}

	bundle := Bundle{
		SchemaVersion: SchemaVersion,
		CreatedAt:     nowRFC3339UTC(),
		ReporterNote:  input.ReporterNote,
		Query: CapturedQuery{
			Surface: input.Surface,
			Target:  input.Target,
			Method:  input.Method,
			Params:  emptyMapIfNil(redactedParamsMap),
			Profile: input.Profile,
		},
		Response: CapturedResponse{
			Truth:      input.Envelope.Truth,
			Error:      input.Envelope.Error,
			Truncated:  input.Truncated,
			Data:       json.RawMessage(dataRaw),
			DataDigest: digest,
		},
		Evidence: EvidenceContext{
			Citations:      citations,
			FactRefs:       factRefs,
			FactRefsState:  factRefsState,
			FactRefsReason: factRefsReason,
		},
		Redaction: RedactionProfile{
			Profile: profile,
			Rules:   rules,
		},
		Payloads: payloads,
		Validation: Validation{
			Status: "passed",
			Checks: append([]string(nil), ValidationChecks...),
		},
	}

	bundleID, err := computeBundleID(bundle)
	if err != nil {
		return Bundle{}, fmt.Errorf("compute bundle_id: %w", err)
	}
	bundle.BundleID = bundleID

	if profile == ProfilePrivateTriage {
		bundle.Validation.Status = "waived_for_payload_attachment"
		bundle.Validation.Checks = append(bundle.Validation.Checks, "payload_attachment_excluded_from_share_safe_gate")
	}

	if err := Validate(bundle, ValidateOptions{}); err != nil {
		return Bundle{}, fmt.Errorf("captured bundle failed its own share-safe validation gate (refusing to emit): %w", err)
	}
	return bundle, nil
}

// canonicalDigest returns the hex sha256 of replay.CanonicalizeValue applied
// to value with the zero-value CanonicalOptions (sorted object keys only, no
// volatile/derived substitution — see replay/canonical.go:38-39). Response
// data is not a fact-envelope cassette, so the fact-envelope defaults
// (DefaultCanonicalOptions) do not apply; a report bundle's digest must
// reflect the actual captured value, not a fixture-normalized one.
func canonicalDigest(value any) (string, error) {
	canonical, err := replay.CanonicalizeValue(value, replay.CanonicalOptions{})
	if err != nil {
		return "", fmt.Errorf("canonicalize value for digest: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// computeBundleID returns the hex sha256 of the bundle's own canonical JSON
// content with BundleID cleared (it cannot include itself). The result is
// deterministic for identical content; CreatedAt participates, so two
// captures of the same query/response at different times get distinct ids by
// design.
func computeBundleID(bundle Bundle) (string, error) {
	bundle.BundleID = ""
	raw, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("marshal bundle for bundle_id: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("decode bundle for bundle_id: %w", err)
	}
	return canonicalDigest(doc)
}

func copyParams(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	return out
}

func emptyMapIfNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func dedupeSorted(rules []string) []string {
	seen := make(map[string]struct{}, len(rules))
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		if _, ok := seen[rule]; ok {
			continue
		}
		seen[rule] = struct{}{}
		out = append(out, rule)
	}
	sort.Strings(out)
	return out
}
