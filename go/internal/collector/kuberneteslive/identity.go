// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ObjectIdentity is the durable identity tuple for one live Kubernetes object.
// Per the ADR, identity is (cluster_id, api_group, version, resource,
// namespace, name, uid); metadata.uid preserves historical uniqueness across
// delete-and-recreate of the same name.
type ObjectIdentity struct {
	ClusterID string
	APIGroup  string
	Version   string
	Resource  string
	Namespace string
	Name      string
	UID       string
}

// Validate checks the minimum identity fields. Namespace may be empty for
// cluster-scoped objects such as Namespace itself. UID is required because it
// is the stable identity anchor.
func (o ObjectIdentity) Validate() error {
	if strings.TrimSpace(o.ClusterID) == "" {
		return fmt.Errorf("object identity cluster_id must not be blank")
	}
	if strings.TrimSpace(o.Version) == "" {
		return fmt.Errorf("object identity version must not be blank")
	}
	if strings.TrimSpace(o.Resource) == "" {
		return fmt.Errorf("object identity resource must not be blank")
	}
	if strings.TrimSpace(o.Name) == "" {
		return fmt.Errorf("object identity name must not be blank")
	}
	if strings.TrimSpace(o.UID) == "" {
		return fmt.Errorf("object identity uid must not be blank")
	}
	return nil
}

// GroupVersionResource returns the api_group/version/resource label, using
// "core" for the empty (core) API group so the value is never blank.
func (o ObjectIdentity) GroupVersionResource() string {
	group := strings.TrimSpace(o.APIGroup)
	if group == "" {
		group = "core"
	}
	return fmt.Sprintf("%s/%s/%s", group, strings.TrimSpace(o.Version), strings.TrimSpace(o.Resource))
}

// ObjectID returns the deterministic, replay-stable identifier for this object.
// It is keyed on the durable identity tuple so the same live object always maps
// to the same ID, and a recreated object with a new UID maps to a new ID.
func (o ObjectIdentity) ObjectID() string {
	return facts.StableID("KubernetesLiveObject", map[string]any{
		"api_group": strings.TrimSpace(o.APIGroup),
		"cluster":   strings.TrimSpace(o.ClusterID),
		"name":      strings.TrimSpace(o.Name),
		"namespace": strings.TrimSpace(o.Namespace),
		"resource":  strings.TrimSpace(o.Resource),
		"uid":       strings.TrimSpace(o.UID),
		"version":   strings.TrimSpace(o.Version),
	})
}

// ClusterScopeID returns the durable ingestion scope id for a cluster target.
// It is keyed on the operator-declared ClusterID only, never the API URL.
func ClusterScopeID(clusterID string) (string, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return "", fmt.Errorf("cluster_id must not be blank")
	}
	return "kubernetes_live:" + facts.StableID("KubernetesLiveCluster", map[string]any{
		"cluster_id": clusterID,
	}), nil
}

// clusterGenerationID returns the deterministic generation id for one cluster
// snapshot. It depends only on the durable cluster id and the observation time
// so every fact in the same snapshot shares one generation id regardless of how
// partial the snapshot turned out to be. Re-listing at the same instant is
// replay-stable.
func clusterGenerationID(clusterID string, observedAt time.Time) string {
	return "kubernetes_live:" + facts.StableID("KubernetesLiveGeneration", map[string]any{
		"cluster_id": strings.TrimSpace(clusterID),
		"observed":   observedAt.UTC().Format(time.RFC3339Nano),
	})
}
