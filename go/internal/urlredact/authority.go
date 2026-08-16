// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Authority parses value into the URL whose authority component answers "does
// this carry userinfo", and reports whether value had to be rewritten to get
// an authority at all.
//
// url.Parse only looks for userinfo inside an authority, and a value only has
// an authority after "//". `svc:PASSWORD@h.internal:5432/tool` has none:
// net/url returns scheme "svc", Opaque "PASSWORD@h.internal:5432/tool" and
// User nil, so testing parsed.User read a password in plain sight as no
// credential at all. Re-parsing the value as "//"+value asks net/url the same
// question about the authority the writer actually spelled, which keeps
// net/url — not a hand-written character rule — the decider of what a
// credential is.
//
// The rewrite is skipped unless opaqueHasAuthority says there is one, because
// it turns a rootless path into a host: an opaque body with no "@" ahead of
// its first "/" (a tool name such as `mcp:tool/name`, or a purl such as
// `pkg:npm/lodash@4.17.21`) carries no userinfo under any reading, and
// rewriting it can fail on a value that was never a credential.
//
// Errors never repeat value: they reach terminals and logs, the places the
// artifact beside them is redacted for.
func Authority(value string) (parsed *url.URL, rewritten bool, err error) {
	parsed, err = url.Parse(value)
	if err != nil {
		return nil, false, fmt.Errorf("parse value: %w", errWithoutValue(err))
	}
	if !opaqueHasAuthority(parsed.Opaque) {
		return parsed, false, nil
	}
	authority, err := url.Parse("//" + value)
	if err != nil {
		return nil, true, fmt.Errorf("parse value authority: %w", errWithoutValue(err))
	}
	return authority, true, nil
}

// CarriesUserinfo reports whether value embeds a credential in userinfo
// position under any reading net/url accepts: the direct parse, or the
// authority re-parse Authority performs when an opaque body is shaped like an
// authority. An unparseable value reports true — nothing can prove a string
// credential-free when it cannot be taken apart — so a fail-closed caller
// drops or refuses it.
//
// The accepted cost: `mailto:user@example.com` is structurally identical to
// `svc:PASSWORD@example.com` and reports true. No character rule can split
// them, and guessing wrong in the other direction ships a password.
func CarriesUserinfo(value string) bool {
	parsed, _, err := Authority(value)
	if err != nil {
		return true
	}
	return parsed.User != nil
}

// opaqueHasAuthority reports whether an opaque URL body (everything after
// "scheme:" when no "//" follows it) begins with something shaped like an
// authority: an "@" ahead of the first "/".
//
// This only selects whether there is an authority worth handing back to
// net/url. It decides nothing about what a credential is — that stays with
// net/url, which is why an "@" after the first "/" is left alone rather than
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

// errWithoutValue rebuilds a url.Parse failure so it keeps the operation and
// a reason but none of the parsed value. Dropping the *url.Error envelope is
// only half of that: the envelope quotes the whole URL, and the NESTED error
// can quote input again (`invalid port ":secret" after host`), so the reason
// goes through ParseErrorReason instead of being wrapped verbatim.
func errWithoutValue(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	return fmt.Errorf("%s: %s", urlErr.Op, ParseErrorReason(urlErr.Err))
}

// ParseErrorReason returns the reason half of a net/url parse failure as text
// that carries none of the parsed input. net/url copies offending input into
// several of its messages: `invalid port %q after host` repeats an unbounded
// slice of the value, EscapeError and InvalidHostError quote input
// characters, and "invalid host:" wraps a netip error that spells out the
// whole host. So only messages known to be static constants pass through
// verbatim; the input-quoting shapes map onto fixed text, and anything
// unrecognized — including a message a future Go version adds — fails closed
// to a generic reason rather than gamble that it carries no input.
//
// It accepts either the inner error or the whole *url.Error, so a caller that
// must keep its own envelope (cli/report keeps the request path) can classify
// just the reason.
func ParseErrorReason(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	var escapeErr url.EscapeError
	if errors.As(err, &escapeErr) {
		return "invalid URL escape"
	}
	var hostErr url.InvalidHostError
	if errors.As(err, &hostErr) {
		return "invalid character in host name"
	}
	msg := err.Error()
	switch msg {
	case "missing protocol scheme",
		"empty url",
		"invalid URI for request",
		"first path segment in URL cannot contain colon",
		"net/url: invalid control character in URL",
		"net/url: invalid userinfo",
		"invalid IP-literal",
		"missing ']' in host":
		return msg
	}
	switch {
	case strings.HasPrefix(msg, "invalid port "):
		return "invalid port after host"
	case strings.HasPrefix(msg, "invalid host:"):
		return "invalid host"
	}
	return "malformed URL"
}
