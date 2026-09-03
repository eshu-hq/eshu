// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

func TestContainerImageIdentityHandlerFailsClosedOnUnknownRetirementWarning(t *testing.T) {
	t.Parallel()

	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{
			scopeFacts: []facts.Envelope{
				gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
			},
			warnings: []facts.Envelope{
				retirementWarningEnvelope("future_completeness_warning", ""),
			},
		},
		Writer: writer,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5854-unknown-warning",
		Domain:       reducercontract.DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "test",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want unknown active warning to fail closed")
	}
	if !strings.Contains(err.Error(), "unrecognized OCI registry warning code") {
		t.Fatalf("Handle() error = %q, want unknown warning classification", err)
	}
	if !strings.Contains(err.Error(), "warning-future_completeness_warning") {
		t.Fatalf("Handle() error = %q, want source warning fact ID", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 after unknown active warning", writer.calls)
	}
}

func TestClassifyContainerImageIdentityWarningCoversSchemaCatalog(t *testing.T) {
	t.Parallel()

	expected := map[string]containerImageIdentityWarningDisposition{
		ociregistryv1.WarningCodeUnsupportedReferrersAPI: containerImageIdentityWarningNoRetirementHold,
		ociregistryv1.WarningCodeComputedManifestDigest:  containerImageIdentityWarningNoRetirementHold,
		ociregistryv1.WarningCodeConfigBlobUnavailable:   containerImageIdentityWarningHoldConfigBlob,
		ociregistryv1.WarningCodeConfigBlobOversized:     containerImageIdentityWarningNoRetirementHold,
		ociregistryv1.WarningCodeTagListTruncated:        containerImageIdentityWarningHoldTagList,
		ociregistryv1.WarningCodeMissingManifestDigest:   containerImageIdentityWarningHoldMissingManifest,
	}
	for _, warningCode := range ociregistryv1.KnownWarningCodes() {
		want, ok := expected[warningCode]
		if !ok {
			t.Fatalf("schema warning code %q has no retirement disposition", warningCode)
		}
		got, err := classifyContainerImageIdentityWarning(warningCode)
		if err != nil {
			t.Fatalf("classifyContainerImageIdentityWarning(%q) error = %v", warningCode, err)
		}
		if got != want {
			t.Fatalf("classifyContainerImageIdentityWarning(%q) = %v, want %v", warningCode, got, want)
		}
		delete(expected, warningCode)
	}
	if len(expected) != 0 {
		t.Fatalf("retirement dispositions not present in schema catalog: %v", expected)
	}

	for _, warningCode := range []string{"", " ", "future_completeness_warning"} {
		got, err := classifyContainerImageIdentityWarning(warningCode)
		if err == nil {
			t.Fatalf("classifyContainerImageIdentityWarning(%q) error = nil, want non-nil", warningCode)
		}
		if got != containerImageIdentityWarningInvalid {
			t.Fatalf(
				"classifyContainerImageIdentityWarning(%q) = %v, want invalid",
				warningCode,
				got,
			)
		}
	}
}

var (
	benchmarkRetirementParsed ParsedContainerImageRef
	benchmarkRetirementOK     bool
)

func BenchmarkContainerImageIdentityRetirementImageRefParsing(b *testing.B) {
	imageRef := "registry.example.com/team/api:prod"

	b.Run("legacy_two_parses", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkRetirementParsed, benchmarkRetirementOK = ParseContainerImageRef(imageRef)
			benchmarkRetirementParsed, benchmarkRetirementOK = ParseContainerImageRef(imageRef)
		}
	})
	b.Run("candidate_single_parse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkRetirementParsed, benchmarkRetirementOK = ParseContainerImageRef(imageRef)
		}
	})
}
