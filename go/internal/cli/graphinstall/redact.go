// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphinstall

import (
	"net/url"
	"strings"
)

// redactedCredentialMarker replaces a credential in the operator-facing
// rendering of an install source reference. It is deliberately visible rather
// than an empty string: an operator reading a manifest should be able to tell
// that a credential was supplied and stripped, not be left thinking the
// install came from a bare URL.
const redactedCredentialMarker = "REDACTED"

// redactSourceRef returns a display-only form of a NornicDB install source
// reference with every credential removed.
//
// `eshu graph install --from https://user:pw@host/build.tar.gz` used to put
// that reference verbatim into preparedInstallSource.SourcePath, which reaches
// three operator-facing sinks: the JSON the CLI prints to stdout
// (cmd/eshu/graph_install_cmd.go's printJSON), the install manifest persisted
// at <managed home>/graph-backends/nornicdb/manifest.json, and install.go's
// "sha256 mismatch for %q" error. Redacting here -- at the point SourcePath is
// assigned rather than at each sink -- means the struct never carries the
// secret, so a future renderer cannot reintroduce the leak by reading a field
// that looks safe.
//
// The whole query string is dropped rather than filtered per parameter. For
// this artifact the query carries no diagnostic value an operator needs:
// source_sha256 already identifies the exact bytes installed, and host plus
// path already identify the origin. Presigned download URLs (S3, GCS, and
// artifact CDNs) put the entire bearer credential in machine-generated query
// parameters, and only some of them -- X-Amz-Credential, say -- have a
// credential-shaped name. Filtering by name would pass X-Amz-Signature
// through, so name-based filtering is the wrong tool at this sink.
//
// This is a display transform, not a reversible one: the result is not a
// fetchable URL. That is safe here because nothing re-reads it. The install
// manifest is write-only (no code path in the repo parses it back), and the
// actual download always uses the caller's original reference, which
// materializeInstallSource holds separately.
//
// Local filesystem paths are returned unchanged. They are not URLs, they carry
// no userinfo or query string, and rewriting them would corrupt a legitimate
// path that happens to contain a "?" character.
func redactSourceRef(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Scheme == "file" {
		return trimmed
	}
	redacted := *parsed
	if redacted.User != nil {
		// Replaces both halves. A username is a credential in its own right --
		// several CI systems put the deploy token in the user position and
		// leave the password empty -- so masking only the password would
		// still leak.
		redacted.User = url.User(redactedCredentialMarker)
	}
	if redacted.RawQuery != "" {
		redacted.RawQuery = redactedCredentialMarker
	}
	return redacted.String()
}
