// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	scannerworkerv1 "github.com/eshu-hq/eshu/sdk/go/factschema/scannerworker/v1"
)

func testScannerWorkerEnvelope(factKind string, payload map[string]any) Envelope {
	return Envelope{
		FactKind:         factKind,
		SchemaVersion:    "1.0.0",
		StableFactKey:    "scanner-worker:test",
		ScopeID:          "scanner-scope",
		GenerationID:     "scanner-generation",
		CollectorKind:    "scanner_worker",
		SourceConfidence: "reported",
		Payload:          payload,
	}
}

func TestDecodeScannerWorkerAnalysis_RoundTrip(t *testing.T) {
	t.Parallel()

	distro := "debian"
	distroVersion := "12"
	packageManager := "dpkg"
	configuredImage := "ghcr.io/eshu-hq/demo:latest"
	original := scannerworkerv1.Analysis{
		Analyzer:                 "image_unpacking",
		TargetKind:               "image",
		TargetLocatorHash:        "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		AnalysisStatus:           "completed",
		CoverageStatus:           "scanned",
		ResultCount:              2,
		FactCount:                3,
		ImageReference:           "ghcr.io/eshu-hq/demo@sha256:2222222222222222222222222222222222222222222222222222222222222222",
		ImageDigest:              "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		EvidenceSource:           "rootfs",
		ExtractionReason:         "package_database_found",
		Distro:                   &distro,
		DistroVersion:            &distroVersion,
		PackageManager:           &packageManager,
		ConfiguredImageReference: &configuredImage,
	}

	payload, err := EncodeScannerWorkerAnalysis(original)
	if err != nil {
		t.Fatalf("EncodeScannerWorkerAnalysis() error = %v, want nil", err)
	}

	decoded, err := DecodeScannerWorkerAnalysis(testScannerWorkerEnvelope(FactKindScannerWorkerAnalysis, payload))
	if err != nil {
		t.Fatalf("DecodeScannerWorkerAnalysis() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeScannerWorkerAnalysis() = %+v, want %+v", decoded, original)
	}
}

func TestDecodeScannerWorkerWarning_MissingRequiredField(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"analyzer":            "image_unpacking",
		"target_kind":         "image",
		"target_locator_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"reason":              "image_analyzer_unsupported_target",
		"warning_class":       "scanner_worker_warning",
		"analysis_status":     "not_scanned",
		"coverage_status":     "unsupported",
		"image_reference":     "ghcr.io/eshu-hq/demo:latest",
		"image_digest":        "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"evidence_source":     "layer",
		"extraction_reason":   "unsupported_image",
	}
	delete(payload, "target_locator_hash")

	got, err := DecodeScannerWorkerWarning(testScannerWorkerEnvelope(FactKindScannerWorkerWarning, payload))
	if err == nil {
		t.Fatalf("DecodeScannerWorkerWarning() error = nil, want missing required field error")
	}

	var classified *DecodeError
	if !errors.As(err, &classified) {
		t.Fatalf("DecodeScannerWorkerWarning() error = %T, want *DecodeError", err)
	}
	if classified.Classification != ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want %q", classified.Classification, ClassificationInputInvalid)
	}
	if classified.Field != "target_locator_hash" {
		t.Fatalf("Field = %q, want target_locator_hash", classified.Field)
	}

	var zero scannerworkerv1.Warning
	if !reflect.DeepEqual(got, zero) {
		t.Fatalf("DecodeScannerWorkerWarning() returned non-zero struct %+v on error, want zero value", got)
	}
}
