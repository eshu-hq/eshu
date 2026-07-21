// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// ContainerSummary is the redacted, metadata-only view of one container or
// init container. It carries image references and declared shape, never
// environment values, secret references resolved to values, or logs.
type ContainerSummary struct {
	Name string
	// Image is the raw image reference string as declared in the pod template.
	Image string
	// Init reports whether this is an init container.
	Init bool
	// Ports are declared container ports.
	Ports []int32
	// EnvKeys are environment variable NAMES only. Values are never collected.
	EnvKeys []string
	// EnvFromSecret reports whether the container references secret-backed env
	// without collecting any value. It records the existence of a reference for
	// drift evidence only.
	EnvFromSecret bool
	// ResolvedImageDigest is the CRI-resolved digest for this container,
	// normalized from pod.Status.ContainerStatuses[].ImageID into the bare
	// repo@sha256:<digest> form. It is empty when the pod status has not been
	// observed (e.g. for Deployments and ReplicaSets, which carry pod spec only)
	// or when the ImageID cannot be normalized to a repo@sha256:<digest> form.
	// It is metadata-only — a digest is a content fingerprint, never a secret.
	ResolvedImageDigest string
}

// PodTemplateObservation is the input for one kubernetes_live.pod_template fact.
type PodTemplateObservation struct {
	Identity       ObjectIdentity
	Containers     []ContainerSummary
	ServiceAccount string
	Selector       map[string]string
	Labels         map[string]string
	// Annotations are the workload's declared annotations, carried through to
	// the emitted fact's optional Annotations field. It exists to surface the
	// ArgoCD argocd.argoproj.io/tracking-id annotation, the declared->live
	// identity signal #5471 F2 introduces. Nil when the source object had no
	// annotations or none were observed.
	Annotations         map[string]string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
	// DesiredReplicas is the DESIRED replica count from a Deployment,
	// ReplicaSet, or StatefulSet's .Spec.Replicas, or a DaemonSet's OBSERVED
	// .Status.DesiredNumberScheduled (a DaemonSet has no replica spec; its
	// per-node scheduling count is the closest analogue). Nil for a Pod, Job,
	// or CronJob observation.
	DesiredReplicas *int32
	// ReadyReplicas is the OBSERVED ready replica count from a Deployment,
	// ReplicaSet, or StatefulSet's .Status.ReadyReplicas, or a DaemonSet's
	// .Status.NumberReady. Nil for a Pod, Job, or CronJob observation.
	ReadyReplicas *int32
	// AvailableReplicas is the OBSERVED available replica count from a
	// Deployment, ReplicaSet, or StatefulSet's .Status.AvailableReplicas, or a
	// DaemonSet's .Status.NumberAvailable. Nil for a Pod, Job, or CronJob
	// observation.
	AvailableReplicas *int32
	// PodPhase is the OBSERVED pod lifecycle phase from a Pod's
	// .Status.Phase. Nil for every other workload kind (Deployment,
	// ReplicaSet, StatefulSet, DaemonSet, Job, CronJob observation).
	PodPhase *string
}

// RelationshipObservation is the input for one kubernetes_live.relationship
// fact. From and To are durable object identities; the edge is directed
// From -> To.
type RelationshipObservation struct {
	ClusterID           string
	Type                RelationshipType
	From                ObjectIdentity
	To                  ObjectIdentity
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// WarningObservation is the input for one kubernetes_live.warning fact.
type WarningObservation struct {
	ClusterID           string
	Reason              string
	ResourceScope       string
	Message             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// NewPodTemplateEnvelope builds the durable pod-template fact. It redacts
// everything except metadata-only fields: image refs, env var names, declared
// ports, service account, and selector/label metadata.
func NewPodTemplateEnvelope(observation PodTemplateObservation) (facts.Envelope, error) {
	if err := observation.Identity.Validate(); err != nil {
		return facts.Envelope{}, fmt.Errorf("pod template identity: %w", err)
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "pod template observation"); err != nil {
		return facts.Envelope{}, err
	}
	objectID := observation.Identity.ObjectID()
	containers := make([]kuberneteslivev1.PodTemplateContainer, 0, len(observation.Containers))
	images := make([]string, 0, len(observation.Containers))
	for _, container := range observation.Containers {
		containers = append(containers, typedContainer(container))
		if image := strings.TrimSpace(container.Image); image != "" {
			images = append(images, image)
		}
	}
	anchors := []string{objectID}
	anchors = append(anchors, images...)
	clusterID := observation.Identity.ClusterID
	groupVersionResource := observation.Identity.GroupVersionResource()
	namespace := observation.Identity.Namespace
	name := observation.Identity.Name
	uid := observation.Identity.UID
	serviceAccount := strings.TrimSpace(observation.ServiceAccount)
	payload, err := factschema.EncodeKubernetesLivePodTemplate(kuberneteslivev1.PodTemplate{
		ObjectID:             objectID,
		ClusterID:            &clusterID,
		Namespace:            &namespace,
		Name:                 &name,
		WorkloadUID:          &uid,
		GroupVersionResource: &groupVersionResource,
		ServiceAccount:       &serviceAccount,
		Containers:           containers,
		ImageRefs:            images,
		Selector:             sortedStringMap(observation.Selector),
		Labels:               sortedStringMap(observation.Labels),
		Annotations:          sortedStringMap(observation.Annotations),
		CorrelationAnchors:   anchors,
		DesiredReplicas:      observation.DesiredReplicas,
		ReadyReplicas:        observation.ReadyReplicas,
		AvailableReplicas:    observation.AvailableReplicas,
		PodPhase:             observation.PodPhase,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode kubernetes_live.pod_template payload: %w", err)
	}
	payload["collector_instance_id"] = observation.CollectorInstanceID
	return newEnvelope(
		observation.Identity.ClusterID,
		facts.KubernetesPodTemplateFactKind,
		facts.KubernetesPodTemplateSchemaVersion,
		objectID,
		observation.GenerationID,
		observation.CollectorInstanceID,
		observation.FencingToken,
		observation.ObservedAt,
		observation.SourceURI,
		objectID,
		payload,
	)
}

// NewRelationshipEnvelope builds the durable relationship fact for one directed
// edge between two live objects.
func NewRelationshipEnvelope(observation RelationshipObservation) (facts.Envelope, error) {
	if strings.TrimSpace(string(observation.Type)) == "" {
		return facts.Envelope{}, fmt.Errorf("relationship type must not be blank")
	}
	if err := observation.From.Validate(); err != nil {
		return facts.Envelope{}, fmt.Errorf("relationship from identity: %w", err)
	}
	if err := observation.To.Validate(); err != nil {
		return facts.Envelope{}, fmt.Errorf("relationship to identity: %w", err)
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "relationship observation"); err != nil {
		return facts.Envelope{}, err
	}
	fromID := observation.From.ObjectID()
	toID := observation.To.ObjectID()
	stableKey := facts.StableID(facts.KubernetesRelationshipFactKind, map[string]any{
		"from": fromID,
		"to":   toID,
		"type": string(observation.Type),
	})
	clusterID := strings.TrimSpace(observation.ClusterID)
	relationshipType := string(observation.Type)
	fromGroupVersionResource := observation.From.GroupVersionResource()
	toGroupVersionResource := observation.To.GroupVersionResource()
	payload, err := factschema.EncodeKubernetesLiveRelationship(kuberneteslivev1.Relationship{
		RelationshipType:         relationshipType,
		FromObjectID:             fromID,
		ToObjectID:               toID,
		ClusterID:                &clusterID,
		FromGroupVersionResource: &fromGroupVersionResource,
		ToGroupVersionResource:   &toGroupVersionResource,
		CorrelationAnchors:       []string{fromID, toID},
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode kubernetes_live.relationship payload: %w", err)
	}
	payload["collector_instance_id"] = observation.CollectorInstanceID
	return newEnvelope(
		observation.ClusterID,
		facts.KubernetesRelationshipFactKind,
		facts.KubernetesRelationshipSchemaVersion,
		stableKey,
		observation.GenerationID,
		observation.CollectorInstanceID,
		observation.FencingToken,
		observation.ObservedAt,
		observation.SourceURI,
		fromID+"->"+toID,
		payload,
	)
}

// NewWarningEnvelope builds the durable non-fatal warning fact.
func NewWarningEnvelope(observation WarningObservation) (facts.Envelope, error) {
	reason := strings.TrimSpace(observation.Reason)
	if reason == "" {
		return facts.Envelope{}, fmt.Errorf("warning reason must not be blank")
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "warning observation"); err != nil {
		return facts.Envelope{}, err
	}
	clusterID := strings.TrimSpace(observation.ClusterID)
	if clusterID == "" {
		return facts.Envelope{}, fmt.Errorf("warning cluster_id must not be blank")
	}
	resourceScope := strings.TrimSpace(observation.ResourceScope)
	stableKey := facts.StableID(facts.KubernetesWarningFactKind, map[string]any{
		"cluster_id":     clusterID,
		"reason":         reason,
		"resource_scope": resourceScope,
	})
	message := sanitizeText(observation.Message)
	payload, err := factschema.EncodeKubernetesLiveWarning(kuberneteslivev1.Warning{
		Reason:             reason,
		ClusterID:          clusterID,
		ResourceScope:      &resourceScope,
		Message:            &message,
		CorrelationAnchors: []string{clusterID},
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode kubernetes_live.warning payload: %w", err)
	}
	payload["collector_instance_id"] = observation.CollectorInstanceID
	return newEnvelope(
		clusterID,
		facts.KubernetesWarningFactKind,
		facts.KubernetesWarningSchemaVersion,
		stableKey,
		observation.GenerationID,
		observation.CollectorInstanceID,
		observation.FencingToken,
		observation.ObservedAt,
		observation.SourceURI,
		reason+":"+resourceScope,
		payload,
	)
}

func typedContainer(container ContainerSummary) kuberneteslivev1.PodTemplateContainer {
	ports := make([]int32, 0, len(container.Ports))
	ports = append(ports, container.Ports...)
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })
	envKeys := append([]string(nil), container.EnvKeys...)
	sort.Strings(envKeys)
	name := strings.TrimSpace(container.Name)
	image := strings.TrimSpace(container.Image)
	init := container.Init
	envFromSecret := container.EnvFromSecret
	pc := kuberneteslivev1.PodTemplateContainer{
		Name:          &name,
		Image:         &image,
		Init:          &init,
		Ports:         ports,
		EnvKeys:       envKeys,
		EnvFromSecret: &envFromSecret,
	}
	if digest := strings.TrimSpace(container.ResolvedImageDigest); digest != "" {
		resolvedImageDigest := new(string)
		*resolvedImageDigest = digest
		pc.ResolvedImageDigest = resolvedImageDigest
	}
	return pc
}

func sortedStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[strings.TrimSpace(key)] = value
	}
	return output
}

func newEnvelope(
	clusterID string,
	factKind string,
	schemaVersion string,
	stableKey string,
	generationID string,
	collectorInstanceID string,
	fencingToken int64,
	observedAt time.Time,
	sourceURI string,
	sourceRecordID string,
	payload map[string]any,
) (facts.Envelope, error) {
	scopeID, err := ClusterScopeID(clusterID)
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID:           factID(factKind, stableKey, scopeID, generationID),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     fencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(observedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        stableKey,
			SourceURI:      sanitizeURL(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}, nil
}

func factID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("KubernetesLiveFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func validateBoundary(generationID, collectorInstanceID, noun string) error {
	if strings.TrimSpace(generationID) == "" {
		return fmt.Errorf("%s generation_id must not be blank", noun)
	}
	if strings.TrimSpace(collectorInstanceID) == "" {
		return fmt.Errorf("%s collector_instance_id must not be blank", noun)
	}
	return nil
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func sanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if isSensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sanitizeText(input string) string {
	return sensitiveURLPattern.ReplaceAllStringFunc(input, sanitizeURL)
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}

// NormalizeCRIImageID normalizes a CRI container runtime ImageID string into the
// bare repo@sha256:<digest> form. It strips any container runtime scheme prefix
// (e.g. docker-pullable://, docker://, cri-o://) and returns only values that
// parse to a repository@sha256:digest form. A bare sha256: digest with no
// repository, a tag reference, or an unparseable string returns "".
//
// Kubernetes publishes the CRI-resolved digest at
// pod.Status.ContainerStatuses[].ImageID (and InitContainerStatuses) for every
// container, even for tag-referenced images, in forms like
// docker-pullable://repo@sha256:... or bare repo@sha256:... .
//
// The normalization mirrors the reducer's parseContainerImageRef semantics so
// the resolved digest is joinable against the source digest index: the scheme
// prefix is stripped, then the remainder is checked for the @sha256:<hex>
// pattern; if the part before the @ contains no repository (bare sha256:), the
// value is unjoinable and returns "".
func NormalizeCRIImageID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// Strip any scheme:// prefix (docker-pullable://, docker://, cri-o://, etc.).
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		trimmed = trimmed[idx+3:]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	// Only accept repo@sha256:<digest> forms. Mirror parseContainerImageRef:
	// split on "@" and require the digest part to start with "sha256:".
	before, digest, ok := strings.Cut(trimmed, "@")
	if !ok || !strings.HasPrefix(digest, "sha256:") {
		return ""
	}
	// The part before "@" must be non-empty (a bare sha256: has no repository
	// and is not joinable).
	repo := strings.Trim(strings.TrimSpace(before), "/")
	if repo == "" {
		return ""
	}
	return repo + "@" + digest
}
