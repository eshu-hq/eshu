// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is a local copy of test fixtures and doubles the
// container-image-identity family's own test suite defined before it moved to
// internal/reducer/containerimage (issue #6061). Go test files cannot share
// unexported symbols across a package boundary, and several still-in-root
// families' tests (cross_scope_readiness_floor_handler_test.go,
// supply_chain_impact_repository_anchor_ci_run_test.go, defaults_test.go,
// defaults_cicd_test.go, provenance_edge_submission_metrics_test.go, the
// aws_*/gcp_*/iam_*/security_group_* materialization tests, and the
// supply_chain_impact_* reachability tests) still reference these under their
// original unqualified names, so root keeps this trimmed copy rather than
// requiring every one of those files to import containerimage and requalify
// every call site. Mirrors internal/reducer/secretsiam's writer test gaining
// a local exec double for the same reason.

// ciRunFact and ciArtifactFact build minimal ci.run / ci.artifact envelopes.
// They mirror the equivalent fixture builders in the ci_cd_run_correlation
// family's own test suite (go/internal/reducer/cicdrun) and in
// internal/reducer/containerimage's own copy: none of the three packages can
// share one package-private helper across the root/cicdrun/containerimage
// seam (issue #6061), so each keeps its own copy of these trivial builders.
func ciRunFact(runID, provider, repositoryID, commitSHA string) facts.Envelope {
	return facts.Envelope{
		FactID:           "ci.run:" + runID,
		FactKind:         facts.CICDRunFactKind,
		SourceRef:        facts.Ref{SourceSystem: "ci_cd_run"},
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":      provider,
			"run_id":        runID,
			"run_attempt":   "1",
			"repository_id": repositoryID,
			"commit_sha":    commitSHA,
			"status":        "completed",
			"result":        "success",
		},
	}
}

func ciArtifactFact(factID, runID, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         facts.CICDArtifactFactKind,
		SourceConfidence: facts.SourceConfidenceReported,
		Payload: map[string]any{
			"provider":        "github_actions",
			"run_id":          runID,
			"run_attempt":     "1",
			"artifact_type":   "container_image",
			"artifact_digest": digest,
		},
	}
}

func containerImageIdentityFact(factID, repositoryID, imageRef, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		FactKind:         containerImageIdentityFactKind,
		SourceConfidence: facts.SourceConfidenceInferred,
		Payload: map[string]any{
			"repository_id": repositoryID,
			"image_ref":     imageRef,
			"digest":        digest,
		},
	}
}

// stringSliceContains reports whether want appears in values.
func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// decisionsByRef indexes decisions by ImageRef for assertion convenience.
func decisionsByRef(decisions []ContainerImageIdentityDecision) map[string]ContainerImageIdentityDecision {
	out := make(map[string]ContainerImageIdentityDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.ImageRef] = decision
	}
	return out
}

func ociManifestFact(factID string, digest string) facts.Envelope {
	return ociImageFact(factID, facts.OCIImageManifestFactKind, digest, map[string]any{})
}

func ociImageFact(factID string, kind string, digest string, extra map[string]any) facts.Envelope {
	payload := map[string]any{
		"registry":      "registry.example.com",
		"repository":    "team/api",
		"repository_id": "oci-registry://registry.example.com/team/api",
		"digest":        digest,
		"media_type":    "application/vnd.oci.image.manifest.v1+json",
	}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "oci-registry://registry.example.com/team/api",
		GenerationID:     "generation-oci",
		FactKind:         kind,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "oci_registry",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "oci_registry",
		},
		Payload: payload,
	}
}

// stubContainerImageIdentityFactLoader is a local copy of
// internal/reducer/containerimage's own fixture, trimmed to the surface
// supply_chain_impact_repository_anchor_ci_run_test.go exercises. It
// satisfies factload.FactLoader plus containerimage's private cross-scope
// loader interfaces structurally (Go interfaces are duck-typed), so the same
// stub works as ContainerImageIdentityHandler.FactLoader without this package
// importing any of those private interface types.
type stubContainerImageIdentityFactLoader struct {
	scopeFacts                 []facts.Envelope
	active                     []facts.Envelope
	kindCalls                  [][]string
	activeCall                 int
	slsaActive                 []facts.Envelope
	slsaActiveCall             int
	ciActive                   []facts.Envelope
	ciActiveCall               int
	ciActiveOwnerRepositoryIDs []string
	warnings                   []facts.Envelope
	warningCalls               int
	warningErr                 error
}

func (s *stubContainerImageIdentityFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageIdentityFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.activeCall++
	return append([]facts.Envelope(nil), s.active...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageIdentityWarnings(
	context.Context,
) ([]facts.Envelope, error) {
	s.warningCalls++
	return append([]facts.Envelope(nil), s.warnings...), s.warningErr
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageSLSAFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.slsaActiveCall++
	return append([]facts.Envelope(nil), s.slsaActive...), nil
}

func (s *stubContainerImageIdentityFactLoader) ListActiveContainerImageCIFacts(
	_ context.Context,
	ownerRepositoryID string,
) ([]facts.Envelope, error) {
	s.ciActiveCall++
	s.ciActiveOwnerRepositoryIDs = append(s.ciActiveOwnerRepositoryIDs, ownerRepositoryID)
	return append([]facts.Envelope(nil), s.ciActive...), nil
}

// recordingContainerImageIdentityWriter is a trimmed local copy of
// internal/reducer/containerimage's own fixture. The upstream version also
// sets ContainerImageIdentityWriteResult's package-private
// effectiveDecisions/effectiveProjectionPresent fields to feed the graph
// projection path; this package cannot reach those unexported fields across
// the package boundary, and its own callers (defaults_test.go,
// supply_chain_impact_repository_anchor_ci_run_test.go) construct this only
// as a zero-value stand-in and never inspect the returned result, so the
// simpler CanonicalWrites-only result is equivalent for their purposes.
type recordingContainerImageIdentityWriter struct {
	write ContainerImageIdentityWrite
	calls int
	err   error
}

func (*recordingContainerImageIdentityWriter) ContainerImageIdentityActivationEpoch(
	context.Context,
	string,
	string,
) (int64, error) {
	return 1, nil
}

func (w *recordingContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	_ context.Context,
	write ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	w.calls++
	w.write = write
	if w.err != nil {
		return ContainerImageIdentityWriteResult{}, w.err
	}
	return ContainerImageIdentityWriteResult{
		CanonicalWrites: len(write.Decisions),
	}, nil
}

// recordingContainerImageProvenanceEdgeWriter records every BUILT_FROM
// write/retract call for assertion.
type recordingContainerImageProvenanceEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeSources []string
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageProvenanceEdgeWriter) WriteBuiltFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, evidenceSource string,
) error {
	w.writeRows = append(w.writeRows, rows)
	w.writeSources = append(w.writeSources, evidenceSource)
	return w.writeErr
}

func (w *recordingContainerImageProvenanceEdgeWriter) RetractBuiltFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

// recordingContainerImageDerivedFromEdgeWriter records every DERIVED_FROM
// write/retract call for assertion.
type recordingContainerImageDerivedFromEdgeWriter struct {
	retractCalls []string
	writeRows    [][]map[string]any
	writeErr     error
	retractErr   error
}

func (w *recordingContainerImageDerivedFromEdgeWriter) WriteDerivedFromEdges(
	_ context.Context, rows []map[string]any, _ string, _ string, _ string,
) error {
	w.writeRows = append(w.writeRows, rows)
	return w.writeErr
}

func (w *recordingContainerImageDerivedFromEdgeWriter) RetractDerivedFromEdges(
	_ context.Context, _ string, _ string, evidenceSource string,
) error {
	w.retractCalls = append(w.retractCalls, evidenceSource)
	return w.retractErr
}

// metricHasAttrs reports whether metricName has a positive int64 sum data
// point matching every key/value pair in attrs. Used by materialization tests
// across several root-staying families (AWS, GCP, IAM escalation, security
// group reachability) that assert on emitted counters.
func metricHasAttrs(rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != metricName {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				matches := true
				for key, want := range attrs {
					got, ok := point.Attributes.Value(attribute.Key(key))
					if !ok || got.AsString() != want {
						matches = false
						break
					}
				}
				if matches && point.Value > 0 {
					return true
				}
			}
		}
	}
	return false
}
