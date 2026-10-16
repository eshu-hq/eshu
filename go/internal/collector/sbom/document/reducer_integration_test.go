// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package document_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sbom/document"
	"github.com/eshu-hq/eshu/go/internal/reducer/sbomattest"
)

// TestParserFeedsReducerAttachmentTruth proves the parser's emitted facts
// satisfy the SBOM attachment reducer contract end-to-end. Parser-only output
// must be classified as `attached_parse_only` because no attestation or
// signature evidence is present yet.
func TestParserFeedsReducerAttachmentTruth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		fixture    string
		parser     func(raw []byte, ctx document.FixtureContext) ([]parserEnvelopes, error)
		wantStatus sbomattest.SBOMAttachmentStatus
	}{
		{
			name:       "cyclonedx subject -> parse-only",
			fixture:    "testdata/cyclonedx_image_subject.json",
			parser:     cycloneDXParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentAttachedParseOnly,
		},
		{
			name:       "spdx subject -> parse-only",
			fixture:    "testdata/spdx_image_subject.json",
			parser:     spdxParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentAttachedParseOnly,
		},
		{
			name:       "cyclonedx missing subject -> unknown_subject",
			fixture:    "testdata/cyclonedx_missing_subject.json",
			parser:     cycloneDXParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentUnknownSubject,
		},
		{
			name:       "spdx missing subject -> unknown_subject",
			fixture:    "testdata/spdx_missing_subject.json",
			parser:     spdxParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentUnknownSubject,
		},
		{
			name:       "cyclonedx malformed -> unparseable",
			fixture:    "testdata/cyclonedx_malformed.json",
			parser:     cycloneDXParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentUnparseable,
		},
		{
			name:       "spdx malformed -> unparseable",
			fixture:    "testdata/spdx_malformed.json",
			parser:     spdxParserAdapter,
			wantStatus: sbomattest.SBOMAttachmentUnparseable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := readFixture(t, tc.fixture)
			ctx := document.FixtureContext{
				ScopeID:             "sbom://truth-test",
				GenerationID:        "gen-1",
				CollectorInstanceID: "fixture",
				FencingToken:        1,
				ObservedAt:          time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
			}
			adapter, err := tc.parser(raw, ctx)
			if err != nil {
				t.Fatalf("parser error: %v", err)
			}
			decisions := sbomattest.BuildSBOMAttestationAttachmentDecisions(unwrap(adapter))
			if len(decisions) != 1 {
				t.Fatalf("decisions = %d, want 1: %#v", len(decisions), decisions)
			}
			if decisions[0].AttachmentStatus != tc.wantStatus {
				t.Fatalf("attachment status = %q, want %q (reason=%q)",
					decisions[0].AttachmentStatus, tc.wantStatus, decisions[0].Reason)
			}
		})
	}
}
