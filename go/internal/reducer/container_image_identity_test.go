package reducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

const testContainerDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testOtherContainerDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type stubContainerImageIdentityFactLoader struct {
	scopeFacts []facts.Envelope
	active     []facts.Envelope
	kindCalls  [][]string
	activeCall int
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

type recordingContainerImageIdentityWriter struct {
	write ContainerImageIdentityWrite
	calls int
	err   error
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
		CanonicalWrites: len(containerImageIdentityCanonicalDecisions(write.Decisions)),
	}, nil
}

func TestBuildContainerImageIdentityDecisionsClassifiesDigestAndTagTruth(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gitImageRefFact("git-digest", "registry.example.com/team/api@"+testContainerDigest),
		gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		ociManifestFact("oci-manifest", testContainerDigest),
		ociTagFact("oci-tag", "prod", testContainerDigest, false, ""),
	})

	got := decisionsByRef(decisions)
	assertContainerImageDecision(t, got["registry.example.com/team/api@"+testContainerDigest],
		ContainerImageIdentityExactDigest, testContainerDigest, 1)
	assertContainerImageDecision(t, got["registry.example.com/team/api:prod"],
		ContainerImageIdentityTagResolved, testContainerDigest, 1)
}

func TestBuildContainerImageIdentityDecisionsRejectsWeakMissingAndAmbiguousTags(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		gitImageRefFact("git-missing", "registry.example.com/team/missing:prod"),
		gitImageRefFact("git-ambiguous", "registry.example.com/team/api:prod"),
		ociTagFact("oci-tag-1", "prod", testContainerDigest, false, ""),
		ociTagFact("oci-tag-2", "prod", testOtherContainerDigest, false, ""),
	})

	got := decisionsByRef(decisions)
	assertContainerImageDecision(t, got["registry.example.com/team/missing:prod"],
		ContainerImageIdentityUnresolved, "", 0)
	assertContainerImageDecision(t, got["registry.example.com/team/api:prod"],
		ContainerImageIdentityAmbiguousTag, "", 0)
}

func TestBuildContainerImageIdentityDecisionsDetectsStaleRuntimeTags(t *testing.T) {
	t.Parallel()

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		awsImageRelationshipFact(
			"aws-runtime-image",
			"registry.example.com/team/api:prod",
			"registry.example.com/team/api@"+testOtherContainerDigest,
		),
		ociTagFact("oci-tag", "prod", testContainerDigest, true, testOtherContainerDigest),
	})

	got := decisionsByRef(decisions)
	assertContainerImageDecision(t, got["registry.example.com/team/api:prod"],
		ContainerImageIdentityStaleTag, testContainerDigest, 0)
}

func TestContainerImageIdentityHandlerLoadsActiveRegistryFactsAndEmitsOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		},
		active: []facts.Envelope{
			ociTagFact("oci-tag", "prod", testContainerDigest, false, ""),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:  loader,
		Writer:      writer,
		Instruments: inst,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "container image references observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteContainerImageIdentityDecisions() calls = %d, want 1", writer.calls)
	}
	if loader.activeCall != 1 {
		t.Fatalf("ListActiveContainerImageIdentityFacts() calls = %d, want 1", loader.activeCall)
	}
	if got, want := strings.Join(loader.kindCalls[0], ","), strings.Join(containerImageIdentityFactKinds(), ","); got != want {
		t.Fatalf("ListFactsByKind() kinds = %q, want %q", got, want)
	}
	if !strings.Contains(result.EvidenceSummary, "tag_resolved=1") {
		t.Fatalf("EvidenceSummary = %q, want tag_resolved count", result.EvidenceSummary)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if !metricHasAttrs(rm, "eshu_dp_container_image_identity_decisions_total", map[string]string{
		telemetry.MetricDimensionDomain:  string(DomainContainerImageIdentity),
		telemetry.MetricDimensionOutcome: string(ContainerImageIdentityTagResolved),
	}) {
		t.Fatal("container image identity decision counter with tag_resolved outcome not emitted")
	}
}

func TestContainerImageIdentityHandlerDoesNotEmitOutcomesBeforeDurableWrite(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{
			scopeFacts: []facts.Envelope{
				gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
			},
			active: []facts.Envelope{
				ociTagFact("oci-tag", "prod", testContainerDigest, false, ""),
			},
		},
		Writer: &recordingContainerImageIdentityWriter{
			err: errors.New("database unavailable"),
		},
		Instruments: inst,
	}

	_, err = handler.Handle(context.Background(), Intent{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Domain:       DomainContainerImageIdentity,
		Cause:        "container image references observed",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want durable write error")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if metricHasAttrs(rm, "eshu_dp_container_image_identity_decisions_total", map[string]string{
		telemetry.MetricDimensionDomain:  string(DomainContainerImageIdentity),
		telemetry.MetricDimensionOutcome: string(ContainerImageIdentityTagResolved),
	}) {
		t.Fatal("container image identity decision counter emitted before durable write succeeded")
	}
}

func TestPostgresContainerImageIdentityWriterPersistsCanonicalDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				EvidenceFactIDs:  []string{"git-tag", "oci-tag"},
				IdentityStrength: "tag_observation_with_digest",
			},
			{
				ImageRef:        "registry.example.com/team/missing:prod",
				Outcome:         ContainerImageIdentityUnresolved,
				Reason:          "no registry digest observation matched the image reference",
				CanonicalWrites: 0,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[3], containerImageIdentityFactKind; got != want {
		t.Fatalf("fact_kind = %v, want %v", got, want)
	}

	payloadBytes, ok := db.execs[0].args[14].([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", db.execs[0].args[14])
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["digest"], testContainerDigest; got != want {
		t.Fatalf("payload digest = %#v, want %q", got, want)
	}
	if got, want := payload["identity_strength"], "tag_observation_with_digest"; got != want {
		t.Fatalf("payload identity_strength = %#v, want %q", got, want)
	}
}

func TestPostgresContainerImageIdentityWriterUsesStableTagReferenceIdentity(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB: db,
	}
	write := ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("first WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	write.Decisions[0].Digest = testOtherContainerDigest
	_, err = writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("second WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if got, want := db.execs[1].args[0], db.execs[0].args[0]; got != want {
		t.Fatalf("fact_id changed after tag digest moved: first=%v second=%v", want, got)
	}
	if got, want := db.execs[1].args[4], db.execs[0].args[4]; got != want {
		t.Fatalf("stable_fact_key changed after tag digest moved: first=%v second=%v", want, got)
	}
	firstPayload := unmarshalContainerImageIdentityPayload(t, db.execs[0].args[14])
	secondPayload := unmarshalContainerImageIdentityPayload(t, db.execs[1].args[14])
	if got, want := secondPayload["canonical_id"], firstPayload["canonical_id"]; got != want {
		t.Fatalf("canonical_id changed after tag digest moved: first=%v second=%v", want, got)
	}
	if got, want := secondPayload["digest"], testOtherContainerDigest; got != want {
		t.Fatalf("second payload digest = %#v, want %q", got, want)
	}
}

func TestPostgresContainerImageIdentityWriterPublishesKnownTruthLayers(t *testing.T) {
	t.Parallel()

	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresContainerImageIdentityWriter{
		DB: db,
	}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:     "intent-image-identity",
		ScopeID:      "repo:team-api",
		GenerationID: "generation-git",
		SourceSystem: "git",
		Cause:        "container image references observed",
		Decisions: []ContainerImageIdentityDecision{
			{
				ImageRef:         "registry.example.com/team/api:prod",
				Digest:           testContainerDigest,
				RepositoryID:     "oci-registry://registry.example.com/team/api",
				Outcome:          ContainerImageIdentityTagResolved,
				Reason:           "tag resolved to one registry digest observation",
				CanonicalWrites:  1,
				IdentityStrength: "tag_observation_with_digest",
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions() error = %v, want nil", err)
	}
	payload := unmarshalContainerImageIdentityPayload(t, db.execs[0].args[14])
	layers, ok := payload["source_layers"].([]any)
	if !ok {
		t.Fatalf("payload source_layers = %T, want []any", payload["source_layers"])
	}
	if len(layers) == 0 {
		t.Fatal("payload source_layers is empty, want known truth layers")
	}
	for _, raw := range layers {
		if _, err := truth.ParseLayer(fmt.Sprint(raw)); err != nil {
			t.Fatalf("source layer %q is not in truth model: %v", raw, err)
		}
	}
}

func decisionsByRef(decisions []ContainerImageIdentityDecision) map[string]ContainerImageIdentityDecision {
	out := make(map[string]ContainerImageIdentityDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.ImageRef] = decision
	}
	return out
}

func unmarshalContainerImageIdentityPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	payloadBytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func assertContainerImageDecision(
	t *testing.T,
	decision ContainerImageIdentityDecision,
	outcome ContainerImageIdentityOutcome,
	digest string,
	writes int,
) {
	t.Helper()
	if decision.Outcome != outcome {
		t.Fatalf("Outcome = %q, want %q for %#v", decision.Outcome, outcome, decision)
	}
	if decision.Digest != digest {
		t.Fatalf("Digest = %q, want %q for %#v", decision.Digest, digest, decision)
	}
	if decision.CanonicalWrites != writes {
		t.Fatalf("CanonicalWrites = %d, want %d for %#v", decision.CanonicalWrites, writes, decision)
	}
}

func gitImageRefFact(factID string, imageRefs ...string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "repo:team-api",
		GenerationID:     "generation-git",
		FactKind:         factKindContentEntity,
		SchemaVersion:    "1.0.0",
		CollectorKind:    "git",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "git",
		},
		Payload: map[string]any{
			"uid":         "entity:deployment",
			"entity_type": "KubernetesResource",
			"metadata": map[string]any{
				"container_images": imageRefs,
			},
		},
	}
}

func ociManifestFact(factID string, digest string) facts.Envelope {
	return ociImageFact(factID, facts.OCIImageManifestFactKind, digest, map[string]any{})
}

func ociTagFact(factID string, tag string, digest string, mutated bool, previousDigest string) facts.Envelope {
	return ociImageFact(factID, facts.OCIImageTagObservationFactKind, digest, map[string]any{
		"tag":             tag,
		"resolved_digest": digest,
		"mutated":         mutated,
		"previous_digest": previousDigest,
	})
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

func awsImageRelationshipFact(factID string, imageRef string, resolvedImageRef string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          "aws:123456789012:us-east-1:lambda",
		GenerationID:     "generation-aws",
		FactKind:         facts.AWSRelationshipFactKind,
		SchemaVersion:    facts.AWSRelationshipSchemaVersion,
		CollectorKind:    "aws_cloud",
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"relationship_type":  "lambda_function_uses_image",
			"source_resource_id": "arn:aws:lambda:us-east-1:123456789012:function:team-api",
			"target_resource_id": imageRef,
			"target_type":        "container_image",
			"attributes": map[string]any{
				"resolved_image_uri": resolvedImageRef,
			},
		},
	}
}

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

func TestContainerImageIdentityCanonicalDecisionFilter(t *testing.T) {
	t.Parallel()

	decisions := containerImageIdentityCanonicalDecisions([]ContainerImageIdentityDecision{
		{Outcome: ContainerImageIdentityTagResolved, CanonicalWrites: 1},
		{Outcome: ContainerImageIdentityUnresolved, CanonicalWrites: 0},
	})
	if !slices.EqualFunc(decisions, []ContainerImageIdentityDecision{
		{Outcome: ContainerImageIdentityTagResolved, CanonicalWrites: 1},
	}, func(left, right ContainerImageIdentityDecision) bool {
		return left.Outcome == right.Outcome && left.CanonicalWrites == right.CanonicalWrites
	}) {
		t.Fatalf("canonical decisions = %#v, want only writable decision", decisions)
	}
}
