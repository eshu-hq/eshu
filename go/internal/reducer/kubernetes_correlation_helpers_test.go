// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testK8sCluster    = "prod-us-east-1"
	testK8sNamespace  = "checkout"
	testK8sRegistry   = "registry.example.com"
	testK8sRepository = "team/checkout"
	testK8sDigest     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	testK8sDigest2    = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

type stubKubernetesCorrelationFactLoader struct {
	scopeFacts  []facts.Envelope
	activeFacts []facts.Envelope
	kindCalls   [][]string
	activeCalls int
}

func (s *stubKubernetesCorrelationFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubKubernetesCorrelationFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	kinds []string,
) ([]facts.Envelope, error) {
	s.kindCalls = append(s.kindCalls, append([]string(nil), kinds...))
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *stubKubernetesCorrelationFactLoader) ListActiveContainerImageIdentityFacts(
	context.Context,
) ([]facts.Envelope, error) {
	s.activeCalls++
	return append([]facts.Envelope(nil), s.activeFacts...), nil
}

type recordingKubernetesCorrelationWriter struct {
	write KubernetesCorrelationWrite
	calls int
}

func (w *recordingKubernetesCorrelationWriter) WriteKubernetesCorrelations(
	_ context.Context,
	write KubernetesCorrelationWrite,
) (KubernetesCorrelationWriteResult, error) {
	w.calls++
	w.write = write
	return KubernetesCorrelationWriteResult{FactsWritten: len(write.Decisions)}, nil
}

// podTemplateFact builds a kubernetes_live.pod_template fact envelope carrying
// one workload's redacted image references and selector, the live workload
// substrate the correlation index reads.
func podTemplateFact(factID, name, uid string, imageRefs []string, selector map[string]string, tombstone bool) facts.Envelope {
	return podTemplateFactWithResolvedDigests(factID, name, uid, imageRefs, selector, tombstone, nil)
}

// podTemplateFactWithResolvedDigests builds a pod_template fact with optional
// CRI-resolved image digests keyed by the container's declared image ref.
// A nil or empty resolved map emits no resolved_image_digest field (backward-
// compatible fixture for pre-CRI-digest behavior). A non-empty entry sets
// resolved_image_digest on the container carrying that image ref.
func podTemplateFactWithResolvedDigests(
	factID, name, uid string,
	imageRefs []string,
	selector map[string]string,
	tombstone bool,
	resolved map[string]string,
) facts.Envelope {
	objectID := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/" + name
	containers := make([]any, 0, len(imageRefs))
	for i, ref := range imageRefs {
		container := map[string]any{
			"name":  name + "-c" + string(rune('0'+i)),
			"image": ref,
			"init":  false,
		}
		if d, ok := resolved[ref]; ok && d != "" {
			container["resolved_image_digest"] = d
		}
		containers = append(containers, container)
	}
	sel := make(map[string]any, len(selector))
	for k, v := range selector {
		sel[k] = v
	}
	return facts.Envelope{
		FactID:      factID,
		FactKind:    facts.KubernetesPodTemplateFactKind,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"cluster_id":             testK8sCluster,
			"object_id":              objectID,
			"group_version_resource": "apps/v1/deployments",
			"namespace":              testK8sNamespace,
			"name":                   name,
			"uid":                    uid,
			"service_account":        "default",
			"image_refs":             imageRefs,
			"containers":             containers,
			"selector":               sel,
			"labels":                 sel,
			"correlation_anchors":    append([]string{objectID}, imageRefs...),
		},
	}
}

// k8sRelationshipFact builds a kubernetes_live.relationship fact for a directed
// edge between two live objects (owner_reference is structural; a
// selector-derived workload edge cannot prove exact ownership).
func k8sRelationshipFact(factID, relType, fromName, toName string) facts.Envelope {
	from := "k8s://" + testK8sCluster + "/apps/v1/deployments/" + testK8sNamespace + "/" + fromName
	to := "k8s://" + testK8sCluster + "/v1/pods/" + testK8sNamespace + "/" + toName
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.KubernetesRelationshipFactKind,
		Payload: map[string]any{
			"cluster_id":                  testK8sCluster,
			"relationship_type":           relType,
			"from_object_id":              from,
			"to_object_id":                to,
			"from_group_version_resource": "apps/v1/deployments",
			"to_group_version_resource":   "v1/pods",
			"correlation_anchors":         []string{from, to},
		},
	}
}

// k8sWarningFact builds a kubernetes_live.warning fact (a partial list or
// ambiguous selector capability gap), attached as workload correlation evidence.
func k8sWarningFact(factID, reason, resourceScope string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.KubernetesWarningFactKind,
		Payload: map[string]any{
			"cluster_id":     testK8sCluster,
			"reason":         reason,
			"resource_scope": resourceScope,
			"message":        "non-fatal",
		},
	}
}

// k8sSourceManifestFact builds an oci_registry.image_manifest fact: one
// digest-addressed deployment-source observation the live digest joins against.
func k8sSourceManifestFact(factID, registry, repository, digest string, tombstone bool) facts.Envelope {
	return facts.Envelope{
		FactID:      factID,
		FactKind:    facts.OCIImageManifestFactKind,
		IsTombstone: tombstone,
		Payload: map[string]any{
			"registry":   registry,
			"repository": repository,
			"digest":     digest,
		},
	}
}

// k8sSourceTagFact builds an oci_registry.image_tag_observation fact resolving a
// tag to a digest, the weaker repository+tag join evidence.
func k8sSourceTagFact(factID, registry, repository, tag, digest, previousDigest string, mutated bool) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.OCIImageTagObservationFactKind,
		Payload: map[string]any{
			"registry":        registry,
			"repository":      repository,
			"tag":             tag,
			"digest":          digest,
			"resolved_digest": digest,
			"previous_digest": previousDigest,
			"mutated":         mutated,
		},
	}
}

func kubernetesCorrelationByImageRef(
	decisions []KubernetesCorrelationDecision,
) map[string]KubernetesCorrelationDecision {
	out := make(map[string]KubernetesCorrelationDecision, len(decisions))
	for _, decision := range decisions {
		out[decision.WorkloadObjectID+"|"+decision.ImageRef+"|"+decision.IdentityEdgeKey] = decision
	}
	return out
}

func assertKubernetesOutcome(
	t *testing.T,
	decision KubernetesCorrelationDecision,
	wantOutcome KubernetesCorrelationOutcome,
	wantDrift string,
) {
	t.Helper()
	if decision.Outcome != wantOutcome {
		t.Fatalf("outcome = %q, want %q; reason=%s", decision.Outcome, wantOutcome, decision.Reason)
	}
	if decision.DriftKind != wantDrift {
		t.Fatalf("drift_kind = %q, want %q; reason=%s", decision.DriftKind, wantDrift, decision.Reason)
	}
}

func unmarshalKubernetesCorrelationPayload(t *testing.T, raw any) map[string]any {
	t.Helper()
	bytes, ok := raw.([]byte)
	if !ok {
		t.Fatalf("payload arg type = %T, want []byte", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	return payload
}
