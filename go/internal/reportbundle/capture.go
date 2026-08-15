// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
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

	// ReporterNote is the reporter's own description of what they expected.
	// Capture scans it — it is reporter-typed input like Params, and the guide
	// asks for a repro, so it commonly holds a pasted curl. Callers must not
	// pre-redact it; see redactFreeText for what the scan does and does not
	// find.
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
	targetPath, targetParams, err := SplitTargetQuery(input.Target)
	if err != nil {
		// Refusing, rather than emptying the query. Nothing can separate a
		// secret from the rest of an unparseable blob, and the CLI derives the
		// request URL from this same split — a "redact the whole query" path
		// would issue a different request than the reporter ran and then file
		// the answer to it as their bug report.
		return Bundle{}, fmt.Errorf("query.target is unusable: %w (fix the endpoint and recapture; an unparseable query string cannot be redacted parameter by parameter)", err)
	}

	// Merge every query-input source FIRST, then scan the result once. Scanning
	// the target's parameters before the merge left CaptureInput.Params — the
	// --params flag and every programmatic caller — with no embedded-key scan
	// at all. One walk after the merge makes that gap unreachable by
	// construction instead of by remembering to add another pre-step.
	redactedParams, paramRules := redactQueryInput(mergeTargetParams(input.Params, targetParams))
	// copyParams always yields a map[string]any and redactQueryInput's map
	// branch returns a map[string]any, so this holds by construction; the checked
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

	redactedError, errorRules := redactErrorEnvelope(input.Envelope.Error)

	// The note is reporter-typed input like the parameters above, just free text
	// rather than a parsed query string — see redactFreeText.
	reporterNote, noteRedacted := redactFreeText(input.ReporterNote)
	var noteRules []string
	if noteRedacted {
		noteRules = []string{reporterNoteRule}
	}

	rules := dedupeSorted(append(append(append(append([]string{}, paramRules...), dataRules...), errorRules...), noteRules...))

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
		ReporterNote:  reporterNote,
		Query: CapturedQuery{
			Surface: input.Surface,
			Target:  targetPath,
			Method:  input.Method,
			Params:  emptyMapIfNil(redactedParamsMap),
			Profile: input.Profile,
		},
		Response: CapturedResponse{
			Truth:      input.Envelope.Truth,
			Error:      redactedError,
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

// SplitTargetQuery separates a target into its bare path and the parameters
// carried in its query string, decoded into the same map[string]any shape
// Params uses so both go through one redaction walk.
//
// This exists because every redaction rule in this package matches on object
// KEY names, and the fail-closed gate (collector.ValidateShareSafeKeys) does
// too — neither ever inspects a string value. Target is a plain string, and
// `eshu report capture` puts whatever follows --endpoint into it verbatim, so
// `--endpoint "/path?api_key=sk-live-..."` used to place a live credential in a
// bundle stamped "public" that passed its own validation. Splitting the query
// string out converts those values back into keys, where the redactor can see
// them.
//
// The parameters are kept rather than discarded: a maintainer reproducing a
// wrong answer needs to know what was asked, and a bundle that silently drops
// half its inputs is a worse report. Only the sensitive-named ones are removed,
// by the same walk that handles Params.
//
// A query string net/url cannot parse is an ERROR, not an empty result. The
// first version of this function returned no parameters in that case, which
// looked conservative and was the opposite: the credential stayed in the target
// string while Validate, finding no parameters to judge, reported nothing
// sensitive and let `--require-public` accept the bundle. Callers fail closed
// on the error instead — see Capture and validateTargetQuery.
//
// The parse is faithful: sensitive parameters are returned like any other, and
// deciding what may be recorded is the caller's job. `eshu report capture` needs
// the real parameters to issue the reporter's actual request; only the bundle
// is redacted.
//
// Exported so `eshu report capture` can apply the same split to the path it
// issues the HTTP request against, keeping the request URL and the recorded
// bundle derived from one function instead of two rules that drift.
func SplitTargetQuery(target string) (string, map[string]any, error) {
	path, rawQuery, found := strings.Cut(target, "?")
	if !found || strings.TrimSpace(rawQuery) == "" {
		return path, nil, nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		// The target is NOT interpolated here. See the package error rule in
		// doc.go: an error names the field, never the value, because these
		// errors reach terminals, CI logs, and pasted bug reports — the same
		// places the bundle is redacted for. url.ParseQuery's own error is
		// wrapped because its two shapes are safe to repeat: url.EscapeError
		// quotes the three-byte escape token only, and the semicolon case is a
		// fixed sentence. The full-egress canary, not that argument, is what
		// keeps it true.
		return path, nil, fmt.Errorf("parse query string of query.target: %w", err)
	}
	params := make(map[string]any, len(values))
	for key, list := range values {
		switch len(list) {
		case 0:
			continue
		case 1:
			params[key] = list[0]
		default:
			repeated := make([]any, len(list))
			for i, item := range list {
				repeated[i] = item
			}
			params[key] = repeated
		}
	}
	return path, params, nil
}

// mergeTargetParams folds query-string parameters into a copy of the
// caller-supplied params. An explicitly supplied parameter wins on a name
// collision: it is the more deliberate input, and overwriting it would make the
// bundle misreport what was asked.
func mergeTargetParams(params, fromTarget map[string]any) map[string]any {
	merged := copyParams(params)
	for key, value := range fromTarget {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	return merged
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

// redactErrorEnvelope redacts an error envelope in two domains and returns a
// copy, so the caller's envelope is never mutated: Details goes through
// redactQueryInput (the query-input walk, not the key-name-only one Data gets),
// and the free-text fields Message and CorrelationID go through redactFreeText.
//
// Details belongs to the query-input domain because it echoes the reporter's
// own selectors back: query/service_story_seam.go:131 puts the caller's
// service selector into details.selector on an ambiguous match. That is the
// reporter's typed string arriving by a third route, so it takes the same
// treatment the other two do. Details is also structured diagnostic metadata
// rather than answer content, so scanning it costs none of the evidence the
// Data exemption protects.
//
// Before this, Response.Error was copied into the bundle verbatim. That was
// safe but only in the all-or-nothing sense: a sensitive-shaped key inside
// Details would fail the final Validate gate and Capture would refuse to emit
// a bundle at all. Every other field redacts and continues, so a user whose
// error happened to carry a "token" key got no bundle instead of a redacted
// one, and nothing tested the difference (#5059).
//
// Message and CorrelationID were once left alone, on the recorded belief that
// they were "fixed contract fields the redactor has no key names to work with,
// and Validate still covers them". Both halves were wrong. Message is composed:
// query/service_workload_resolution.go:39 formats the caller's service selector
// into it, and query/service_story_seam.go:129 emits that sentence beside a
// details.selector holding the identical string — so the redactor DROPPED the
// selector from Details, on rule "selector", and kept it verbatim one field over
// in a bundle stamped profile=public / validation=passed. `redacted := *envelope`
// is what carries it: the struct copy takes every scalar field along and only
// Details was ever walked afterwards. And Validate did not cover them — before
// this change validate.go named Message nowhere at all.
//
// Code, Capability and Profiles are still copied unscanned, and that is not the
// same claim: each is a server-side constant (an ErrorCode enum value, a
// capability name declared as a package const, a QueryProfile pair) with no
// route from caller input. See redactFreeText for why CorrelationID is not one
// of them.
//
// Every non-nil Details map is copied, including an empty one. Skipping the
// copy when there is nothing to redact looks free and is not: the bundle would
// then hold the caller's own map, and error envelopes are commonly filled in
// after construction. A key added later would land in a bundle that already
// passed Validate. Nil stays nil so `details` keeps being omitted from the
// serialized error.
func redactErrorEnvelope(envelope *query.ErrorEnvelope) (*query.ErrorEnvelope, []string) {
	if envelope == nil {
		return nil, nil
	}
	redacted := *envelope
	var rules []string
	if message, changed := redactFreeText(envelope.Message); changed {
		redacted.Message = message
		rules = append(rules, errorMessageRule)
	}
	if correlationID, changed := redactFreeText(envelope.CorrelationID); changed {
		redacted.CorrelationID = correlationID
		rules = append(rules, errorCorrelationIDRule)
	}
	if envelope.Details == nil {
		redacted.Details = nil
		return &redacted, rules
	}

	value, detailRules := redactQueryInput(copyParams(envelope.Details))
	rules = append(rules, detailRules...)
	details, ok := value.(map[string]any)
	if !ok {
		// copyParams yields a map and redactQueryInput's map branch returns one, so
		// this cannot happen today. Drop the details rather than pass through
		// an unredacted value if that ever stops being true: losing diagnostic
		// context is recoverable, leaking is not. The redacted scalars above are
		// kept — building a fresh envelope out of the ORIGINAL fields here is how
		// a fix applied at the top of this function would quietly not apply on
		// this branch.
		redacted.Details = nil
		return &redacted, rules
	}
	redacted.Details = details
	return &redacted, rules
}
