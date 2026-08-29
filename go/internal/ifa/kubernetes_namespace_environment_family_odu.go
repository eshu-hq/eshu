// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// The kubernetes_namespace_environment family Odù (#6228, under the #6181
// direct-materialization umbrella).
//
// This is a DIRECT-materialization family: the reducer writes it straight to
// cypher.KubernetesNamespaceNodeWriter through the WriteKubernetesNamespaceNodes
// port, with no shared-projection intent row in between. The port's name says
// "Nodes" and the Cypher it executes MERGEs a TARGETS_ENVIRONMENT
// RELATIONSHIP; that mismatch is one of the six #6181 names, and the whole
// point of deriving this fixture from
// canonicalKubernetesNamespaceWithEnvironmentUpsertCypher rather than from the
// port name.
//
// Facts are built as typed kuberneteslivev1.Namespace values and encoded
// through factschema.EncodeKubernetesLiveNamespace, never as hand-built maps,
// so a payload-contract change breaks this fixture at compile or encode time
// instead of turning it silently inert (the Contract System v1 rule; a
// mismatched payload shape decodes to a quarantined fact and produces zero
// rows, which reads exactly like a writer regression).

const (
	// KubernetesNamespaceEnvironmentFamilyOduName is this Odù's catalog name,
	// the ref a materialized_edges:kubernetes_namespace_environment coverage
	// row would name to resolve through it. Exported so materializededges'
	// family tests can name and look it up by that ref without duplicating
	// the literal.
	KubernetesNamespaceEnvironmentFamilyOduName = "odu:ifa-kubernetes-namespace-environment-family"

	// kubernetesNamespaceEnvironmentFamilyScopeID is the single cluster scope
	// every fact in this Odù belongs to. The reducer handler loads one scope
	// generation's namespace facts, so a fixture spanning scopes would not
	// mirror any real intent.
	kubernetesNamespaceEnvironmentFamilyScopeID = "kubernetes_live:eshu-fixture-cluster"

	// kubernetesNamespaceEnvironmentFamilyClusterID is the operator-declared
	// cluster identity stamped on every namespace below.
	kubernetesNamespaceEnvironmentFamilyClusterID = "eshu-fixture-cluster"

	// kubernetesNamespaceEnvironmentFamilyGenerationID is the one scope
	// generation the committed cassette replays. The reducer's handler loads a
	// single scope generation's namespace facts, so every fact below shares it.
	kubernetesNamespaceEnvironmentFamilyGenerationID = "gen-ifa-kubernetes-namespace-environment-family-1"

	// kubernetesNamespaceEnvironmentFamilyCollectorKind and
	// kubernetesNamespaceEnvironmentFamilySchemaVersion mirror what the
	// kubernetes_live collector stamps on a namespace fact, so the compiled Odù
	// and the committed cassette describe the same envelope rather than agreeing
	// only on the payload.
	kubernetesNamespaceEnvironmentFamilyCollectorKind = "kubernetes_live"
	kubernetesNamespaceEnvironmentFamilySchemaVersion = "1.0.0"

	// kubernetesNamespaceEnvironmentFamilySourceConfidence marks these facts as
	// directly observed, the posture a live cluster read carries.
	kubernetesNamespaceEnvironmentFamilySourceConfidence = "observed"
)

// kubernetesNamespaceEnvironmentFamilyStableFactKey derives one fact's durable
// dedup key from the identity the collector keys it by, so the cassette and the
// compiled Odù cannot drift apart on two independently hand-typed strings.
func kubernetesNamespaceEnvironmentFamilyStableFactKey(namespace string) string {
	return fmt.Sprintf(
		"kubernetes_live:%s:namespace::%s",
		kubernetesNamespaceEnvironmentFamilyClusterID, namespace,
	)
}

// kubernetesNamespaceEnvironmentFixture describes one namespace fact in the
// Odù, and — separately — whether it is expected to produce a
// TARGETS_ENVIRONMENT edge.
//
// The two are held apart on purpose. The reducer's extractor emits a node row
// for EVERY namespace it decodes; only a row whose environment label resolved
// to a KNOWN token carries a non-empty environment, and only such a row is
// routed to the write template that MERGEs the relationship. A fixture that
// conflated "produced a row" with "produced an edge" would call the two
// deliberately-unbound namespaces below a regression.
type kubernetesNamespaceEnvironmentFixture struct {
	// ObjectID is the collector-emitted stable identity, which is also the
	// KubernetesNamespace node uid and therefore the edge's source identity.
	ObjectID string
	// Namespace is the namespace object's own metadata.name.
	Namespace string
	// Labels are the only evidence surface for environment binding (#5434).
	Labels map[string]string
}

// kubernetesNamespaceEnvironmentFamilyFixtures is the hand-authored namespace
// set this Odù carries. Two bind and two deliberately do not.
//
// The two unbound namespaces are the load-bearing half. namespaceEnvironmentFromLabels
// gates on environment.IsKnownToken, not on "the label is present and
// non-empty", precisely so an unrecognized team name can never become an
// Environment node — and the writer's unbound template MERGEs no relationship
// at all. Without a negative control here, a regression that dropped the
// IsKnownToken check would still reproduce the expected edge set exactly and
// this fixture would report green while the graph gained an invented
// environment.
var kubernetesNamespaceEnvironmentFamilyFixtures = []kubernetesNamespaceEnvironmentFixture{
	{
		// BOUND via the plain "environment" key, through the production alias
		// (environment.Canonical maps "production" -> "prod"), so the expected
		// target is the canonical name and not the raw label value.
		ObjectID:  "k8s-ns:eshu-fixture-cluster:payments-prod",
		Namespace: "payments-prod",
		Labels:    map[string]string{"environment": "production"},
	},
	{
		// BOUND via the Kubernetes-recommended common label. Both keys in
		// namespaceEnvironmentLabelKeys must be exercised or a regression that
		// dropped the second key would leave this fixture green.
		// "staging" -> "stage" is the second alias.
		ObjectID:  "k8s-ns:eshu-fixture-cluster:payments-staging",
		Namespace: "payments-staging",
		Labels:    map[string]string{"app.kubernetes.io/environment": "staging"},
	},
	{
		// UNBOUND: the label is present and non-empty but "platform-team" is
		// not a known environment token, so no Environment node and no edge.
		ObjectID:  "k8s-ns:eshu-fixture-cluster:platform-team",
		Namespace: "platform-team",
		Labels:    map[string]string{"environment": "platform-team"},
	},
	{
		// UNBOUND: no environment label at all, the ordinary majority case.
		ObjectID:  "k8s-ns:eshu-fixture-cluster:kube-system",
		Namespace: "kube-system",
		Labels:    map[string]string{"kubernetes.io/metadata.name": "kube-system"},
	},
}

// KubernetesNamespaceEnvironmentFamilyOdu builds the cataloged Odù for the
// kubernetes_namespace_environment direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name; the builder itself must
// stay in this package so the seed does not import materializededges (the
// production cycle #6053 records).
//
// It panics on an encode failure rather than returning an error: this is
// registration-time construction of a committed fixture from typed values, so
// a failure here means the payload contract changed under the fixture and
// every downstream coverage claim built on it is already void.
func KubernetesNamespaceEnvironmentFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, len(kubernetesNamespaceEnvironmentFamilyFixtures))
	clusterID := kubernetesNamespaceEnvironmentFamilyClusterID
	for _, fixture := range kubernetesNamespaceEnvironmentFamilyFixtures {
		namespaceName := fixture.Namespace
		payload, err := factschema.EncodeKubernetesLiveNamespace(kuberneteslivev1.Namespace{
			ObjectID:  fixture.ObjectID,
			ClusterID: &clusterID,
			Namespace: &namespaceName,
			Labels:    fixture.Labels,
		})
		if err != nil {
			panic(fmt.Sprintf(
				"ifa: catalog_seed %s: encode kubernetes_live.namespace payload for %q: %v",
				KubernetesNamespaceEnvironmentFamilyOduName, fixture.ObjectID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          kubernetesNamespaceEnvironmentFamilyScopeID,
			GenerationID:     kubernetesNamespaceEnvironmentFamilyGenerationID,
			FactKind:         facts.KubernetesNamespaceFactKind,
			StableFactKey:    kubernetesNamespaceEnvironmentFamilyStableFactKey(fixture.Namespace),
			SchemaVersion:    kubernetesNamespaceEnvironmentFamilySchemaVersion,
			CollectorKind:    kubernetesNamespaceEnvironmentFamilyCollectorKind,
			SourceConfidence: kubernetesNamespaceEnvironmentFamilySourceConfidence,
			Payload:          payload,
		})
	}

	return CatalogOdu{
		Odu:    Odu{Name: KubernetesNamespaceEnvironmentFamilyOduName, Facts: factsForOdu},
		Detail: "four kubernetes_live.namespace facts for the direct-materialization kubernetes_namespace_environment family: two alias-bound (one per checked label key) and two deliberately unbound, so the TARGETS_ENVIRONMENT expected set proves both the binding and the no-invented-environment rule",
	}
}
