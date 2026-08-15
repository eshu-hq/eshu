// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// ValidateOptions controls the strictness Validate applies. RequirePublic
// mirrors `eshu report validate --require-public`: it rejects any bundle whose
// Redaction.Profile is not ProfilePublic, which is what the wrong-answer issue
// template instructs maintainers to run before accepting an attachment.
type ValidateOptions struct {
	RequirePublic bool
}

// ValidationChecks names, in order, the checks Validate runs on every bundle
// under default ValidateOptions — exactly as Capture invokes it. It is the
// single source of truth for the Bundle.Validation.Checks metadata so that
// recorded list cannot silently drift from Validate's actual behavior: add a
// check to Validate and add its name here in the same change (guarded by
// TestValidationChecksMatchValidateBehavior). The caller-side --require-public
// posture (opts.RequirePublic) is a conditional gate a producer does not run,
// so it is intentionally not part of the capture-time record.
var ValidationChecks = []string{
	"schema_version",
	"bundle_id",
	"profile_payloads_consistency",
	"query_inputs",
	"reporter_note",
	"response_error_text",
	"share_safe_keys",
}

// validateReporterNote rejects a bundle whose reporter_note still carries a
// credential-shaped pair. It calls redactFreeText, the same function
// Capture calls, and rejects when that function would have changed something —
// so the two cannot drift. A separate predicate written to "mirror" the
// redactor is how the query-input half went wrong the first time: Capture was
// widened, Validate was not, and a hand-edited bundle passed every check.
//
// Validate needs this for the reason it re-checks the query inputs: a
// maintainer runs --require-public against a file somebody else sent, which may
// have been hand-edited, written by another tool, or produced before this scan
// existed.
func validateReporterNote(bundle Bundle) error {
	if _, redacted := redactFreeText(bundle.ReporterNote); redacted {
		return fmt.Errorf("reporter_note carries a credential-shaped key=value or header pair: free text is never inspected by the share-safe key walk, so it reached the bundle unredacted; recapture the bundle instead of editing it")
	}
	return nil
}

// validateResponseErrorText rejects a bundle whose error envelope still carries
// a credential-shaped pair in one of its free-text fields. Like
// validateReporterNote it calls the redactor rather than a predicate written to
// mirror it, so the two cannot drift.
//
// It exists for the reason the whole Validate half exists — a maintainer runs
// --require-public against a file somebody else sent — plus one specific to
// these fields: a bundle captured before the Message scan existed passed its own
// Capture-time gate and is stamped validation=passed, so the recorded stamp is
// not evidence about the fields. Validate has to look for itself.
//
// Only the FIELD is named, never the text. These messages land in terminals and
// CI logs, which is the same egress the bundle is redacted for; quoting the
// message back would re-leak exactly what the check just caught.
func validateResponseErrorText(bundle Bundle) error {
	if bundle.Response.Error == nil {
		return nil
	}
	var offending []string
	if _, redacted := redactFreeText(bundle.Response.Error.Message); redacted {
		offending = append(offending, "response.error.message")
	}
	if _, redacted := redactFreeText(bundle.Response.Error.CorrelationID); redacted {
		offending = append(offending, "response.error.correlation_id")
	}
	if len(offending) == 0 {
		return nil
	}
	return fmt.Errorf("%s carries a credential-shaped key=value or header pair: the share-safe key walk never inspects string values, and a composed error message interpolates the caller's own selector, so it reached the bundle unredacted; recapture the bundle instead of editing it", strings.Join(offending, ", "))
}

// validateQueryInputs rejects a bundle whose reporter-typed query inputs still
// carry a credential the share-safe key walk cannot see. It covers the same
// domain redactQueryInput redacts: Query.Target's query string, every string
// inside Query.Params at any depth, and Response.Error.Details — which sits in
// the Response section but holds the reporter's own selector echoed back, so it
// belongs to this domain rather than to the server-produced evidence Data
// carries.
//
// The share-safe walk below judges object key names and never reads a string
// value, so a credential sitting inside any of these strings is invisible to
// it. Capture routes all of them through one redaction walk before it builds a
// bundle, but Validate is what a maintainer points at a file somebody sent
// them — hand-edited, written by a third-party tool, or produced by a build
// from before that walk existed. It has to find the leak without assuming
// Capture produced the input.
//
// It mirrors redactQueryInput deliberately, and covering only Target was a real
// hole rather than a theoretical one: a bundle carrying
// "params":{"next":"/x?api_key=sk-live"} passed every check this package had.
// Mirroring keeps the package's own invariant intact in both directions — a
// Capture-produced bundle always passes Validate, and anything Validate accepts
// is something Capture would have been willing to emit.
//
// Five ways the inputs fail, and all five are reached without trusting the
// producer:
//
//   - The target's query string does not parse. Returning "no sensitive
//     parameters found" for a string nobody could take apart is how a
//     credential gets waved through, so it is rejected.
//   - A target parameter's NAME is sensitive.
//   - A target parameter's VALUE embeds a sensitive-named key, which is what
//     happens when a second "?" pushes the credential inside a benign parameter
//     like "next" (see embeddedSensitiveKey).
//   - A Query.Params value at any depth embeds one the same way.
//   - A Response.Error.Details value does, which is how a selector the reporter
//     typed comes back through the error envelope.
//
// A sensitive param NAME inside Query.Params is left to the share-safe gate,
// which already walks the whole serialized document and reports the full key
// path. Duplicating it here would give the same bundle two different rejection
// messages depending on check order.
func validateQueryInputs(bundle Bundle) error {
	_, params, err := SplitTargetQuery(bundle.Query.Target)
	if err != nil {
		return fmt.Errorf("query.target carries a query string that cannot be parsed (%w): its parameters cannot be checked one by one, so it may hold an unredacted credential; recapture the bundle instead of editing it", err)
	}
	offending := make([]string, 0, len(params))
	for key, value := range params {
		if isRedactedKeyName(key) || carriesEmbeddedSensitiveKey(value) {
			offending = append(offending, safePathSegment(key))
		}
	}
	if len(offending) > 0 {
		sort.Strings(offending)
		return fmt.Errorf("query.target carries sensitive parameter(s) %s in its query string: the value is unredacted because the share-safe key walk never inspects string values; recapture the bundle instead of editing it", strings.Join(offending, ", "))
	}

	paths := embeddedCredentialPaths("query.params", bundle.Query.Params)
	if bundle.Response.Error != nil {
		paths = append(paths, embeddedCredentialPaths("response.error.details", bundle.Response.Error.Details)...)
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return fmt.Errorf("a sensitive key=value pair is embedded in the value of %s: the share-safe key walk never inspects string values, so it reached the bundle unredacted; recapture the bundle instead of editing it", strings.Join(paths, ", "))
	}
	return nil
}

// redactedPathSegment stands in for a map key this package will not repeat back
// to the user. It carries no "=" or ":" and no sensitive word, so a message
// containing it cannot trip this package's own scans or the share-safe gate.
const redactedPathSegment = "[redacted-key]"

// safePathSegment returns key when an error message may repeat it, and a fixed
// marker when it may not.
//
// A bundle's parameter names are reporter-typed data, not schema fields, and a
// name can BE the credential: url.ParseQuery percent-decodes key names, so
// `--endpoint '/x?api_key%3Dsk-live-...'` parses to one parameter whose name is
// literally "api_key=sk-live-...". Building a path out of that name and quoting
// it in a rejection put the credential in a terminal and a CI log — the same
// egress the bundle beside it was redacted for, and the same defect class this
// whole check exists to catch, one level up.
//
// The test is the package's usual one, asked about the key instead of the
// value: a key that is itself query-shaped and carries a sensitive-named pair
// is not repeated. A key that is merely sensitive-NAMED still is — "api_key" is
// a field name, not a credential, and naming it is the entire value of the
// message. So the limit is the usual one too: a credential written under a
// benign-looking name is still repeated. That is the same blind spot the
// redaction walk has, not a new one, and closing it would mean judging what a
// value looks like, which this package does not do.
func safePathSegment(key string) string {
	if _, found := embeddedSensitiveKey(key); found {
		return redactedPathSegment
	}
	return key
}

// embeddedCredentialPaths returns the dotted field paths, rooted at prefix,
// whose value embeds a sensitive-named key. It reports the path rather than the
// value, per the package error rule: the message has to be actionable in a
// terminal or a CI log without repeating the credential into one. Map keys go
// through safePathSegment, because a key is reporter-typed too.
//
// The check lives on the string leaf, not on the parent that holds it. Deciding
// at the parent stopped the path one level short for exactly the shape a
// repeated parameter takes: a list of strings reported
// "query.params.filters.redirect" and left a maintainer to read every element to
// find which one tripped, while the list branch below was already formatting
// indices for a list of objects. Judging the leaf makes both shapes report the
// same way, and names every offending element rather than only the first.
func embeddedCredentialPaths(prefix string, value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var paths []string
		for key, child := range typed {
			paths = append(paths, embeddedCredentialPaths(prefix+"."+safePathSegment(key), child)...)
		}
		return paths
	case []any:
		var paths []string
		for i, child := range typed {
			paths = append(paths, embeddedCredentialPaths(fmt.Sprintf("%s[%d]", prefix, i), child)...)
		}
		return paths
	case string:
		if _, found := embeddedSensitiveKey(typed); found {
			return []string{prefix}
		}
		return nil
	default:
		return nil
	}
}

// Validate checks a finished Bundle: schema_version fail-closed (mirroring
// evidencebundle/validate.go:14-15), a required bundle_id, profile↔payloads
// consistency, the three free-string scans (query_inputs, reporter_note and
// response_error_text, all of which read string VALUES the key-name gate cannot
// see), the --require-public posture when requested, and the share-safe
// key-name gate — collector.ValidateShareSafeKeys applied to the whole bundle
// document except the PayloadAttachment section, which is the one place a
// private-triage bundle is allowed to carry raw excerpt/fact bytes under
// --include-payloads. A public-profile bundle that trips the gate is a bug:
// Capture calls Validate before returning, so `eshu report capture` refuses to
// write such a bundle rather than silently emitting it.
//
// The Payloads section is stripped from the share-safe walk ONLY for a
// private-triage bundle, where it is legitimately present. A public-profile
// bundle carrying a non-nil Payloads section is a redaction-safety violation
// (raw excerpt/fact bytes under a "share-safe" label) and is REJECTED rather
// than stripped-then-passed — otherwise `eshu report validate --require-public`
// would green-light a bundle that leaks private payload bytes.
func Validate(bundle Bundle, opts ValidateOptions) error {
	if bundle.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: got %q want %q", bundle.SchemaVersion, SchemaVersion)
	}
	// Presence only — the id is deliberately NOT recomputed and compared.
	// Recomputing is cheap (Validate already marshals the whole bundle a few
	// lines down), but computeBundleID is deterministic and its inputs are all
	// in the file, so anyone who edits a bundle can restamp a matching id. It
	// would defeat no one while rejecting bundles whose only problem is age: a
	// capture from a build whose Bundle struct had one field fewer serializes
	// differently and would now fail as "corrupt". Share-safety is what this
	// gate is for, and a leaked credential is equally leaked under a correct id.
	if strings.TrimSpace(bundle.BundleID) == "" {
		return fmt.Errorf("bundle_id is required")
	}
	profile := bundle.Redaction.Profile
	switch profile {
	case ProfilePublic:
		// A share-safe bundle must not carry any raw payload attachment.
		if bundle.Payloads != nil {
			return fmt.Errorf("public redaction profile must not carry a payloads attachment: a payload section is only valid under the %q profile", ProfilePrivateTriage)
		}
	case ProfilePrivateTriage:
		// The profile exists solely to carry payloads; a private-triage
		// bundle with no attachment is mislabeled.
		if bundle.Payloads == nil {
			return fmt.Errorf("%q redaction profile requires a payloads attachment; a bundle with no payloads must use the %q profile", ProfilePrivateTriage, ProfilePublic)
		}
	default:
		return fmt.Errorf("redaction profile %q is unsupported", profile)
	}
	if err := validateQueryInputs(bundle); err != nil {
		return err
	}
	if err := validateReporterNote(bundle); err != nil {
		return err
	}
	if err := validateResponseErrorText(bundle); err != nil {
		return err
	}
	if opts.RequirePublic && profile != ProfilePublic {
		return fmt.Errorf("bundle redaction profile %q fails --require-public: only %q bundles may be treated as share-safe", profile, ProfilePublic)
	}

	// Exclude the (now consistency-checked) PayloadAttachment from the
	// share-safe walk so a legitimate private-triage attachment cannot trip
	// its own bundle's gate. Every other field is still walked. For a public
	// bundle Payloads is guaranteed nil by the check above, so nothing is
	// hidden from the walk.
	checkable := bundle
	checkable.Payloads = nil
	raw, err := json.Marshal(checkable)
	if err != nil {
		return fmt.Errorf("marshal bundle for validation: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode bundle for validation: %w", err)
	}
	if err := collector.ValidateShareSafeKeys(doc); err != nil {
		return fmt.Errorf("share-safe gate: %w", err)
	}
	return nil
}
