// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// generationBuilder accumulates the envelopes for one cluster snapshot. It maps
// listed metadata-only objects into Kubernetes live and Kubernetes
// secrets/IAM source facts, marks the generation partial when any resource
// family list was incomplete, and records owner-reference and ingress-to-service
// relationships.
type generationBuilder struct {
	source              *Source
	target              ClusterTarget
	collectorInstanceID string
	observedAt          time.Time

	envelopes []facts.Envelope
	partial   bool
	// uidIndex maps object UID -> identity so owner references can resolve to a
	// concrete in-cluster identity. Missing references emit a warning.
	uidIndex map[string]ObjectIdentity
	// serviceIndex maps namespace/name -> service identity for ingress edges.
	serviceIndex map[string]ObjectIdentity
	// serviceAccountIndex maps namespace/name -> ServiceAccount metadata so
	// workload identity-use facts can include UID posture when available.
	serviceAccountIndex map[string]ServiceAccountObject
	// serviceSelectorIndex retains each Service's identity and label selector
	// so the selector-match pass (run after collectWorkloads) can find which
	// Pods it actually routes to. A Service with an empty selector is excluded:
	// Kubernetes never treats an empty selector as "match every Pod", so
	// indexing it would only add unproductive comparisons.
	serviceSelectorIndex []serviceSelectorEntry
	// podLabelIndex retains each Pod's identity and labels for the
	// selector-match pass. Only Pods are indexed here — a Service selector
	// targets Pods, never Deployments, ReplicaSets, or other workload kinds.
	podLabelIndex []podLabelEntry
}

// serviceSelectorEntry is one Service's identity and non-empty label selector,
// captured by collectServices for the selector-match pass.
type serviceSelectorEntry struct {
	identity ObjectIdentity
	selector map[string]string
}

// podLabelEntry is one Pod's identity and labels, captured by collectWorkloads
// for the selector-match pass.
type podLabelEntry struct {
	identity ObjectIdentity
	labels   map[string]string
}

func (b *generationBuilder) run(ctx context.Context, client Client) error {
	b.uidIndex = make(map[string]ObjectIdentity)
	b.serviceIndex = make(map[string]ObjectIdentity)
	b.serviceAccountIndex = make(map[string]ServiceAccountObject)

	if err := b.collectNamespaces(ctx, client); err != nil {
		return err
	}
	// Services first so ingress backends can resolve service identities.
	services, err := b.collectServices(ctx, client)
	if err != nil {
		return err
	}
	if err := b.collectServiceAccounts(ctx, client); err != nil {
		return err
	}
	if err := b.collectRBAC(ctx, client); err != nil {
		return err
	}
	if err := b.collectWorkloads(ctx, client); err != nil {
		return err
	}
	if err := b.matchSelectors(ctx); err != nil {
		return err
	}
	if err := b.collectIngresses(ctx, client, services); err != nil {
		return err
	}
	return nil
}

func (b *generationBuilder) collectNamespaces(ctx context.Context, client Client) error {
	result, err := client.ListNamespaces(ctx)
	if err != nil {
		return err
	}
	b.markPartial(ctx, ResourceScopeNamespaces, result.Partial, result.Reason)
	b.source.recordResourcesListed(ctx, ResourceScopeNamespaces, len(result.Items), result.Partial)
	for _, meta := range result.Items {
		identity := b.indexObject(meta)
		envelope, err := NewNamespaceEnvelope(NamespaceObservation{
			Identity:            identity,
			Labels:              meta.Labels,
			GenerationID:        b.generationID(),
			CollectorInstanceID: b.collectorInstanceID,
			FencingToken:        b.target.FencingToken,
			ObservedAt:          b.observedAt,
			SourceURI:           b.target.SourceURI,
		})
		if err != nil {
			return err
		}
		b.append(ctx, envelope)
	}
	return nil
}

func (b *generationBuilder) collectServices(ctx context.Context, client Client) ([]ServiceObject, error) {
	result, err := client.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	b.markPartial(ctx, ResourceScopeServices, result.Partial, result.Reason)
	b.source.recordResourcesListed(ctx, ResourceScopeServices, len(result.Items), result.Partial)
	for _, service := range result.Items {
		identity := b.indexObject(service.Meta)
		b.serviceIndex[namespacedName(identity.Namespace, identity.Name)] = identity
		if len(service.Selector) > 0 {
			b.serviceSelectorIndex = append(b.serviceSelectorIndex, serviceSelectorEntry{
				identity: identity,
				selector: service.Selector,
			})
		}
	}
	return result.Items, nil
}

func (b *generationBuilder) collectWorkloads(ctx context.Context, client Client) error {
	// Parents must be indexed before children: addOwnerEdges resolves an
	// owner's UID against b.uidIndex immediately, so an owned kind listed
	// before its owner would find an unindexed UID and drop the edge with a
	// WarningInvalidOwnerReference instead of emitting it. Deployment before
	// ReplicaSet before Pod, and CronJob before Job before Pod.
	lists := []struct {
		resourceScope string
		list          func(context.Context) (ListResult[WorkloadObject], error)
	}{
		{ResourceScopeDeployments, client.ListDeployments},
		{ResourceScopeReplicaSets, client.ListReplicaSets},
		{ResourceScopeStatefulSets, client.ListStatefulSets},
		{ResourceScopeDaemonSets, client.ListDaemonSets},
		{ResourceScopeCronJobs, client.ListCronJobs},
		{ResourceScopeJobs, client.ListJobs},
		{ResourceScopePods, client.ListPods},
	}
	for _, entry := range lists {
		result, err := entry.list(ctx)
		if err != nil {
			return err
		}
		b.markPartial(ctx, entry.resourceScope, result.Partial, result.Reason)
		b.source.recordResourcesListed(ctx, entry.resourceScope, len(result.Items), result.Partial)
		for _, workload := range result.Items {
			identity, err := b.addWorkload(ctx, workload)
			if err != nil {
				return err
			}
			if entry.resourceScope == ResourceScopePods {
				b.podLabelIndex = append(b.podLabelIndex, podLabelEntry{
					identity: identity,
					labels:   workload.Meta.Labels,
				})
			}
		}
	}
	return nil
}

func (b *generationBuilder) addWorkload(ctx context.Context, workload WorkloadObject) (ObjectIdentity, error) {
	identity := b.indexObject(workload.Meta)
	envelope, err := NewPodTemplateEnvelope(PodTemplateObservation{
		Identity:            identity,
		Containers:          workload.Containers,
		ServiceAccount:      workload.ServiceAccount,
		Selector:            workload.Selector,
		Labels:              workload.Meta.Labels,
		Annotations:         workload.Meta.Annotations,
		GenerationID:        b.generationID(),
		CollectorInstanceID: b.collectorInstanceID,
		FencingToken:        b.target.FencingToken,
		ObservedAt:          b.observedAt,
		SourceURI:           b.target.SourceURI,
		DesiredReplicas:     workload.DesiredReplicas,
		ReadyReplicas:       workload.ReadyReplicas,
		AvailableReplicas:   workload.AvailableReplicas,
		PodPhase:            workload.PodPhase,
	})
	if err != nil {
		return ObjectIdentity{}, err
	}
	b.append(ctx, envelope)
	if err := b.addWorkloadIdentityUse(ctx, identity, workload); err != nil {
		return ObjectIdentity{}, err
	}
	if err := b.addOwnerEdges(ctx, identity, workload.Meta.OwnerReferences); err != nil {
		return ObjectIdentity{}, err
	}
	return identity, nil
}

func (b *generationBuilder) addOwnerEdges(ctx context.Context, owned ObjectIdentity, owners []OwnerReference) error {
	for _, owner := range owners {
		ownerIdentity, ok := b.uidIndex[strings.TrimSpace(owner.UID)]
		if !ok {
			// The owner was not in the collected set (for example a controller
			// outside the core resource families). Record ambiguity evidence
			// rather than inventing an identity.
			if err := b.emitWarning(ctx, WarningInvalidOwnerReference, b.ownerScope(owner)); err != nil {
				return err
			}
			continue
		}
		envelope, err := NewRelationshipEnvelope(RelationshipObservation{
			ClusterID:           b.target.ClusterID,
			Type:                RelationshipOwnerReference,
			From:                owned,
			To:                  ownerIdentity,
			GenerationID:        b.generationID(),
			CollectorInstanceID: b.collectorInstanceID,
			FencingToken:        b.target.FencingToken,
			ObservedAt:          b.observedAt,
			SourceURI:           b.target.SourceURI,
		})
		if err != nil {
			return err
		}
		b.append(ctx, envelope)
	}
	return nil
}

func (b *generationBuilder) collectIngresses(ctx context.Context, client Client, _ []ServiceObject) error {
	result, err := client.ListIngresses(ctx)
	if err != nil {
		return err
	}
	b.markPartial(ctx, ResourceScopeIngresses, result.Partial, result.Reason)
	b.source.recordResourcesListed(ctx, ResourceScopeIngresses, len(result.Items), result.Partial)
	for _, ingress := range result.Items {
		identity := b.indexObject(ingress.Meta)
		for _, serviceName := range ingress.BackendServices {
			key := namespacedName(identity.Namespace, serviceName)
			serviceIdentity, ok := b.serviceIndex[key]
			if !ok {
				if err := b.emitWarning(ctx, WarningSelectorAmbiguous, ResourceScopeIngresses); err != nil {
					return err
				}
				continue
			}
			envelope, err := NewRelationshipEnvelope(RelationshipObservation{
				ClusterID:           b.target.ClusterID,
				Type:                RelationshipIngressToService,
				From:                identity,
				To:                  serviceIdentity,
				GenerationID:        b.generationID(),
				CollectorInstanceID: b.collectorInstanceID,
				FencingToken:        b.target.FencingToken,
				ObservedAt:          b.observedAt,
				SourceURI:           b.target.SourceURI,
			})
			if err != nil {
				return err
			}
			b.append(ctx, envelope)
		}
	}
	return nil
}

func (b *generationBuilder) indexObject(meta ObjectMeta) ObjectIdentity {
	identity := identityFromMeta(b.target.ClusterID, meta)
	if uid := strings.TrimSpace(identity.UID); uid != "" {
		b.uidIndex[uid] = identity
	}
	return identity
}

func (b *generationBuilder) markPartial(ctx context.Context, resourceScope string, partial bool, reason string) {
	if !partial {
		return
	}
	b.partial = true
	if strings.TrimSpace(reason) == "" {
		reason = WarningPartialList
	}
	_ = b.emitWarning(ctx, reason, resourceScope)
	_ = b.emitSecretsCoverageWarning(ctx, reason, resourceScope)
}

func (b *generationBuilder) emitWarning(ctx context.Context, reason, resourceScope string) error {
	envelope, err := NewWarningEnvelope(WarningObservation{
		ClusterID:           b.target.ClusterID,
		Reason:              reason,
		ResourceScope:       resourceScope,
		GenerationID:        b.generationID(),
		CollectorInstanceID: b.collectorInstanceID,
		FencingToken:        b.target.FencingToken,
		ObservedAt:          b.observedAt,
		SourceURI:           b.target.SourceURI,
	})
	if err != nil {
		return err
	}
	b.append(ctx, envelope)
	b.source.recordWarning(ctx, reason)
	return nil
}

func (b *generationBuilder) append(ctx context.Context, envelope facts.Envelope) {
	b.envelopes = append(b.envelopes, envelope)
	b.source.recordFactEmitted(ctx, envelope.FactKind)
}

func (b *generationBuilder) generationID() string {
	return clusterGenerationID(b.target.ClusterID, b.observedAt)
}

func (b *generationBuilder) ownerScope(owner OwnerReference) string {
	kind := strings.TrimSpace(strings.ToLower(owner.Kind))
	switch kind {
	case "deployment":
		return ResourceScopeDeployments
	case "replicaset":
		return ResourceScopeReplicaSets
	case "statefulset":
		return ResourceScopeStatefulSets
	case "daemonset":
		return ResourceScopeDaemonSets
	case "job":
		return ResourceScopeJobs
	case "cronjob":
		return ResourceScopeCronJobs
	default:
		return kind
	}
}

func identityFromMeta(clusterID string, meta ObjectMeta) ObjectIdentity {
	return ObjectIdentity{
		ClusterID: clusterID,
		APIGroup:  strings.TrimSpace(meta.APIGroup),
		Version:   strings.TrimSpace(meta.Version),
		Resource:  strings.TrimSpace(meta.Resource),
		Namespace: strings.TrimSpace(meta.Namespace),
		Name:      strings.TrimSpace(meta.Name),
		UID:       strings.TrimSpace(meta.UID),
	}
}

func namespacedName(namespace, name string) string {
	return strings.TrimSpace(namespace) + "/" + strings.TrimSpace(name)
}
