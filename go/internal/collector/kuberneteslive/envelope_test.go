// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func sampleIdentity() ObjectIdentity {
	return ObjectIdentity{
		ClusterID: "prod-us-east-1",
		APIGroup:  "apps",
		Version:   "v1",
		Resource:  "deployments",
		Namespace: "payments",
		Name:      "checkout",
		UID:       "uid-checkout-1",
	}
}

func TestNewPodTemplateEnvelopeShape(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity: sampleIdentity(),
		Containers: []ContainerSummary{
			{
				Name:          "app",
				Image:         "ghcr.io/acme/checkout@sha256:abc",
				Ports:         []int32{8080, 80},
				EnvKeys:       []string{"DATABASE_HOST", "API_KEY"},
				EnvFromSecret: true,
			},
		},
		ServiceAccount:      "checkout-sa",
		Selector:            map[string]string{"app": "checkout"},
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
		FencingToken:        7,
		ObservedAt:          observedAt,
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	if envelope.FactKind != facts.KubernetesPodTemplateFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.KubernetesPodTemplateFactKind)
	}
	if envelope.SchemaVersion != facts.KubernetesPodTemplateSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.KubernetesPodTemplateSchemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want reported", envelope.SourceConfidence)
	}
	if envelope.FencingToken != 7 {
		t.Fatalf("FencingToken = %d, want 7", envelope.FencingToken)
	}
	wantScope, _ := ClusterScopeID("prod-us-east-1")
	if envelope.ScopeID != wantScope {
		t.Fatalf("ScopeID = %q, want %q", envelope.ScopeID, wantScope)
	}
	if !envelope.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %v, want %v", envelope.ObservedAt, observedAt)
	}

	// Env var NAMES are kept; values must never appear because none are read.
	envKeys, ok := envelope.Payload["containers"].([]map[string]any)
	if !ok || len(envKeys) != 1 {
		t.Fatalf("containers payload missing or wrong shape: %#v", envelope.Payload["containers"])
	}
	keys, _ := envKeys[0]["env_keys"].([]string)
	if strings.Join(keys, ",") != "API_KEY,DATABASE_HOST" {
		t.Fatalf("env_keys = %v, want sorted names only", keys)
	}
	if envKeys[0]["env_from_secret"] != true {
		t.Fatalf("env_from_secret = %v, want true", envKeys[0]["env_from_secret"])
	}
}

func TestPodTemplateRedactionNoSecretValues(t *testing.T) {
	t.Parallel()

	// The payload must never contain any of these sentinel secret strings;
	// only metadata (names, images, ports) is emitted.
	secret := "super-secret-password-value"
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		Containers:          []ContainerSummary{{Name: "app", Image: "img:1", EnvKeys: []string{"DB_PASSWORD"}}},
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	if payloadContains(envelope.Payload, secret) {
		t.Fatalf("payload leaked a secret value")
	}
	// Sanity: the key name is retained, the value is not present anywhere.
	if !payloadContains(envelope.Payload, "DB_PASSWORD") {
		t.Fatalf("payload should retain env var key name DB_PASSWORD")
	}
}

func TestPodTemplateEnvelopeIsDeterministic(t *testing.T) {
	t.Parallel()

	build := func() facts.Envelope {
		env, err := NewPodTemplateEnvelope(PodTemplateObservation{
			Identity:            sampleIdentity(),
			Containers:          []ContainerSummary{{Name: "app", Image: "img:1"}},
			GenerationID:        "gen-1",
			CollectorInstanceID: "k8s-prod",
			ObservedAt:          time.Unix(100, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
		}
		return env
	}
	first := build()
	second := build()
	if first.FactID != second.FactID {
		t.Fatalf("FactID is not deterministic: %q vs %q", first.FactID, second.FactID)
	}
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("StableFactKey is not deterministic")
	}
}

func TestNewRelationshipEnvelope(t *testing.T) {
	t.Parallel()

	owner := sampleIdentity()
	owned := ObjectIdentity{
		ClusterID: "prod-us-east-1",
		APIGroup:  "apps",
		Version:   "v1",
		Resource:  "replicasets",
		Namespace: "payments",
		Name:      "checkout-abc",
		UID:       "uid-rs-1",
	}
	envelope, err := NewRelationshipEnvelope(RelationshipObservation{
		ClusterID:           "prod-us-east-1",
		Type:                RelationshipOwnerReference,
		From:                owned,
		To:                  owner,
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewRelationshipEnvelope() error = %v", err)
	}
	if envelope.FactKind != facts.KubernetesRelationshipFactKind {
		t.Fatalf("FactKind = %q, want relationship", envelope.FactKind)
	}
	if envelope.Payload["relationship_type"] != string(RelationshipOwnerReference) {
		t.Fatalf("relationship_type = %v", envelope.Payload["relationship_type"])
	}
	if envelope.Payload["from_object_id"] != owned.ObjectID() {
		t.Fatalf("from_object_id mismatch")
	}
	if envelope.Payload["to_object_id"] != owner.ObjectID() {
		t.Fatalf("to_object_id mismatch")
	}
}

func TestNewWarningEnvelope(t *testing.T) {
	t.Parallel()

	envelope, err := NewWarningEnvelope(WarningObservation{
		ClusterID:           "prod-us-east-1",
		Reason:              WarningForbiddenResource,
		ResourceScope:       ResourceScopeServices,
		Message:             "list services forbidden at https://api.example.com?token=leak",
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewWarningEnvelope() error = %v", err)
	}
	if envelope.FactKind != facts.KubernetesWarningFactKind {
		t.Fatalf("FactKind = %q, want warning", envelope.FactKind)
	}
	if envelope.Payload["reason"] != WarningForbiddenResource {
		t.Fatalf("reason = %v", envelope.Payload["reason"])
	}
	message, _ := envelope.Payload["message"].(string)
	if strings.Contains(message, "token=leak") {
		t.Fatalf("warning message leaked sensitive query param: %q", message)
	}
}

func TestEnvelopeBoundaryValidation(t *testing.T) {
	t.Parallel()

	if _, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		CollectorInstanceID: "k8s-prod",
	}); err == nil {
		t.Fatalf("expected error for blank generation_id")
	}
	if _, err := NewWarningEnvelope(WarningObservation{
		ClusterID:           "c",
		Reason:              "",
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	}); err == nil {
		t.Fatalf("expected error for blank reason")
	}
	if _, err := NewRelationshipEnvelope(RelationshipObservation{
		ClusterID:           "c",
		Type:                RelationshipOwnerReference,
		From:                ObjectIdentity{},
		To:                  sampleIdentity(),
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	}); err == nil {
		t.Fatalf("expected error for invalid from identity")
	}
}

func payloadContains(payload map[string]any, needle string) bool {
	for _, value := range payload {
		if valueContains(value, needle) {
			return true
		}
	}
	return false
}

func valueContains(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, item := range typed {
			if strings.Contains(item, needle) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if payloadContains(item, needle) {
				return true
			}
		}
	case map[string]any:
		return payloadContains(typed, needle)
	case map[string]string:
		for k, v := range typed {
			if strings.Contains(k, needle) || strings.Contains(v, needle) {
				return true
			}
		}
	}
	return false
}

func TestNormalizeCRIImageID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "docker-pullable with repo@sha256",
			raw:  "docker-pullable://docker.io/library/nginx@sha256:abc123456789",
			want: "docker.io/library/nginx@sha256:abc123456789",
		},
		{
			name: "docker scheme with repo@sha256",
			raw:  "docker://docker.io/library/nginx@sha256:abc123456789",
			want: "docker.io/library/nginx@sha256:abc123456789",
		},
		{
			name: "bare repo@sha256 no scheme",
			raw:  "docker.io/library/nginx@sha256:abc123456789",
			want: "docker.io/library/nginx@sha256:abc123456789",
		},
		{
			name: "bare sha256 no repo",
			raw:  "sha256:abc123456789",
			want: "",
		},
		{
			name: "docker-pullable sha256 no repo",
			raw:  "docker-pullable://sha256:abc123456789",
			want: "",
		},
		{
			name: "docker scheme sha256 no repo",
			raw:  "docker://sha256:abc123456789",
			want: "",
		},
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "cri-o scheme",
			raw:  "cri-o://docker.io/myapp@sha256:def456789abc",
			want: "docker.io/myapp@sha256:def456789abc",
		},
		{
			name: "containerd tag ref (no digest)",
			raw:  "docker.io/library/nginx:1.25",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCRIImageID(tt.raw)
			if got != tt.want {
				t.Fatalf("NormalizeCRIImageID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestNewPodTemplateEnvelopeEmitsResolvedImageDigest locks the wire contract
// for #5432: a container's CRI-resolved digest must survive the typed
// EncodeKubernetesLivePodTemplate seam into the emitted payload. The encoder
// enumerates fields by hand, so a field added to the struct but not the
// encoder is silently dropped — this test fails in that case.
func TestNewPodTemplateEnvelopeEmitsResolvedImageDigest(t *testing.T) {
	t.Parallel()

	digest := "ghcr.io/acme/checkout@sha256:deadbeef"
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity: sampleIdentity(),
		Containers: []ContainerSummary{
			{Name: "app", Image: "ghcr.io/acme/checkout:1.2.3", ResolvedImageDigest: digest},
			{Name: "sidecar", Image: "ghcr.io/acme/sidecar@sha256:abc"},
		},
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	containers, ok := envelope.Payload["containers"].([]map[string]any)
	if !ok || len(containers) != 2 {
		t.Fatalf("payload containers = %#v, want 2 entries", envelope.Payload["containers"])
	}
	if got := containers[0]["resolved_image_digest"]; got != digest {
		t.Fatalf("containers[0].resolved_image_digest = %#v, want %q", got, digest)
	}
	if _, present := containers[1]["resolved_image_digest"]; present {
		t.Fatalf("containers[1].resolved_image_digest present = %#v, want key omitted when no digest observed", containers[1]["resolved_image_digest"])
	}
}

// TestNewPodTemplateEnvelopeEmitsRuntimeStatus locks the wire contract for
// #5431: a workload's DESIRED replica count (Spec.Replicas) and OBSERVED
// runtime-status fields (ready/available replicas, pod phase) must survive
// the typed EncodeKubernetesLivePodTemplate seam into the emitted payload. The
// encoder enumerates fields by hand, so a field added to the struct but not
// the encoder is silently dropped — this test fails in that case.
func TestNewPodTemplateEnvelopeEmitsRuntimeStatus(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	ready := int32(2)
	available := int32(1)
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
		DesiredReplicas:     &desired,
		ReadyReplicas:       &ready,
		AvailableReplicas:   &available,
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	if got := envelope.Payload["desired_replicas"]; got != int32(3) {
		t.Fatalf("payload desired_replicas = %#v, want 3", got)
	}
	if got := envelope.Payload["ready_replicas"]; got != int32(2) {
		t.Fatalf("payload ready_replicas = %#v, want 2", got)
	}
	if got := envelope.Payload["available_replicas"]; got != int32(1) {
		t.Fatalf("payload available_replicas = %#v, want 1", got)
	}
	if _, present := envelope.Payload["pod_phase"]; present {
		t.Fatalf("payload pod_phase present = %#v, want key omitted when not observed", envelope.Payload["pod_phase"])
	}
}

// TestNewPodTemplateEnvelopeEmitsAnnotations locks the wire contract for
// #5471 F2: an optional Annotations map — carrying, among other keys, the
// ArgoCD argocd.argoproj.io/tracking-id declared->live identity signal — must
// survive the typed EncodeKubernetesLivePodTemplate seam into the emitted
// payload when observed, and the "annotations" key must be omitted entirely
// (not emitted as an empty map) when no annotations were observed, matching
// the existing PodTemplate optional-map contract (Selector, Labels).
func TestNewPodTemplateEnvelopeEmitsAnnotations(t *testing.T) {
	t.Parallel()

	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
		Annotations: map[string]string{
			"argocd.argoproj.io/tracking-id": "checkout:apps/Deployment:payments/checkout",
		},
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	annotations, ok := envelope.Payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("payload annotations = %#v, want map[string]string", envelope.Payload["annotations"])
	}
	if got := annotations["argocd.argoproj.io/tracking-id"]; got != "checkout:apps/Deployment:payments/checkout" {
		t.Fatalf("payload annotations tracking-id = %q, want %q", got, "checkout:apps/Deployment:payments/checkout")
	}
}

// TestNewPodTemplateEnvelopeOmitsAnnotationsWhenAbsent proves the negative
// case for #5471 F2: a PodTemplateObservation with no Annotations observed
// must not emit an "annotations" key at all, preserving backward
// compatibility with collectors and decoders that predate the field.
func TestNewPodTemplateEnvelopeOmitsAnnotationsWhenAbsent(t *testing.T) {
	t.Parallel()

	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	if _, present := envelope.Payload["annotations"]; present {
		t.Fatalf("payload annotations present = %#v, want key omitted when not observed", envelope.Payload["annotations"])
	}
}

// TestNewPodTemplateEnvelopePodPhaseOmittedWhenAbsentReplicasSet locks the
// pod-observation shape for #5431: a pod observation carries PodPhase but no
// replica fields, and the emitted payload must reflect exactly that — the
// pod_phase key present, the replica keys omitted rather than zeroed.
func TestNewPodTemplateEnvelopePodPhaseOmittedWhenAbsentReplicasSet(t *testing.T) {
	t.Parallel()

	phase := "Running"
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            sampleIdentity(),
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
		PodPhase:            &phase,
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	if got := envelope.Payload["pod_phase"]; got != "Running" {
		t.Fatalf("payload pod_phase = %#v, want %q", got, "Running")
	}
	for _, key := range []string{"desired_replicas", "ready_replicas", "available_replicas"} {
		if _, present := envelope.Payload[key]; present {
			t.Fatalf("payload %s present = %#v, want key omitted (Pod observation carries no replica fields)", key, envelope.Payload[key])
		}
	}
}
