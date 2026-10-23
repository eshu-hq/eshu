// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"bytes"
	"errors"
	"testing"
)

// TestExtractionPrepMetadataByteLimit pins the bounded-resource contract the
// extracted producer must preserve: one metadata document may not exceed
// 20MiB, and anything larger is a terminal, non-retryable
// registry_metadata_too_large failure that becomes coverage-gap warning
// evidence instead of a retry loop.
func TestExtractionPrepMetadataByteLimit(t *testing.T) {
	t.Parallel()

	const wantBytes = 20 << 20
	if maxMetadataDocumentBytes != wantBytes {
		t.Fatalf("maxMetadataDocumentBytes = %d, want %d (20MiB)", maxMetadataDocumentBytes, wantBytes)
	}

	tooLarge := metadataTooLargeError{limitBytes: maxMetadataDocumentBytes}
	if !isMetadataTooLarge(tooLarge) {
		t.Fatal("isMetadataTooLarge(newMetadataTooLargeError) = false, want true")
	}
	if got := tooLarge.FailureClass(); got != failureClassMetadataTooLarge {
		t.Fatalf("FailureClass() = %q, want %q", got, failureClassMetadataTooLarge)
	}
	if got := failureClassMetadataTooLarge; got != "registry_metadata_too_large" {
		t.Fatalf("failure class = %q, want %q", got, "registry_metadata_too_large")
	}
	if !tooLarge.TerminalFailure() {
		t.Fatal("metadata_too_large TerminalFailure() = false, want true")
	}

	oversized := bytes.NewReader(make([]byte, maxMetadataDocumentBytes+1))
	if _, err := readBoundedMetadata(oversized); !isMetadataTooLarge(err) {
		t.Fatalf("readBoundedMetadata(20MiB+1) error = %v, want metadata_too_large", err)
	}
	exact := bytes.NewReader(make([]byte, maxMetadataDocumentBytes))
	body, err := readBoundedMetadata(exact)
	if err != nil {
		t.Fatalf("readBoundedMetadata(20MiB) error = %v, want nil", err)
	}
	if len(body) != maxMetadataDocumentBytes {
		t.Fatalf("readBoundedMetadata(20MiB) len = %d, want %d", len(body), maxMetadataDocumentBytes)
	}
}

// TestExtractionPrepWarningTaxonomy pins the closed warning-code vocabulary
// the extracted producer must emit: five runtime collection codes plus the
// parser-layer version-limit code pinned in the packages envelope contract.
func TestExtractionPrepWarningTaxonomy(t *testing.T) {
	t.Parallel()

	codes := map[string]string{
		"metadata not found":          warningCodeMetadataNotFound,
		"metadata too large":          warningCodeMetadataTooLarge,
		"unsupported metadata source": warningCodeUnsupportedMetadataSource,
		"malformed metadata":          warningCodeMalformedMetadata,
		"credentials missing":         warningCodeCredentialsMissing,
	}
	want := map[string]string{
		"metadata not found":          "registry_not_found",
		"metadata too large":          "metadata_too_large",
		"unsupported metadata source": "unsupported_metadata_source",
		"malformed metadata":          "malformed_metadata",
		"credentials missing":         "credentials_missing",
	}
	for noun, code := range codes {
		if code != want[noun] {
			t.Fatalf("warning code for %s = %q, want %q", noun, code, want[noun])
		}
	}

	if got := parserWarningCode(errors.New(`metadata parser for ecosystem "cargo" is not registered`)); got != warningCodeUnsupportedMetadataSource {
		t.Fatalf("parserWarningCode(unregistered) = %q, want %q", got, warningCodeUnsupportedMetadataSource)
	}
	if got := parserWarningCode(errors.New("parse npm packument metadata: unexpected end of JSON input")); got != warningCodeMalformedMetadata {
		t.Fatalf("parserWarningCode(malformed) = %q, want %q", got, warningCodeMalformedMetadata)
	}
}
