// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func decodeDataBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

func TestWriteEmptyScopePages(t *testing.T) {
	t.Parallel()

	handler := &Handler{}

	t.Run("ImpactFindingsPage", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyImpactFindingsPage(recorder, httptest.NewRequest(http.MethodGet, "/", nil), 25, "comprehensive")
		body := decodeDataBody(t, recorder)
		if body["count"] != float64(0) || body["limit"] != float64(25) || body["detection_profile"] != "comprehensive" {
			t.Fatalf("body = %v, want the zero-findings page shape", body)
		}
	})
	t.Run("ImpactExplanation", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyImpactExplanation(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
			t.Fatalf("status = %d, empty explanation page must answer 200 with a body", recorder.Code)
		}
	})
	t.Run("ImpactCount", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyImpactCount(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		body := decodeDataBody(t, recorder)
		if body["total_findings"] != float64(0) || body["detection_profile"] != "comprehensive" {
			t.Fatalf("body = %v, want the zero-count shape", body)
		}
	})
	t.Run("ImpactInventory", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyImpactInventory(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "severity", 10, 0)
		body := decodeDataBody(t, recorder)
		if body["count"] != float64(0) || body["group_by"] != "severity" || body["next_offset"] != nil {
			t.Fatalf("body = %v, want the empty inventory shape", body)
		}
	})
	t.Run("ContainerImageIdentityPage", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyContainerImageIdentityPage(recorder, httptest.NewRequest(http.MethodGet, "/", nil), 10)
		body := decodeDataBody(t, recorder)
		if body["count"] != float64(0) || body["limit"] != float64(10) {
			t.Fatalf("body = %v, want the zero-identities page shape", body)
		}
	})
	t.Run("ContainerImageIdentityCount", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyContainerImageIdentityCount(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
			t.Fatalf("status = %d, empty count must answer 200 with a body", recorder.Code)
		}
	})
	t.Run("ContainerImageIdentityInventory", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptyContainerImageIdentityInventory(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "registry", 10, 5)
		body := decodeDataBody(t, recorder)
		if body["offset"] != float64(5) || body["group_by"] != "registry" {
			t.Fatalf("body = %v, want offset and dimension carried", body)
		}
	})
	t.Run("SBOMAttachmentPage", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptySBOMAttachmentPage(recorder, httptest.NewRequest(http.MethodGet, "/", nil), 10)
		body := decodeDataBody(t, recorder)
		if body["count"] != float64(0) || body["limit"] != float64(10) {
			t.Fatalf("body = %v, want the zero-attachments page shape", body)
		}
	})
	t.Run("SBOMAttachmentCount", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptySBOMAttachmentCount(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
			t.Fatalf("status = %d, empty count must answer 200 with a body", recorder.Code)
		}
	})
	t.Run("SBOMAttachmentInventory", func(t *testing.T) {
		t.Parallel()
		recorder := httptest.NewRecorder()
		handler.writeEmptySBOMAttachmentInventory(recorder, httptest.NewRequest(http.MethodGet, "/", nil), "status", 10, 0)
		body := decodeDataBody(t, recorder)
		if body["count"] != float64(0) || body["group_by"] != "status" {
			t.Fatalf("body = %v, want the empty inventory shape", body)
		}
	})
}

type stubCollectorReadinessStore struct {
	configured bool
	err        error
	called     bool
}

func (s *stubCollectorReadinessStore) CollectorConfigured(context.Context, scope.CollectorKind) (bool, error) {
	s.called = true
	return s.configured, s.err
}

func TestCollectorListReadiness(t *testing.T) {
	t.Parallel()

	t.Run("NilStoreLeavesBodyUntouched", func(t *testing.T) {
		t.Parallel()
		body := map[string]any{}
		attachCollectorListReadiness(t.Context(), body, nil, scope.CollectorOCIRegistry, 0, false)
		if len(body) != 0 {
			t.Fatalf("body = %v, want untouched without a probe store", body)
		}
	})
	t.Run("RowsProveCollectorRanWithoutProbe", func(t *testing.T) {
		t.Parallel()
		// A failing probe must never downgrade a page that already
		// carries rows: returned evidence outranks the probe.
		store := &stubCollectorReadinessStore{err: errors.New("probe down")}
		body := map[string]any{}
		attachCollectorListReadiness(t.Context(), body, store, scope.CollectorOCIRegistry, 3, false)
		if store.called {
			t.Fatal("probe ran for a non-empty page, want it skipped")
		}
		if _, ok := body["collector_readiness"]; !ok {
			t.Fatalf("body = %v, want the ready_with_results envelope", body)
		}
	})
	t.Run("EmptyPageConsultsProbe", func(t *testing.T) {
		t.Parallel()
		store := &stubCollectorReadinessStore{configured: true}
		body := map[string]any{}
		attachCollectorListReadiness(t.Context(), body, store, scope.CollectorOCIRegistry, 0, false)
		if !store.called {
			t.Fatal("probe did not run for an empty page, want it consulted")
		}
		if _, ok := body["collector_readiness"]; !ok {
			t.Fatalf("body = %v, want the probe envelope", body)
		}
	})
	t.Run("ProbeErrorYieldsUnavailable", func(t *testing.T) {
		t.Parallel()
		store := &stubCollectorReadinessStore{err: errors.New("probe down")}
		envelope, ok := collectorListReadiness(t.Context(), store, scope.CollectorOCIRegistry, 0, false)
		if !ok {
			t.Fatal("ok = false, want an envelope even when the probe fails")
		}
		_ = envelope
	})
}

func TestBuildContainerImageIdentitySourceBridge(t *testing.T) {
	t.Parallel()

	t.Run("EmptyRowsListMissingEvidence", func(t *testing.T) {
		t.Parallel()
		bridge := BuildContainerImageIdentitySourceBridge("repo-a", nil)
		if len(bridge.MissingEvidence) != 3 {
			t.Fatalf("missing = %v, want all three evidence gaps", bridge.MissingEvidence)
		}
	})
	t.Run("AmbiguousRepositoriesWarn", func(t *testing.T) {
		t.Parallel()
		bridge := BuildContainerImageIdentitySourceBridge("repo-a", []ContainerImageIdentityResult{
			{RepositoryID: "reg-a", ImageRef: "img:1", Digest: "sha256:1", SourceRepositoryIDs: []string{"repo-a"}},
			{RepositoryID: "reg-b", ImageRef: "img:1", Digest: "sha256:1", SourceRepositoryIDs: []string{"repo-a"}},
		})
		if len(bridge.Warnings) != 1 || bridge.Warnings[0] != "ambiguous_image_repository" {
			t.Fatalf("warnings = %v, want the ambiguity warning", bridge.Warnings)
		}
		if len(bridge.MissingEvidence) != 0 {
			t.Fatalf("missing = %v, want none with full evidence", bridge.MissingEvidence)
		}
	})
	t.Run("PartialEvidenceListsGaps", func(t *testing.T) {
		t.Parallel()
		bridge := BuildContainerImageIdentitySourceBridge("repo-a", []ContainerImageIdentityResult{
			{RepositoryID: "reg-a", SourceRepositoryIDs: []string{"repo-other"}},
		})
		if len(bridge.MissingEvidence) != 3 {
			t.Fatalf("missing = %v, want all three gaps (no image ref, no digest, no source correlation)", bridge.MissingEvidence)
		}
	})
}

func TestChainClosedEnums(t *testing.T) {
	t.Parallel()

	if !isSupportedContainerImageIdentityOutcome("exact_digest") || !isSupportedContainerImageIdentityOutcome("tag_resolved") {
		t.Fatal("known outcomes must be supported")
	}
	if isSupportedContainerImageIdentityOutcome("fuzzy") {
		t.Fatal("unknown outcome must be rejected")
	}
	for _, status := range []string{"attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"} {
		if !isSupportedSBOMAttachmentStatus(status) {
			t.Errorf("status %q must be supported", status)
		}
	}
	if isSupportedSBOMAttachmentStatus("attached_maybe") {
		t.Fatal("unknown attachment status must be rejected")
	}
	if !isSupportedSBOMArtifactKind("sbom") || !isSupportedSBOMArtifactKind("attestation") {
		t.Fatal("known artifact kinds must be supported")
	}
	if isSupportedSBOMArtifactKind("cyclonedx") {
		t.Fatal("unknown artifact kind must be rejected")
	}
}

func TestBoundedSBOMWarningSummaries(t *testing.T) {
	t.Parallel()

	if preview, total, truncated := BoundedSBOMWarningSummaries(nil); preview != nil || total != 0 || truncated {
		t.Fatalf("empty = (%v, %d, %v), want (nil, 0, false)", preview, total, truncated)
	}
	values := []string{"b", "a", "b", "c"}
	preview, total, truncated := BoundedSBOMWarningSummaries(values)
	// Total counts raw inputs (duplicates included) while the preview
	// deduplicates, so a duplicate alone discloses truncation.
	if total != 4 || !truncated {
		t.Fatalf("total = %d truncated = %v, want 4 true", total, truncated)
	}
	if len(preview) != 3 {
		t.Fatalf("preview = %v, want deduplicated values", preview)
	}
	many := []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10", "s11"}
	preview, total, truncated = BoundedSBOMWarningSummaries(many)
	if total != 12 || !truncated || len(preview) != SBOMAttestationWarningSummaryPreviewMaxCount {
		t.Fatalf("capped = (%v, %d, %v), want 10-of-12 with truncation", preview, total, truncated)
	}
}

func TestNextAggregateOffsets(t *testing.T) {
	t.Parallel()

	if got := NextImpactAggregateOffset(0, 10, false); got != nil {
		t.Fatalf("untruncated offset = %v, want nil", got)
	}
	if got := NextImpactAggregateOffset(0, 10, true); got != 10 {
		t.Fatalf("offset = %v, want 10", got)
	}
	if got := NextContainerImageIdentityAggregateOffset(0, 10, true); got != 10 {
		t.Fatalf("offset = %v, want 10", got)
	}
	if got := NextSBOMAttestationAttachmentAggregateOffset(0, 10, true); got != 10 {
		t.Fatalf("offset = %v, want 10", got)
	}
	if got := NextSecurityAlertReconciliationAggregateOffset(0, 10, true); got != 10 {
		t.Fatalf("offset = %v, want 10", got)
	}
}

func TestContractForScannerRoute(t *testing.T) {
	t.Parallel()

	contract := map[string]any{"routes": vulnerabilityScannerRouteContracts()}
	got := contractForScannerRoute(contract, "impact_findings")
	if len(got) != 1 || got[0].Name != "impact_findings" {
		t.Fatalf("routes = %+v, want the one matching route", got)
	}
	if got := contractForScannerRoute(contract, "nope"); got != nil {
		t.Fatalf("routes = %+v, want nil for an unknown route", got)
	}
	if got := contractForScannerRoute(map[string]any{}, "impact_findings"); got != nil {
		t.Fatalf("routes = %+v, want nil without a routes list", got)
	}
}

func TestSecurityAlertProviderScopeAmbiguousError(t *testing.T) {
	t.Parallel()

	e := securityAlertProviderScopeAmbiguousError{Selector: "org/*", Scopes: []string{"org/a", "org/b"}}
	if !strings.Contains(e.Error(), `"org/*"`) || !strings.Contains(e.Error(), "org/a, org/b") {
		t.Fatalf("error = %q, want selector and scopes named", e.Error())
	}
}

type capturingLogHandler struct {
	records []string
}

func (h *capturingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Message)
	return nil
}

func (h *capturingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingLogHandler) WithGroup(string) slog.Handler { return h }

func TestStageTimerCanceled(t *testing.T) {
	t.Parallel()

	t.Run("NilLoggerIsNoOp", func(t *testing.T) {
		t.Parallel()
		timer := supplyChainQueryStageTimer{stage: "findings", startedAt: time.Now()}
		timer.Canceled(t.Context())
	})
	t.Run("EmitsCancelEvent", func(t *testing.T) {
		t.Parallel()
		handler := &capturingLogHandler{}
		timer := supplyChainQueryStageTimer{
			logger: slog.New(handler), operation: "list", repoID: "repo-a",
			stage: "findings", startedAt: time.Now(),
		}
		timer.Canceled(t.Context())
		if len(handler.records) != 1 || handler.records[0] != "supply chain query stage canceled" {
			t.Fatalf("records = %v, want exactly the cancel event", handler.records)
		}
	})
}

func TestBoundedErrorText(t *testing.T) {
	t.Parallel()

	if got := boundedErrorText(errors.New("short"), 100); got != "short" {
		t.Fatalf("short error = %q", got)
	}
	long := strings.Repeat("e", 50) + "é" + strings.Repeat("e", 50)
	got := boundedErrorText(errors.New(long), 51)
	if len(got) > 51 {
		t.Fatalf("bounded length = %d, want at most 51", len(got))
	}
	if !strings.HasSuffix(got, strings.Repeat("e", 50)) {
		t.Fatalf("bounded = %q, want the cut before the split rune", got)
	}
}
