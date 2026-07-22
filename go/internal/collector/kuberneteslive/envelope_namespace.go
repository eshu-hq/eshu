// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// NamespaceObservation is the input for one kubernetes_live.namespace fact.
// It carries only the namespace's labels (never annotations, reserved for
// #5444 -- see kuberneteslivev1.Namespace) so the reducer's environment-alias
// binding domain (issue #5434) has evidence to decide, per namespace,
// whether a label declares a recognized environment.
type NamespaceObservation struct {
	Identity            ObjectIdentity
	Labels              map[string]string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
	SourceURI           string
}

// NewNamespaceEnvelope builds the durable namespace fact. It redacts
// everything except metadata-only fields: the namespace's labels. It never
// carries annotations, ConfigMap-like data, or any other namespace object
// detail -- METADATA-ONLY per the package invariant
// (go/internal/collector/kuberneteslive/AGENTS.md).
func NewNamespaceEnvelope(observation NamespaceObservation) (facts.Envelope, error) {
	if err := observation.Identity.Validate(); err != nil {
		return facts.Envelope{}, fmt.Errorf("namespace identity: %w", err)
	}
	if err := validateBoundary(observation.GenerationID, observation.CollectorInstanceID, "namespace observation"); err != nil {
		return facts.Envelope{}, err
	}
	objectID := observation.Identity.ObjectID()
	anchors := []string{objectID}
	clusterID := observation.Identity.ClusterID
	// A Namespace object is cluster-scoped (ObjectIdentity.Namespace is
	// always blank for it, per Validate's allowance); the namespace's own
	// name -- the (cluster_id, namespace) identity the reducer binds
	// environment evidence against -- is ObjectIdentity.Name.
	namespaceName := observation.Identity.Name
	payload, err := factschema.EncodeKubernetesLiveNamespace(kuberneteslivev1.Namespace{
		ObjectID:           objectID,
		ClusterID:          &clusterID,
		Namespace:          &namespaceName,
		Labels:             sortedStringMap(observation.Labels),
		CorrelationAnchors: anchors,
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode kubernetes_live.namespace payload: %w", err)
	}
	payload["collector_instance_id"] = observation.CollectorInstanceID
	return newEnvelope(
		observation.Identity.ClusterID,
		facts.KubernetesNamespaceFactKind,
		facts.KubernetesNamespaceSchemaVersion,
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
