// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceSelectorMatchEmitsRelationshipEdge is the issue #5437 positive
// case: a Service whose selector is a subset of a Pod's labels emits a
// selector_match relationship fact From=Service To=Pod.
func TestSourceSelectorMatchEmitsRelationshipEdge(t *testing.T) {
	t.Parallel()

	service := ServiceObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "services",
			Namespace: "n", Name: "checkout-svc", UID: "uid-svc",
		},
		Selector: map[string]string{"app": "checkout"},
	}
	pod := WorkloadObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "pods",
			Namespace: "n", Name: "checkout-abc12", UID: "uid-pod",
			Labels: map[string]string{"app": "checkout", "version": "v2"},
		},
	}
	client := &fakeClient{
		services: ListResult[ServiceObject]{Items: []ServiceObject{service}},
		pods:     ListResult[WorkloadObject]{Items: []WorkloadObject{pod}},
	}
	source := newSource(client)
	collected, _, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envs := drain(t, collected.Facts)

	clusterID := source.Config.Clusters[0].ClusterID
	wantFromID := identityFromMeta(clusterID, service.Meta).ObjectID()
	wantToID := identityFromMeta(clusterID, pod.Meta).ObjectID()

	found := 0
	for _, env := range envelopesOfKind(envs, facts.KubernetesRelationshipFactKind) {
		if env.Payload["relationship_type"] != string(RelationshipSelectorMatch) {
			continue
		}
		if env.Payload["from_object_id"] == wantFromID && env.Payload["to_object_id"] == wantToID {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("selector_match edges From=Service To=Pod = %d, want 1 (envs=%d relationship facts)", found, countKind(envs, facts.KubernetesRelationshipFactKind))
	}
}

// TestSourceSelectorMismatchEmitsNoRelationshipEdge is the issue #5437
// negative case: a Service selector with a key/value the Pod's labels do not
// carry must emit no selector_match edge and no fallback fact.
func TestSourceSelectorMismatchEmitsNoRelationshipEdge(t *testing.T) {
	t.Parallel()

	service := ServiceObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "services",
			Namespace: "n", Name: "checkout-svc", UID: "uid-svc",
		},
		Selector: map[string]string{"app": "checkout", "tier": "backend"},
	}
	pod := WorkloadObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "pods",
			Namespace: "n", Name: "checkout-abc12", UID: "uid-pod",
			// Missing tier=backend: the selector is not a subset of these labels.
			Labels: map[string]string{"app": "checkout"},
		},
	}
	client := &fakeClient{
		services: ListResult[ServiceObject]{Items: []ServiceObject{service}},
		pods:     ListResult[WorkloadObject]{Items: []WorkloadObject{pod}},
	}
	source := newSource(client)
	collected, _, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envs := drain(t, collected.Facts)

	for _, env := range envelopesOfKind(envs, facts.KubernetesRelationshipFactKind) {
		if env.Payload["relationship_type"] == string(RelationshipSelectorMatch) {
			t.Fatalf("unexpected selector_match edge for a selector that is not a label subset: %+v", env.Payload)
		}
	}
	if got := countKind(envs, facts.KubernetesWarningFactKind); got != 0 {
		t.Fatalf("warning facts = %d, want 0 (a selector mismatch is not an error, just absence of an edge)", got)
	}
}

// TestSourceSelectorMatchHandlesMultipleMatches is the issue #5437
// multi-match case: one Service selector matching two Pods emits two edges,
// and a Pod matching two Services emits two edges (fan-out both directions).
func TestSourceSelectorMatchHandlesMultipleMatches(t *testing.T) {
	t.Parallel()

	svcWeb := ServiceObject{
		Meta:     ObjectMeta{Version: "v1", Resource: "services", Namespace: "n", Name: "web-svc", UID: "uid-svc-web"},
		Selector: map[string]string{"app": "web"},
	}
	svcFrontend := ServiceObject{
		Meta:     ObjectMeta{Version: "v1", Resource: "services", Namespace: "n", Name: "frontend-svc", UID: "uid-svc-frontend"},
		Selector: map[string]string{"tier": "frontend"},
	}
	// pod1 matches BOTH services (app=web AND tier=frontend): proves a Pod
	// matching two Services emits two edges.
	pod1 := WorkloadObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "pods", Namespace: "n", Name: "web-1", UID: "uid-pod-1",
			Labels: map[string]string{"app": "web", "tier": "frontend"},
		},
	}
	// pod2 matches only svcWeb: proves one selector matching two Pods (with pod1).
	pod2 := WorkloadObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "pods", Namespace: "n", Name: "web-2", UID: "uid-pod-2",
			Labels: map[string]string{"app": "web"},
		},
	}
	// pod3 matches only svcFrontend: proves one selector matching two Pods (with pod1).
	pod3 := WorkloadObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "pods", Namespace: "n", Name: "frontend-3", UID: "uid-pod-3",
			Labels: map[string]string{"tier": "frontend"},
		},
	}
	client := &fakeClient{
		services: ListResult[ServiceObject]{Items: []ServiceObject{svcWeb, svcFrontend}},
		pods:     ListResult[WorkloadObject]{Items: []WorkloadObject{pod1, pod2, pod3}},
	}
	source := newSource(client)
	collected, _, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envs := drain(t, collected.Facts)

	clusterID := source.Config.Clusters[0].ClusterID
	edge := func(from, to ObjectMeta) (string, string) {
		return identityFromMeta(clusterID, from).ObjectID(), identityFromMeta(clusterID, to).ObjectID()
	}

	want := map[[2]string]bool{}
	for _, pair := range [][2]ObjectMeta{
		{svcWeb.Meta, pod1.Meta},
		{svcWeb.Meta, pod2.Meta},
		{svcFrontend.Meta, pod1.Meta},
		{svcFrontend.Meta, pod3.Meta},
	} {
		from, to := edge(pair[0], pair[1])
		want[[2]string{from, to}] = false
	}

	for _, env := range envelopesOfKind(envs, facts.KubernetesRelationshipFactKind) {
		if env.Payload["relationship_type"] != string(RelationshipSelectorMatch) {
			continue
		}
		key := [2]string{env.Payload["from_object_id"].(string), env.Payload["to_object_id"].(string)}
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected selector_match edge %+v", env.Payload)
		}
		want[key] = true
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("missing expected selector_match edge From=%s To=%s", key[0], key[1])
		}
	}
	if got := countKind(envs, facts.KubernetesRelationshipFactKind); got != len(want) {
		t.Fatalf("relationship facts = %d, want %d (exactly the four selector_match edges)", got, len(want))
	}
}
