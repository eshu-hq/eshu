// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fakeClient is a read-only in-memory Kubernetes client for tests. Each field
// is the list result for one resource family; a nil Err returns the items.
type fakeClient struct {
	pingErr             error
	namespaces          ListResult[ObjectMeta]
	pods                ListResult[WorkloadObject]
	deployments         ListResult[WorkloadObject]
	replicasets         ListResult[WorkloadObject]
	statefulsets        ListResult[WorkloadObject]
	daemonsets          ListResult[WorkloadObject]
	jobs                ListResult[WorkloadObject]
	cronjobs            ListResult[WorkloadObject]
	services            ListResult[ServiceObject]
	ingresses           ListResult[IngressObject]
	serviceAccounts     ListResult[ServiceAccountObject]
	roles               ListResult[RBACRoleObject]
	clusterRoles        ListResult[RBACRoleObject]
	roleBindings        ListResult[RBACBindingObject]
	clusterRoleBindings ListResult[RBACBindingObject]
}

func (f *fakeClient) PingReadOnly(context.Context) error { return f.pingErr }
func (f *fakeClient) ListNamespaces(context.Context) (ListResult[ObjectMeta], error) {
	return f.namespaces, nil
}

func (f *fakeClient) ListPods(context.Context) (ListResult[WorkloadObject], error) {
	return f.pods, nil
}

func (f *fakeClient) ListDeployments(context.Context) (ListResult[WorkloadObject], error) {
	return f.deployments, nil
}

func (f *fakeClient) ListReplicaSets(context.Context) (ListResult[WorkloadObject], error) {
	return f.replicasets, nil
}

func (f *fakeClient) ListStatefulSets(context.Context) (ListResult[WorkloadObject], error) {
	return f.statefulsets, nil
}

func (f *fakeClient) ListDaemonSets(context.Context) (ListResult[WorkloadObject], error) {
	return f.daemonsets, nil
}

func (f *fakeClient) ListJobs(context.Context) (ListResult[WorkloadObject], error) {
	return f.jobs, nil
}

func (f *fakeClient) ListCronJobs(context.Context) (ListResult[WorkloadObject], error) {
	return f.cronjobs, nil
}

func (f *fakeClient) ListServices(context.Context) (ListResult[ServiceObject], error) {
	return f.services, nil
}

func (f *fakeClient) ListIngresses(context.Context) (ListResult[IngressObject], error) {
	return f.ingresses, nil
}

func (f *fakeClient) ListServiceAccounts(context.Context) (ListResult[ServiceAccountObject], error) {
	return f.serviceAccounts, nil
}

func (f *fakeClient) ListRoles(context.Context) (ListResult[RBACRoleObject], error) {
	return f.roles, nil
}

func (f *fakeClient) ListClusterRoles(context.Context) (ListResult[RBACRoleObject], error) {
	return f.clusterRoles, nil
}

func (f *fakeClient) ListRoleBindings(context.Context) (ListResult[RBACBindingObject], error) {
	return f.roleBindings, nil
}

func (f *fakeClient) ListClusterRoleBindings(context.Context) (ListResult[RBACBindingObject], error) {
	return f.clusterRoleBindings, nil
}

func factoryFor(client Client) ClientFactory {
	return ClientFactoryFunc(func(context.Context, ClusterTarget) (Client, error) {
		return client, nil
	})
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 5, 31, 9, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func drain(t *testing.T, ch <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for env := range ch {
		out = append(out, env)
	}
	return out
}

func countKind(envs []facts.Envelope, kind string) int {
	n := 0
	for _, env := range envs {
		if env.FactKind == kind {
			n++
		}
	}
	return n
}

func envelopesOfKind(envs []facts.Envelope, kind string) []facts.Envelope {
	var out []facts.Envelope
	for _, env := range envs {
		if env.FactKind == kind {
			out = append(out, env)
		}
	}
	return out
}

func newSource(client Client) *Source {
	return &Source{
		Config: Config{
			CollectorInstanceID: "k8s-prod",
			Clusters:            []ClusterTarget{{ClusterID: "prod-us-east-1", FencingToken: 3}},
		},
		ClientFactory: factoryFor(client),
		Clock:         fixedClock(),
	}
}

func TestSourceHappyPathEmitsTypedFacts(t *testing.T) {
	t.Parallel()

	deployment := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "deployments",
			Namespace: "payments", Name: "checkout", UID: "uid-deploy",
		},
		ServiceAccount: "checkout-sa",
		Selector:       map[string]string{"app": "checkout"},
		Containers:     []ContainerSummary{{Name: "app", Image: "img:1", EnvKeys: []string{"PORT"}}},
	}
	replicaset := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "replicasets",
			Namespace: "payments", Name: "checkout-rs", UID: "uid-rs",
			OwnerReferences: []OwnerReference{{Kind: "Deployment", Name: "checkout", UID: "uid-deploy"}},
		},
		Containers: []ContainerSummary{{Name: "app", Image: "img:1"}},
	}
	statefulset := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "statefulsets",
			Namespace: "payments", Name: "checkout-db", UID: "uid-ss",
		},
		Containers: []ContainerSummary{{Name: "db", Image: "img-db:1"}},
	}
	service := ServiceObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "services",
			Namespace: "payments", Name: "checkout-svc", UID: "uid-svc",
		},
	}
	ingress := IngressObject{
		Meta: ObjectMeta{
			APIGroup: "networking.k8s.io", Version: "v1", Resource: "ingresses",
			Namespace: "payments", Name: "checkout-ing", UID: "uid-ing",
		},
		BackendServices: []string{"checkout-svc"},
	}

	client := &fakeClient{
		namespaces: ListResult[ObjectMeta]{Items: []ObjectMeta{{
			Version: "v1", Resource: "namespaces", Name: "payments", UID: "uid-ns",
			Labels: map[string]string{"environment": "prod"},
		}}},
		deployments:  ListResult[WorkloadObject]{Items: []WorkloadObject{deployment}},
		replicasets:  ListResult[WorkloadObject]{Items: []WorkloadObject{replicaset}},
		statefulsets: ListResult[WorkloadObject]{Items: []WorkloadObject{statefulset}},
		services:     ListResult[ServiceObject]{Items: []ServiceObject{service}},
		ingresses:    ListResult[IngressObject]{Items: []IngressObject{ingress}},
	}

	source := newSource(client)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true")
	}
	if collected.Scope.ScopeKind != scope.KindCluster {
		t.Fatalf("ScopeKind = %q, want cluster", collected.Scope.ScopeKind)
	}
	if collected.Scope.CollectorKind != scope.CollectorKubernetesLive {
		t.Fatalf("CollectorKind = %q, want kubernetes_live", collected.Scope.CollectorKind)
	}
	if collected.Generation.FreshnessHint != "complete" {
		t.Fatalf("FreshnessHint = %q, want complete", collected.Generation.FreshnessHint)
	}

	envs := drain(t, collected.Facts)
	if got := countKind(envs, facts.KubernetesPodTemplateFactKind); got != 3 {
		t.Fatalf("pod_template facts = %d, want 3 (deployment + replicaset + statefulset)", got)
	}
	if got := countKind(envs, facts.KubernetesRelationshipFactKind); got != 2 {
		t.Fatalf("relationship facts = %d, want 2 (owner + ingress), got %d", got, got)
	}
	if got := countKind(envs, facts.KubernetesWarningFactKind); got != 0 {
		t.Fatalf("warning facts = %d, want 0", got)
	}
	nsEnvs := envelopesOfKind(envs, facts.KubernetesNamespaceFactKind)
	if len(nsEnvs) != 1 {
		t.Fatalf("namespace facts = %d, want 1", len(nsEnvs))
	}
	if got := nsEnvs[0].Payload["namespace"]; got != "payments" {
		t.Fatalf("namespace fact payload[namespace] = %#v, want %q", got, "payments")
	}
	nsLabels, ok := nsEnvs[0].Payload["labels"].(map[string]string)
	if !ok || nsLabels["environment"] != "prod" {
		t.Fatalf("namespace fact payload[labels] = %#v, want {environment: prod}", nsEnvs[0].Payload["labels"])
	}
	for _, env := range envs {
		if env.GenerationID != collected.Generation.GenerationID {
			t.Fatalf("fact generation %q != scope generation %q", env.GenerationID, collected.Generation.GenerationID)
		}
		if env.ScopeID != collected.Scope.ScopeID {
			t.Fatalf("fact scope %q != scope %q", env.ScopeID, collected.Scope.ScopeID)
		}
		if env.FencingToken != 3 {
			t.Fatalf("fact fencing token = %d, want 3", env.FencingToken)
		}
	}
}

func TestSourceForbiddenListMarksPartialAndWarns(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		namespaces: ListResult[ObjectMeta]{Items: []ObjectMeta{{Version: "v1", Resource: "namespaces", Name: "default", UID: "uid-ns"}}},
		services:   ListResult[ServiceObject]{Partial: true, Reason: WarningForbiddenResource},
	}
	source := newSource(client)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true")
	}
	if collected.Generation.FreshnessHint != "partial" {
		t.Fatalf("FreshnessHint = %q, want partial", collected.Generation.FreshnessHint)
	}
	envs := drain(t, collected.Facts)
	if got := countKind(envs, facts.KubernetesWarningFactKind); got != 1 {
		t.Fatalf("warning facts = %d, want 1", got)
	}
	for _, env := range envs {
		if env.FactKind == facts.KubernetesWarningFactKind {
			if env.Payload["reason"] != WarningForbiddenResource {
				t.Fatalf("warning reason = %v, want forbidden_resource", env.Payload["reason"])
			}
		}
	}
}

func TestSourceInvalidOwnerReferenceWarns(t *testing.T) {
	t.Parallel()

	// ReplicaSet owned by a Deployment that was never listed -> warning, no edge.
	replicaset := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "replicasets",
			Namespace: "n", Name: "orphan-rs", UID: "uid-rs",
			OwnerReferences: []OwnerReference{{Kind: "Deployment", Name: "ghost", UID: "uid-missing"}},
		},
		Containers: []ContainerSummary{{Name: "app", Image: "img:1"}},
	}
	client := &fakeClient{
		replicasets: ListResult[WorkloadObject]{Items: []WorkloadObject{replicaset}},
	}
	source := newSource(client)
	collected, _, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envs := drain(t, collected.Facts)
	if got := countKind(envs, facts.KubernetesRelationshipFactKind); got != 0 {
		t.Fatalf("relationship facts = %d, want 0 (owner not collected)", got)
	}
	if got := countKind(envs, facts.KubernetesWarningFactKind); got != 1 {
		t.Fatalf("warning facts = %d, want 1 (invalid owner reference)", got)
	}
}

// TestSourceJobOwnedByCronJobEmitsOwnerEdge is a regression test for the
// owner-edge ordering bug: collectWorkloads must index parent kinds before
// their children so addOwnerEdges can resolve the owner UID on first pass.
// A CronJob's UID must be indexed before its owned Jobs are processed, the
// same reason Deployments are listed before ReplicaSets and both before Pods.
func TestSourceJobOwnedByCronJobEmitsOwnerEdge(t *testing.T) {
	t.Parallel()

	cronjob := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "batch", Version: "v1", Resource: "cronjobs",
			Namespace: "n", Name: "nightly", UID: "uid-cronjob",
		},
	}
	job := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "batch", Version: "v1", Resource: "jobs",
			Namespace: "n", Name: "nightly-28234500", UID: "uid-job",
			OwnerReferences: []OwnerReference{{Kind: "CronJob", Name: "nightly", UID: "uid-cronjob"}},
		},
		Containers: []ContainerSummary{{Name: "app", Image: "img:1"}},
	}
	client := &fakeClient{
		jobs:     ListResult[WorkloadObject]{Items: []WorkloadObject{job}},
		cronjobs: ListResult[WorkloadObject]{Items: []WorkloadObject{cronjob}},
	}
	source := newSource(client)
	collected, _, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	envs := drain(t, collected.Facts)

	clusterID := source.Config.Clusters[0].ClusterID
	wantFromID := identityFromMeta(clusterID, job.Meta).ObjectID()
	wantToID := identityFromMeta(clusterID, cronjob.Meta).ObjectID()

	foundOwnerEdge := false
	for _, env := range envelopesOfKind(envs, facts.KubernetesRelationshipFactKind) {
		if env.Payload["relationship_type"] != string(RelationshipOwnerReference) {
			continue
		}
		if env.Payload["from_object_id"] == wantFromID && env.Payload["to_object_id"] == wantToID {
			foundOwnerEdge = true
		}
	}
	if !foundOwnerEdge {
		t.Fatalf("no owner-reference relationship fact From=Job To=CronJob found in %d relationship facts", countKind(envs, facts.KubernetesRelationshipFactKind))
	}

	for _, env := range envelopesOfKind(envs, facts.KubernetesWarningFactKind) {
		if env.Payload["reason"] == WarningInvalidOwnerReference {
			t.Fatalf("unexpected invalid_owner_reference warning for a Job whose CronJob owner was collected: %+v", env.Payload)
		}
	}
}

func TestSourceUnreachableClusterReturnsError(t *testing.T) {
	t.Parallel()

	client := &fakeClient{pingErr: errors.New("dial tcp: connection refused")}
	source := newSource(client)
	if _, _, err := source.Next(context.Background()); err == nil {
		t.Fatalf("Next() error = nil, want unreachable-cluster error")
	}
}

func TestSourceEmptyClusterCommitsEmptyGeneration(t *testing.T) {
	t.Parallel()

	source := newSource(&fakeClient{})
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true")
	}
	if collected.Generation.FreshnessHint != "complete" {
		t.Fatalf("FreshnessHint = %q, want complete", collected.Generation.FreshnessHint)
	}
	if envs := drain(t, collected.Facts); len(envs) != 0 {
		t.Fatalf("empty cluster emitted %d facts, want 0", len(envs))
	}
}

func TestSourceIdempotentFactIdentity(t *testing.T) {
	t.Parallel()

	build := func() []facts.Envelope {
		client := &fakeClient{
			deployments: ListResult[WorkloadObject]{Items: []WorkloadObject{{
				Meta:       ObjectMeta{APIGroup: "apps", Version: "v1", Resource: "deployments", Namespace: "n", Name: "d", UID: "uid-d"},
				Containers: []ContainerSummary{{Name: "app", Image: "img:1"}},
			}}},
		}
		source := newSource(client)
		collected, _, err := source.Next(context.Background())
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		return drain(t, collected.Facts)
	}
	first := build()
	second := build()
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("fact counts differ or empty: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].FactID != second[i].FactID {
			t.Fatalf("fact[%d] FactID not idempotent: %q vs %q", i, first[i].FactID, second[i].FactID)
		}
	}
}

func TestSourceDrainsAndResetsBatch(t *testing.T) {
	t.Parallel()

	source := &Source{
		Config: Config{
			CollectorInstanceID: "k8s-prod",
			Clusters: []ClusterTarget{
				{ClusterID: "a"},
				{ClusterID: "b"},
			},
		},
		ClientFactory: factoryFor(&fakeClient{}),
		Clock:         fixedClock(),
	}
	for i := 0; i < 2; i++ {
		if _, ok, err := source.Next(context.Background()); err != nil || !ok {
			t.Fatalf("Next() call %d ok=%v err=%v, want ok", i, ok, err)
		}
	}
	// Third call drains the batch.
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("Next() drain ok=%v err=%v, want ok=false", ok, err)
	}
	// Fourth call restarts the batch for the next poll.
	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("Next() restart ok=%v err=%v, want ok=true", ok, err)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Parallel()

	if _, err := (Config{}).validated(); err == nil {
		t.Fatalf("expected error for empty config")
	}
	if _, err := (Config{CollectorInstanceID: "x"}).validated(); err == nil {
		t.Fatalf("expected error for no clusters")
	}
	dup := Config{
		CollectorInstanceID: "x",
		Clusters:            []ClusterTarget{{ClusterID: "c"}, {ClusterID: "c"}},
	}
	if _, err := dup.validated(); err == nil {
		t.Fatalf("expected error for duplicate cluster_id")
	}
}
