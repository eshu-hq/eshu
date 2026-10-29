// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// CredentialRefusal reports a capture target that cannot be recorded: it
// carries URL userinfo or cannot be parsed at all. The error names the
// field, never the value, per the package doc rule: these messages reach
// terminals, CI logs, and pasted bug reports — the same places the bundle
// beside them is redacted for.
type CredentialRefusal struct {
	Reason string
}

// Error returns the refusal without repeating the offending value.
func (e *CredentialRefusal) Error() string {
	return "target " + e.Reason
}

// CheckTargetCredentials refuses a target carrying URL userinfo, the
// `user:password@host` an authority component may hold.
//
// Every redaction rule in this package matches an object KEY name, and
// SplitTargetQuery exists to turn a target's query string back into keys so
// those rules can reach it. Userinfo sits before the "?", so the split never
// sees it and there is no key name to match: a pasted
// `https://svc:PASSWORD@host/tool` would land verbatim in query.target of a
// bundle stamped share-safe.
//
// It refuses rather than stripping, matching how Capture handles an
// unparseable query string and how sdk/go/collector's validateSourceURI
// handles the same userinfo on a fact source_ref: a bundle that quietly drops
// half of what the reporter asked misreports the query under investigation,
// and the reporter is the only one who can supply a target without the
// credential.
//
// net/url decides what an authority is, so an "@" inside a path segment
// (`/api/v0/owners/dev@example.com/services`) is untouched. urlredact
// .Authority is the shared decider the collector sanitizers read too, so
// this refusal and the envelope sanitizers cannot drift apart.
func CheckTargetCredentials(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, _, err := urlredact.Authority(trimmed)
	if err != nil {
		return &CredentialRefusal{
			Reason: "cannot be parsed as a URL, so it cannot be checked for an embedded credential; pass a plain path or tool name",
		}
	}
	if parsed.User != nil {
		return &CredentialRefusal{
			Reason: "carries a credential in its URL userinfo (user:password@host); remove it and rerun, because the captured bundle is meant to be attached to a public issue",
		}
	}
	return nil
}
