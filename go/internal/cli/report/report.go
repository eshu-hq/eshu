// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
)

// IncludePayloadsWarning is what `eshu report capture --include-payloads`
// prints to stderr, loudly, in addition to the same warning the bundle itself
// carries in Bundle.Payloads.Warning — so a terminal user sees it even if they
// only read stdout for the bundle JSON and skim past it.
const IncludePayloadsWarning = `
!!! PRIVATE TRIAGE ONLY !!!
This bundle includes raw fact payloads and citation excerpts because
--include-payloads was set. Do NOT attach this bundle to a public GitHub
issue or share it outside your own local triage workflow. Run without
--include-payloads for a share-safe bundle, or run
"eshu report validate --require-public" to confirm before sharing.
`

// EnvelopeClient is the part of the CLI's API client this package needs: the
// two calls that ask for Eshu's canonical envelope MIME type. It is declared
// here, where it is consumed, so the package does not depend on the concrete
// client — go/cmd/eshu is package main and nothing can import it.
type EnvelopeClient interface {
	GetEnvelope(path string, result any) error
	PostEnvelope(path string, body, result any) error
}

// CaptureOptions carries the reporter's inputs to CaptureBundle, already read
// off their flags by the caller. The values arrive raw: normalizing the method,
// decoding ParamsJSON, and deciding which of Endpoint and Tool becomes the
// recorded target are this package's rules, not the CLI's.
type CaptureOptions struct {
	// Endpoint is the API path the query is issued against. It may carry its
	// own query string.
	Endpoint string
	// Tool, when set, names the MCP tool the query originated from. It becomes
	// the recorded target and flips the recorded surface to "mcp"; Endpoint
	// still resolves the answer.
	Tool string
	// Method is "GET" or "POST", case-insensitive. Empty means GET.
	Method string
	// ParamsJSON is the raw --params flag value: a JSON object, or empty.
	ParamsJSON string
	// Note is the reporter's description of what they expected instead.
	Note string
	// IncludePayloads attaches raw fact payloads and citation excerpts and
	// flips the bundle out of the share-safe profile.
	IncludePayloads bool
}

// CaptureResult is a finished bundle and the exact bytes a caller should write
// for it. JSON is indented and newline-terminated, so writing it to a file and
// to stdout produces identical content.
type CaptureResult struct {
	Bundle reportbundle.Bundle
	JSON   []byte
}

// TargetCredentialError reports a target the reporter supplied that carries, or
// may carry, a credential. It is a distinct type so the CLI can map it to the
// usage exit code without matching on message text.
type TargetCredentialError struct {
	// Flag is the operator-facing flag name, for example "--endpoint".
	Flag string
	// Reason says what is wrong. It never repeats the offending value.
	Reason string
}

func (e *TargetCredentialError) Error() string {
	return e.Flag + " " + e.Reason
}

// checkTargetCredentials refuses a target carrying URL userinfo, the
// `user:password@host` an authority component may hold.
//
// Every redaction rule in internal/reportbundle matches an object KEY name, and
// SplitTargetQuery exists to turn a target's query string back into keys so
// those rules can reach it. Userinfo sits before the "?", so the split never
// sees it and there is no key name to match: `--tool
// https://svc:PASSWORD@mcp.internal/tool` put the password verbatim into
// query.target of a bundle stamped `"profile": "public"`, `"rules": []` and
// `"status": "passed"` — a share-safe artifact, meant for a public issue, that
// certified it had screened itself.
//
// It refuses rather than stripping, matching how reportbundle.Capture handles
// an unparseable query string and how sdk/go/collector's validateSourceURI
// handles the same userinfo on a fact source_ref: a bundle that quietly drops
// half of what the reporter asked misreports the query under investigation, and
// the reporter is the only one who can supply a target without the credential.
//
// net/url decides what an authority is, so an "@" inside a path segment
// (`/api/v0/owners/dev@example.com/services`) is untouched. A hand-written
// character rule is exactly what has been wrong here before. targetAuthority
// is what makes net/url the decider for the "//"-less spelling too.
//
// Not covered: a full URL pasted INSIDE a path segment
// (`/api/v0/x/https://svc:pw@host/y`) is not an authority component, so net/url
// reports no userinfo and the value passes. Neither is an "@" that follows the
// first "/" of an opaque target (`svc:name/dev@example.com`), by the same rule
// that keeps the path-segment case above working. A target with no scheme and
// no "//" at all (`dev@example.com/services`) is read as the relative path it
// looks like, so it passes too. An unparseable target is refused instead of
// passed, because nothing can separate a credential from a string that cannot
// be taken apart — including one that only becomes unparseable once its
// authority is read as an authority.
//
// The error names the flag and never repeats the value, per the same rule
// internal/reportbundle states in its doc.go: these messages reach terminals,
// CI logs and pasted bug reports — the places the bundle beside them is
// redacted for.
func checkTargetCredentials(flag, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, _, err := targetAuthority(trimmed)
	if err != nil {
		return &TargetCredentialError{
			Flag:   flag,
			Reason: "cannot be parsed as a URL, so it cannot be checked for an embedded credential; pass a plain path or tool name",
		}
	}
	if parsed.User != nil {
		return &TargetCredentialError{
			Flag:   flag,
			Reason: "carries a credential in its URL userinfo (user:password@host); remove it and rerun, because the captured bundle is meant to be attached to a public issue",
		}
	}
	return nil
}

// targetAuthority parses value into the URL whose authority component answers
// "does this carry userinfo", and reports whether value had to be rewritten to
// get an authority at all.
//
// url.Parse only looks for userinfo inside an authority, and a value only has
// an authority after "//". `svc:PASSWORD@h.internal:5432/tool` has none:
// net/url returns scheme "svc", Opaque "PASSWORD@h.internal:5432/tool" and
// User nil, so testing parsed.User read a password in plain sight as no
// credential at all. Re-parsing the value as "//"+value asks net/url the same
// question about the authority the reporter actually wrote, which keeps
// net/url — not a character rule — the thing that decides what userinfo is.
//
// The rewrite is skipped unless opaqueHasAuthority says there is one, because
// it turns a rootless path into a host: an opaque target with no "@" in front
// of its first "/" (a tool name such as `mcp:tool/name`) carries no userinfo
// under any reading, and rewriting it can fail on a value that was never a
// credential.
func targetAuthority(value string) (parsed *url.URL, rewritten bool, err error) {
	parsed, err = url.Parse(value)
	if err != nil {
		return nil, false, fmt.Errorf("parse target: %w", err)
	}
	if !opaqueHasAuthority(parsed.Opaque) {
		return parsed, false, nil
	}
	authority, err := url.Parse("//" + value)
	if err != nil {
		return nil, true, fmt.Errorf("parse target authority: %w", err)
	}
	return authority, true, nil
}

// opaqueHasAuthority reports whether an opaque URL body (everything after
// "scheme:" when no "//" follows it) begins with something shaped like an
// authority: an "@" ahead of the first "/".
//
// This only selects whether there is an authority worth handing back to
// net/url. It decides nothing about what a credential is — that stays with
// net/url, which is why the "@" after the first "/" is left alone rather than
// treated as a boundary of its own.
func opaqueHasAuthority(opaque string) bool {
	if opaque == "" {
		return false
	}
	authority := opaque
	if slash := strings.IndexByte(opaque, '/'); slash >= 0 {
		authority = opaque[:slash]
	}
	return strings.IndexByte(authority, '@') >= 0
}

// CaptureBundle issues the reporter's query, composes the wrong_answer_report.v1
// bundle from the answer, and returns it with its canonical JSON encoding.
//
// Both Endpoint and Tool are screened for an embedded credential before any
// request goes out: Endpoint reaches the bundle when Tool is absent, and
// reaches the failure message either way, so screening only the one that
// becomes query.target leaves the other live.
//
// It never writes anything. The caller decides where the bytes go, and whether
// to print IncludePayloadsWarning alongside them.
func CaptureBundle(client EnvelopeClient, opts CaptureOptions) (CaptureResult, error) {
	if err := checkTargetCredentials("--endpoint", opts.Endpoint); err != nil {
		return CaptureResult{}, err
	}
	if err := checkTargetCredentials("--tool", opts.Tool); err != nil {
		return CaptureResult{}, err
	}

	endpoint := strings.TrimSpace(opts.Endpoint)
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = "GET"
	}

	params := map[string]any{}
	if strings.TrimSpace(opts.ParamsJSON) != "" {
		if err := json.Unmarshal([]byte(opts.ParamsJSON), &params); err != nil {
			return CaptureResult{}, fmt.Errorf("--params must be a JSON object: %w", err)
		}
	}

	surface := "api"
	target := endpoint
	if tool := strings.TrimSpace(opts.Tool); tool != "" {
		surface = "mcp"
		target = tool
	}

	envelope, err := fetchEnvelope(client, method, endpoint, params)
	if err != nil {
		return CaptureResult{}, fmt.Errorf("fetch query envelope: %w", err)
	}

	bundle, err := reportbundle.Capture(reportbundle.CaptureInput{
		Surface:         surface,
		Target:          target,
		Method:          method,
		Params:          params,
		Profile:         string(envelopeProfile(envelope)),
		ReporterNote:    opts.Note,
		Envelope:        envelope,
		Truncated:       observedTruncation(envelope),
		IncludePayloads: opts.IncludePayloads,
	})
	if err != nil {
		return CaptureResult{}, fmt.Errorf("capture report bundle: %w", err)
	}

	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return CaptureResult{}, fmt.Errorf("marshal report bundle: %w", err)
	}
	return CaptureResult{Bundle: bundle, JSON: append(raw, '\n')}, nil
}

// WriteBundle writes a captured bundle to path with owner-only permissions. A
// private-triage bundle carries raw payloads, and the mode does not depend on
// which profile produced the bytes.
func WriteBundle(path string, raw []byte) error {
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write report bundle: %w", err)
	}
	return nil
}

// fetchEnvelope issues the query the reporter ran and decodes it into the
// canonical query.ResponseEnvelope shape, through the same envelope calls every
// other envelope-backed verb uses.
func fetchEnvelope(client EnvelopeClient, method, endpoint string, params map[string]any) (query.ResponseEnvelope, error) {
	var envelope query.ResponseEnvelope
	switch method {
	case "GET":
		// --endpoint may already carry its own query string. Splitting it and
		// re-encoding both sources together builds one well-formed URL;
		// appending "?"+params to a path that already had a "?" produced a
		// malformed second query string. reportbundle.SplitTargetQuery is the
		// same function Capture uses to keep the credential out of the
		// recorded bundle, so the request and the record cannot drift.
		//
		// A query string it cannot parse stops the run here, before the
		// request goes out. Capture would refuse the same target anyway, and
		// issuing a query the reporter did not type only to throw the answer
		// away is worse than not issuing it.
		//
		// The error is returned unwrapped: CaptureBundle already prefixes it
		// with "fetch query envelope", and a second prefix here would read as
		// two failures.
		path, targetParams, err := reportbundle.SplitTargetQuery(endpoint)
		if err != nil {
			return query.ResponseEnvelope{}, err //nolint:wrapcheck // wrapping would double the prefix CaptureBundle already adds
		}
		values := url.Values{}
		for key, value := range targetParams {
			repeated, ok := value.([]any)
			if !ok {
				values.Set(key, fmt.Sprintf("%v", value))
				continue
			}
			for _, item := range repeated {
				values.Add(key, fmt.Sprintf("%v", item))
			}
		}
		// An explicit --params entry replaces a same-named endpoint parameter,
		// matching how Capture resolves the same collision.
		for key, value := range params {
			values.Set(key, fmt.Sprintf("%v", value))
		}
		requestPath := path
		if len(values) > 0 {
			requestPath += "?" + values.Encode()
		}
		if err := client.GetEnvelope(requestPath, &envelope); err != nil {
			return query.ResponseEnvelope{}, requestErrorWithoutURL(err, path)
		}
	case "POST":
		postPath, _, err := reportbundle.SplitTargetQuery(endpoint)
		if err != nil {
			return query.ResponseEnvelope{}, err //nolint:wrapcheck // wrapping would double the prefix CaptureBundle already adds
		}
		if err := client.PostEnvelope(endpoint, params, &envelope); err != nil {
			return query.ResponseEnvelope{}, requestErrorWithoutURL(err, postPath)
		}
	default:
		return query.ResponseEnvelope{}, fmt.Errorf("unsupported --method %q: want GET or POST", method)
	}
	return envelope, nil
}

// requestErrorWithoutURL strips the request URL out of a transport error and
// puts the bare endpoint path in its place.
//
// The capture command has to issue the reporter's real request, credentials and
// all — reproducing the exact query is the feature. net/http reports a failed
// request as a *url.Error whose Error() quotes that whole URL, so a wrong port
// or an unreachable service printed the credential to stderr and into any CI
// log that captured the run. None of the bundle-side redaction applies: this
// error exists before Capture is ever called.
//
// The wrapped transport error is preserved with %w, so errors.Is/As still work
// and the reader still learns what actually failed (connection refused, TLS
// handshake, timeout). Those carry host:port, never the query string.
//
// The substituted path goes through safeErrorPath rather than being trusted: it
// is the reporter's own --endpoint, and stripping the query string leaves
// `https://svc:PASSWORD@host/path` fully intact. CaptureBundle already refuses
// such an endpoint before any request goes out, so this is the second of two
// guards — deliberately, because "the caller checked it" is the kind of ordering
// assumption this function exists to stop depending on.
//
// Not covered: a server that echoes the request URL back inside a 4xx/5xx
// response body. That arrives as the CLI's apiHTTPError, which lives in package
// main and so cannot be read from here; issue #6059's sibling branch (PR #6117)
// adds the accessor that would make it reachable.
func requestErrorWithoutURL(err error, safePath string) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err //nolint:wrapcheck // the transport error is the answer; a prefix here would displace it
	}
	return fmt.Errorf("%s %s: %w", urlErr.Op, safeErrorPath(safePath), urlErr.Err)
}

// safeErrorPath returns path with any URL userinfo replaced by a fixed marker,
// for use in a message a reporter reads.
//
// Unlike checkTargetCredentials this redacts instead of refusing: the caller is
// already reporting a failure, and the host and path are what a reader needs to
// fix it. A path net/url cannot parse is replaced wholesale, since a string that
// cannot be taken apart cannot have a credential separated out of it.
//
// It reads userinfo through the same targetAuthority the refusal uses, so the
// two cannot disagree about what a credential is. They did once: the
// "//"-less spelling passed the refusal and then arrived here, where the same
// parsed.User test left it in the message verbatim.
func safeErrorPath(path string) string {
	parsed, rewritten, err := targetAuthority(path)
	if err != nil {
		return "[unparseable endpoint]"
	}
	if parsed.User == nil {
		// Returned verbatim rather than through parsed.String(), which
		// re-encodes and would change messages that were already safe.
		return path
	}
	parsed.User = url.User("redacted")
	if rewritten {
		// Drop the "//" targetAuthority added, so the message keeps the
		// spelling the reporter typed. The scheme goes with the userinfo it
		// turned out to be part of: in `svc:PASSWORD@host/x`, "svc" is the
		// username.
		return strings.TrimPrefix(parsed.String(), "//")
	}
	return parsed.String()
}

// observedTruncation looks for a top-level "truncated" boolean in the captured
// response data. Truncation is a read-model field, not part of the envelope
// contract (see query.AnswerPacket), so this is a best-effort read of the SAME
// shape a maintainer would see, not a new contract.
func observedTruncation(envelope query.ResponseEnvelope) bool {
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		return false
	}
	truncated, ok := data["truncated"].(bool)
	return ok && truncated
}

// envelopeProfile returns the query profile the envelope's truth reports, or
// empty when no truth envelope is present (for example an error response).
func envelopeProfile(envelope query.ResponseEnvelope) query.QueryProfile {
	if envelope.Truth == nil {
		return ""
	}
	return envelope.Truth.Profile
}

// ReadBundleInput returns the report bundle bytes for `eshu report validate`:
// the file at path when path is non-empty, otherwise stdin.
func ReadBundleInput(in io.Reader, path string) ([]byte, error) {
	if strings.TrimSpace(path) != "" {
		raw, err := os.ReadFile(path) // #nosec G304 -- operator-supplied local validation path, not an HTTP request param //nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("read report bundle: %w", err)
		}
		return raw, nil
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("read report bundle stdin: %w", err)
	}
	return raw, nil
}

// ValidateBundle decodes raw and runs it through reportbundle.Validate, writing
// the one-line verdict to w before returning.
//
// The verdict is written on the failure path too, and before the error is
// returned: a maintainer running this against an attachment gets the same
// "validation: failed" line on stdout whether they read the exit code or not.
func ValidateBundle(w io.Writer, raw []byte, requirePublic bool) error {
	var bundle reportbundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return fmt.Errorf("decode report bundle: %w", err)
	}
	if err := reportbundle.Validate(bundle, reportbundle.ValidateOptions{RequirePublic: requirePublic}); err != nil {
		_, _ = fmt.Fprintln(w, "report bundle validation: failed")
		//nolint:wrapcheck // the validation failure is the message; a prefix would displace what the gate said
		return err
	}
	_, _ = fmt.Fprintln(w, "report bundle validation: passed")
	return nil
}
