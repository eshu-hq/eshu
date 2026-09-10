// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// fixedCrossScopeReadiness answers the readiness question with a canned result
// and counts calls, so a test can prove the lookup was consulted — or skipped.
//
// Its twin of the same name lives in the reducer root's
// cross_scope_readiness_floor_handler_test.go for the staying floor tests; the
// two bodies are identical by construction apart from the leaf-qualified
// spellings this package must use across the root/core seam (issue #6061),
// and must stay that way — a double that drifts from the root copy silently
// changes what the moved tests cover.
type fixedCrossScopeReadiness struct {
	ready bool
	// readyByProducer overrides ready for the named producer domains, so a test
	// can express the shape the aggregate bool could not: one producer ready
	// while another is still inside its activation window.
	readyByProducer map[reducercontract.Domain]bool
	err             error
	calls           int
}

// stringSliceContains reports whether values contains want. Its twins of the
// same name live in the reducer root
// (container_image_identity_root_test_doubles_test.go) and the cicdrun and
// containerimage suites; each keeps its own copy across the package seams
// (issue #6061).
func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// assertSubDurationsPresent fails if SubDurations is nil or any required key is
// absent. Its twin lives in the reducer root's
// materialization_subduration_helpers_test.go for the staying materialization
// suites; this copy serves the moved supplychain diagnostics tests across the
// root/core seam (issue #6061) and names the leaf result type directly.
func assertSubDurationsPresent(t *testing.T, result reducercontract.Result, domain string, keys []string) {
	t.Helper()
	if result.SubDurations == nil {
		t.Fatalf("%s: SubDurations must not be nil", domain)
	}
	for _, k := range keys {
		if _, ok := result.SubDurations[k]; !ok {
			t.Fatalf("%s: SubDurations missing key %q; got keys: %v", domain, k, mapKeys(result.SubDurations))
		}
	}
}

func mapKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// boolPtr is the core twin of the same-named helper in the reducer root
// (correlation_root_test_doubles_test.go) and correlation's source_test.go,
// kept for the moved tests that build *bool fields without importing another
// suite (issue #6061).
func boolPtr(value bool) *bool {
	return &value
}

// packageSourceRepositoryFact builds a repository fact envelope. Its twins of
// the same name live in the reducer root (correlation_root_test_doubles_test.go)
// and packages/correlation/source_test.go; this copy serves the moved
// supplychain tests across the root/core seam (issue #6061). The bodies are
// identical by construction, and must stay that way.
func packageSourceRepositoryFact(
	repositoryID string,
	repositoryName string,
	remoteURL string,
	tombstone bool,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactKind:      factload.FactKindRepository,
		ObservedAt:    observedAt,
		IsTombstone:   tombstone,
		StableFactKey: "repository:" + repositoryID,
		Payload: map[string]any{
			"graph_id":   repositoryID,
			"name":       repositoryName,
			"remote_url": remoteURL,
		},
	}
}

// stubContainerImageIdentityFactLoader is a local copy of
// internal/reducer/containerimage's own fixture, trimmed to the surface
// repository_anchor_ci_run_test.go exercises. It
// satisfies factload.FactLoader plus containerimage's private cross-scope
// loader interfaces structurally (Go interfaces are duck-typed), so the same
// stub works as a cross-scope loader without this package
// importing any of those private interface types. Copied verbatim from the
// reducer root's container_image_identity_root_test_doubles_test.go twin
// across the root/core seam (issue #6061).
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
// the package boundary, and its own caller
// (repository_anchor_ci_run_test.go) constructs this only
// as a zero-value stand-in and never inspects the returned result, so the
// simpler CanonicalWrites-only result is equivalent for its purposes. Copied
// from the reducer root's container_image_identity_root_test_doubles_test.go
// twin across the root/core seam (issue #6061).
type recordingContainerImageIdentityWriter struct {
	write containerimage.ContainerImageIdentityWrite
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
	write containerimage.ContainerImageIdentityWrite,
) (containerimage.ContainerImageIdentityWriteResult, error) {
	w.calls++
	w.write = write
	if w.err != nil {
		return containerimage.ContainerImageIdentityWriteResult{}, w.err
	}
	return containerimage.ContainerImageIdentityWriteResult{
		CanonicalWrites: len(write.Decisions),
	}, nil
}

// decisionsByRef indexes containerimage decisions by ImageRef for assertion
// convenience. Twin of the reducer root's
// container_image_identity_root_test_doubles_test.go helper and the
// containerimage suite's own copy (issue #6061).
func decisionsByRef(decisions []containerimage.ContainerImageIdentityDecision) map[string]containerimage.ContainerImageIdentityDecision {
	out := make(map[string]containerimage.ContainerImageIdentityDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.ImageRef] = decision
	}
	return out
}

// ciRunFact and ciArtifactFact build minimal ci.run / ci.artifact envelopes.
// They mirror the equivalent fixture builders in the ci_cd_run_correlation
// family's own test suite (go/internal/reducer/cicdrun), in
// internal/reducer/containerimage's copy, and in the reducer root's
// container_image_identity_root_test_doubles_test.go: none of the packages can
// share one package-private helper across the seams (issue #6061), so each
// keeps its own copy of these trivial builders. This copy serves the moved
// supplychain anchor test.
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

// ociManifestFact builds an OCI image-manifest fact envelope. Its twins of the
// same name live in the reducer root
// (container_image_identity_root_test_doubles_test.go) and the containerimage
// suite; this copy serves the moved supplychain anchor tests across the
// root/core seam (issue #6061). The bodies are identical by construction, and
// must stay that way.
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

// securityAlertEnvelopeMissingRepositoryID builds a
// security_alert.repository_alert envelope whose payload deliberately omits
// the required repository_id identity anchor, so the typed decode seam
// dead-letters it as input_invalid. It intentionally does NOT route through
// securityAlertEnvelope, which always stamps repository_id. Twin of the
// reducer root's security_alert_test_fixtures_test.go helper for the moved
// input-invalid tests (issue #6061).
func securityAlertEnvelopeMissingRepositoryID(factID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "security-alert:github:acme/api",
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

// packageConsumptionCorrelationEnvelope builds a package-consumption
// correlation fact envelope. Its twin of the same name lives in the reducer
// root (security_alert_test_fixtures_test.go) for the staying tests; this
// copy serves the moved supplychain tests across the root/core seam
// (issue #6061). The bodies are identical by construction, and must stay
// that way.
func packageConsumptionCorrelationEnvelope(factID string, repoID string, packageID string, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      repoID,
		GenerationID: "generation-1",
		FactKind:     correlation.PackageConsumptionFactKind,
		ObservedAt:   time.Date(2026, 5, 23, 11, 0, 0, 0, time.UTC),
		Payload: map[string]any{
			"repository_id": repoID,
			"package_id":    packageID,
			"relative_path": relativePath,
			"outcome":       "exact",
		},
	}
}

// securityAlertEnvelope builds a provider security-alert fact envelope. Its
// twins of the same name live in the reducer root
// (security_alert_test_fixtures_test.go) for the staying tests and in the
// securityalert suite; this copy serves the moved supplychain tests across
// the root/core seam (issue #6061). The bodies are identical by construction,
// and must stay that way.
func securityAlertEnvelope(factID string, repoID string, payload map[string]any) facts.Envelope {
	payload["repository_id"] = repoID
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          repoID,
		GenerationID:     "generation-1",
		FactKind:         facts.SecurityAlertRepositoryAlertFactKind,
		SchemaVersion:    facts.SecurityAlertSchemaVersionV1,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

// packageManifestDependencyFactWithMetadata builds a content-entity manifest
// dependency fact envelope. Its twins of the same name live in the reducer
// root (correlation_root_test_doubles_test.go) for the staying tests and in
// packages/correlation/source_test.go for the moved correlation tests; this
// copy serves the moved supplychain tests across the root/core seam
// (issue #6061). The bodies are identical by construction apart from the
// leaf-qualified fact-kind const, and must stay that way.
func packageManifestDependencyFactWithMetadata(
	repositoryID string,
	repositoryName string,
	relativePath string,
	dependencyName string,
	packageManager string,
	dependencyRange string,
	observedAt time.Time,
	metadata map[string]any,
) facts.Envelope {
	metadata["config_kind"] = "dependency"
	metadata["package_manager"] = packageManager
	metadata["value"] = dependencyRange
	if _, ok := metadata["section"]; !ok {
		metadata["section"] = "dependencies"
	}
	return facts.Envelope{
		FactID:        "manifest-dep:" + repositoryID + ":" + dependencyName,
		FactKind:      factload.FactKindContentEntity,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "git"},
		StableFactKey: "content_entity:" + repositoryID + ":" + dependencyName,
		Payload: map[string]any{
			"repo_id":         repositoryID,
			"relative_path":   relativePath,
			"entity_type":     "Variable",
			"entity_name":     dependencyName,
			"entity_metadata": metadata,
			"repo_name":       repositoryName,
		},
	}
}

// assertInputReady fails if SubSignals is nil, lacks "input_ready", or its value
// differs from want. Twin of the reducer root's
// materialization_subduration_helpers_test.go helper for the moved diagnostics
// tests (issue #6061).
func assertInputReady(t *testing.T, result reducercontract.Result, domain string, want float64) {
	t.Helper()
	if result.SubSignals == nil {
		t.Fatalf("%s: SubSignals must not be nil", domain)
	}
	got, ok := result.SubSignals[reducercontract.DiagnosticSignalInputReady]
	if !ok {
		t.Fatalf("%s: SubSignals missing %q; got keys: %v", domain, reducercontract.DiagnosticSignalInputReady, mapKeys(result.SubSignals))
	}
	if got != want {
		t.Fatalf("%s: input_ready = %v, want %v", domain, got, want)
	}
}

// assertWrittenRows fails if SubSignals["written_rows"] != want. It reads the
// SIGNAL, not result.CanonicalWrites, so a missing written_rows key fails.
// Twin of the reducer root's materialization_subduration_helpers_test.go helper
// for the moved diagnostics tests (issue #6061).
func assertWrittenRows(t *testing.T, result reducercontract.Result, domain string, want int) {
	t.Helper()
	if result.SubSignals == nil {
		t.Fatalf("%s: SubSignals must not be nil", domain)
	}
	got, ok := result.SubSignals[reducercontract.DiagnosticSignalWrittenRows]
	if !ok {
		t.Fatalf("%s: SubSignals missing %q; got keys: %v", domain, reducercontract.DiagnosticSignalWrittenRows, mapKeys(result.SubSignals))
	}
	if got != float64(want) {
		t.Fatalf("%s: written_rows = %v, want %d", domain, got, want)
	}
}

func (r *fixedCrossScopeReadiness) CrossScopeProducersReady(
	_ context.Context,
	consumer reducercontract.Domain,
	_ string,
	_ string,
) (crossscope.ProducerReadinessByDomain, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	// Built from the real catalog, so a canned answer always covers exactly the
	// producers the floor is about to ask about — the same contract the
	// production store owes.
	readiness := crossscope.ProducerReadinessByDomain{}
	for _, dependency := range crossscope.DependenciesForRegistration(consumer) {
		for _, producer := range dependency.ProducerDomains {
			ready := r.ready
			if override, named := r.readyByProducer[producer]; named {
				ready = override
			}
			readiness[producer] = ready
		}
	}
	return readiness, nil
}
