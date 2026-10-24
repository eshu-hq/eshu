// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package npmrc parses npm `.npmrc` files into registry-URL facts and nothing
// else.
//
// An `.npmrc` holds the registry URLs security wants (`registry=` and
// `@scope:registry=`) next to credentials (`_authToken`, `_auth`, `_password`,
// `//host/:username`, `email`). The content store keeps only an empty-body row
// for the file (the `metadata` content-store policy, #8812), and parser output
// is not governed by that policy, so this parser is built to be unable to
// emit a credential: it reads only the two registry key shapes, sanitizes each
// value as a URL, and drops the line when it cannot prove the result carries
// no credential.
//
// Parse emits one `config_kind: "registry_url"` row in the `variables` bucket
// per accepted registry line. A row carries the key (`registry` or
// `@scope:registry`), the scheme, the host with any port, and the path of the
// URL. The userinfo (`user:pass@`), the query string, and the fragment are
// removed before any field is built. A path that might carry a token (a UUID,
// a long mixed alphanumeric run, a `key=value` segment, a detector match) is
// dropped, and the row keeps the scheme and host with
// `registry_path_withheld: true`. A line is skipped, never partially emitted,
// when the value is not an absolute http or https URL with a valid
// host, holds an environment expansion (`${...}`), a control character, or a
// non-ASCII byte, exceeds the line bound, sits under an ini section, or holds
// a credential shape in its scheme or host according to the shared detector.
//
// Parse never sets `source`, never reads a credential key, and never reports
// an error that quotes file text.
package npmrc
