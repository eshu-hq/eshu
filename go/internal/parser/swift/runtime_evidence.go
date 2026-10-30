// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// swiftToolsVersionMarker is the Package.swift comment SwiftPM reads for the
// tools version. The match is case-sensitive and requires the colon form, so a
// differently-cased marker or a bare mention without a value declares
// nothing.
const swiftToolsVersionMarker = "swift-tools-version"

// ExtractSwiftToolsVersionEvidence reads the swift-tools-version comment from
// one supplied Package.swift body for the content-only collector entrypoint.
// Native parsing attaches the same evidence from the bytes it already read, so
// the snapshot seam carries this result once without a second parse. Only the
// first marker counts. A file without the marker is absent (parsed_empty, no
// candidate); a marker without a value is malformed (unavailable, never a
// silent empty declaration). A credential-shaped value is rejected by the
// shared guard and counted, never emitted.
func ExtractSwiftToolsVersionEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	for number, line := range strings.Split(string(source), "\n") {
		if number == 0 {
			line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		}
		value, ok := swiftToolsVersionCommentValue(line)
		if !ok {
			continue
		}
		if value == "" {
			return shared.RuntimeEvidence{State: "unavailable", ByteSize: len(source), Digest: shared.RuntimeDigest(source)}
		}
		evidence.Add("swift_tools_version", "swift", value, number+1)
		return evidence
	}
	return evidence
}

// swiftToolsVersionCommentValue reports whether line is a swift-tools-version
// comment and returns its declared value. ok is false when the line carries
// no marker, so a string literal that merely mentions the marker declares
// nothing: only a leading comment counts.
func swiftToolsVersionCommentValue(line string) (value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "//") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
	if !strings.HasPrefix(rest, swiftToolsVersionMarker) {
		return "", false
	}
	after := strings.TrimSpace(rest[len(swiftToolsVersionMarker):])
	if !strings.HasPrefix(after, ":") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(after, ":")), true
}
