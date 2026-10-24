// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kuberneteslive parity pin for #8859 (source-parity acceptance,
// in-tree side).
//
// These tests prove the CURRENT in-tree source behavior that the #4047
// cutover gate must reproduce exactly: the same scopes, kinds, stable keys,
// fencing tokens, and in-band failure signals, whether the facts come from a
// live snapshot or from the checked-in cassette
// (testdata/8859-parity-kubernetes-live.json, cut from a live run with the
// same fixed clock). The #6710 compatibility harness reuses this pack: the
// live side is the oracle, the cassette is the recorded expectation.
//
// Out of scope here (core-owned, recorded for the external producer):
// grant revocation enforcement (see PRODUCER_GRANT_DRAFT.md) and durable
// commit/lease semantics owned by the ingestion store and workflow control
// store. The in-tree path pins the emission half; the external path must
// additionally fail closed on missing, expired, or revoked grants.
//
// Claim-driven collection is deferred for this collector (no ClaimedService
// wiring in go/cmd/collector-kubernetes-live); the claim seam is mapped, not
// pinned, in SHIM_MAP.md.
package kuberneteslive

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/access/posture"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

var parity8859ObservedAt = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// parity8859LiveSource rebuilds the exact source the cassette fixture was cut
// from: same targets, same fake evidence, same fixed clock, so scope IDs,
// generation IDs, and stable keys reproduce deterministically.
func parity8859LiveSource(t *testing.T) *Source {
	t.Helper()

	full := &fakeClient{
		namespaces: ListResult[ObjectMeta]{Items: []ObjectMeta{{
			Version: "v1", Resource: "namespaces",
			Name: "payments", UID: "uid-ns-payments",
			Labels: map[string]string{"environment": "staging"},
		}}},
		deployments: ListResult[WorkloadObject]{Items: []WorkloadObject{{
			Meta: ObjectMeta{
				APIGroup: "apps", Version: "v1", Resource: "deployments",
				Namespace: "payments", Name: "checkout", UID: "uid-deploy-full",
				Labels: map[string]string{"app": "checkout"},
			},
			ServiceAccount: "checkout-sa",
			Selector:       map[string]string{"app": "checkout"},
			Containers: []ContainerSummary{{
				Name: "app", Image: "ghcr.io/parity/checkout:1.2.3",
				Ports: []int32{8080}, EnvKeys: []string{"PORT", "DB_PASSWORD"},
			}},
		}}},
		replicasets: ListResult[WorkloadObject]{Items: []WorkloadObject{{
			Meta: ObjectMeta{
				APIGroup: "apps", Version: "v1", Resource: "replicasets",
				Namespace: "payments", Name: "checkout-rs", UID: "uid-rs-full",
				OwnerReferences: []OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "checkout", UID: "uid-deploy-full"}},
			},
			Containers: []ContainerSummary{{Name: "app", Image: "ghcr.io/parity/checkout:1.2.3"}},
		}}},
		statefulsets: ListResult[WorkloadObject]{Items: []WorkloadObject{{
			Meta: ObjectMeta{
				APIGroup: "apps", Version: "v1", Resource: "statefulsets",
				Namespace: "payments", Name: "checkout-db", UID: "uid-ss-full",
			},
			Containers: []ContainerSummary{{Name: "db", Image: "ghcr.io/parity/db:4.5.6"}},
		}}},
		pods: ListResult[WorkloadObject]{Items: []WorkloadObject{{
			Meta: ObjectMeta{
				Version: "v1", Resource: "pods",
				Namespace: "payments", Name: "checkout-pod", UID: "uid-pod-full",
				Labels:          map[string]string{"app": "checkout"},
				OwnerReferences: []OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "checkout-rs", UID: "uid-rs-full"}},
			},
			ServiceAccount: "checkout-sa",
			Containers: []ContainerSummary{{
				Name: "app", Image: "ghcr.io/parity/checkout:1.2.3",
				ResolvedImageDigest: "ghcr.io/parity/checkout@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			}},
		}}},
		services: ListResult[ServiceObject]{Items: []ServiceObject{{
			Meta: ObjectMeta{
				Version: "v1", Resource: "services",
				Namespace: "payments", Name: "checkout-svc", UID: "uid-svc-full",
			},
			Selector: map[string]string{"app": "checkout"},
		}}},
		// No ingresses: the ingress group (networking.k8s.io) is not a
		// documented safe host form, so the recorder's private-data belt
		// refuses a cassette containing it. Ingress-to-service emission
		// stays pinned by TestSourceHappyPathEmitsTypedFacts; see
		// EXTRACTION_PREP.md for the fixture-scope note.
		serviceAccounts: ListResult[ServiceAccountObject]{Items: []ServiceAccountObject{{
			Meta: ObjectMeta{
				Version: "v1", Resource: "serviceaccounts",
				Namespace: "payments", Name: "checkout-sa", UID: "uid-sa-full",
			},
			AnnotationKeys:              []string{"eks.amazonaws.com/role-arn"},
			IRSAAnnotation:              "arn:aws:iam::123456789012:role/parity-role",
			GCPServiceAccountAnnotation: "parity-sa@parity-project.iam.gserviceaccount.com",
			AutomountToken:              parity8859BoolPtr(true),
			SecretRefCount:              1,
		}}},
		// No roles, cluster roles, or bindings: the RBAC group
		// (rbac.authorization.k8s.io) is not a documented safe host form,
		// so the recorder's private-data belt refuses a cassette
		// containing it. RBAC emission stays pinned by
		// secrets_iam_source_test.go; see EXTRACTION_PREP.md.
	}

	hostile := &fakeClient{
		namespaces: ListResult[ObjectMeta]{Items: []ObjectMeta{{
			Version: "v1", Resource: "namespaces",
			Name: "default", UID: "uid-ns-default",
		}}},
		// Forbidden deployment list: the snapshot stays partial and carries
		// the in-band warning evidence instead of failing.
		deployments: ListResult[WorkloadObject]{Partial: true, Reason: WarningForbiddenResource},
		pods: ListResult[WorkloadObject]{Items: []WorkloadObject{{
			Meta: ObjectMeta{
				Version: "v1", Resource: "pods",
				Namespace: "default", Name: "orphan", UID: "uid-orphan",
				// The owner is outside the collected set: ambiguity evidence,
				// never an invented identity.
				OwnerReferences: []OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "gone", UID: "uid-gone"}},
			},
			// Env var NAMES only; the model has no field for values, so a
			// hostile name still cannot leak a value.
			Containers: []ContainerSummary{{Name: "app", Image: "ghcr.io/parity/orphan:0.0.1", EnvKeys: []string{"DB_PASSWORD"}}},
		}}},
	}

	clients := map[string]*fakeClient{
		"parity-full":    full,
		"parity-hostile": hostile,
	}
	return &Source{
		Config: Config{
			CollectorInstanceID: "k8s-parity-8859",
			Clusters: []ClusterTarget{
				{
					ClusterID:       "parity-full",
					Provider:        "eks",
					GCPWorkloadPool: "parity-project.svc.id.goog",
					Environment:     "staging",
					FencingToken:    7,
					SourceURI:       "https://api.parity-full.example.test:6443",
				},
				{
					ClusterID:    "parity-hostile",
					FencingToken: 9,
					// Credential-bearing URI: userinfo and secret query keys
					// must be sanitized before emission.
					SourceURI: "https://svc:HOSTILESECRET@api.parity-hostile.example.test/cluster?token=HOSTILETOKEN&region=us-east-1",
				},
			},
		},
		ClientFactory: ClientFactoryFunc(func(_ context.Context, target ClusterTarget) (Client, error) {
			client, ok := clients[target.ClusterID]
			if !ok {
				return nil, context.DeadlineExceeded
			}
			return client, nil
		}),
		Clock: func() time.Time { return parity8859ObservedAt },
	}
}

func parity8859BoolPtr(value bool) *bool { return &value }

func parity8859KindMultiset(envelopes []facts.Envelope) map[string]int {
	counts := map[string]int{}
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

// parity8859WantCollectorKind pins the dual-collector-kind emission of this
// source: kubernetes_live.* facts carry CollectorKind, while the
// secrets/IAM posture facts it also emits carry posture.CollectorKind
// ("secrets_iam_posture"). The extracted producer must preserve both.
func parity8859WantCollectorKind(factKind string) string {
	if strings.HasPrefix(factKind, "kubernetes_live.") {
		return CollectorKind
	}
	return posture.CollectorKind
}

func parity8859StableKeys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	slices.Sort(keys)
	return keys
}

// TestKubernetesLive8859ParityLiveAgreesWithCassette is the source-parity pin:
// the live snapshot and the checked-in cassette agree scope by scope on scope
// ID, generation ID, fact-kind multiset, stable keys, schema versions,
// collector kind, and fencing tokens. A cutover candidate must reproduce this
// same agreement against the same fixture.
func TestKubernetesLive8859ParityLiveAgreesWithCassette(t *testing.T) {
	t.Parallel()

	recorded, err := cassette.NewSource("testdata/8859-parity-kubernetes-live.json")
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	type snapshot struct {
		scopeID      string
		generationID string
		kinds        map[string]int
		keys         []string
		attrs        map[string]string
		envelopes    []facts.Envelope
	}

	// attrKey binds the host-enforced boundary attributes to the stable key
	// so the cassette pins what admission and grant checks key on.
	attrKey := func(envelope facts.Envelope) string {
		return envelope.StableFactKey + "\x00" + envelope.FactKind + "\x00" +
			envelope.SchemaVersion + "\x00" + strconv.FormatInt(envelope.FencingToken, 10)
	}
	var want []snapshot
	for {
		collected, ok, err := recorded.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next() error = %v", err)
		}
		if !ok {
			break
		}
		envelopes := drain(t, collected.Facts)
		if len(envelopes) == 0 {
			t.Fatalf("cassette scope %q has no facts", collected.Scope.ScopeID)
		}
		for _, envelope := range envelopes {
			if envelope.GenerationID != collected.Generation.GenerationID {
				t.Fatalf("cassette fact generation %q != scope generation %q", envelope.GenerationID, collected.Generation.GenerationID)
			}
		}
		attrs := map[string]string{}
		for _, envelope := range envelopes {
			attrs[envelope.StableFactKey] = attrKey(envelope)
		}
		want = append(want, snapshot{
			scopeID:      collected.Scope.ScopeID,
			generationID: collected.Generation.GenerationID,
			kinds:        parity8859KindMultiset(envelopes),
			keys:         parity8859StableKeys(envelopes),
			attrs:        attrs,
			envelopes:    envelopes,
		})
	}
	if len(want) != 2 {
		t.Fatalf("cassette scopes = %d, want 2 (parity-full, parity-hostile)", len(want))
	}

	// Index by scope ID: the canonical cassette sorts scopes by scope_id
	// while the live source yields clusters in configuration order, so the
	// comparison is order-insensitive by design.
	byScope := map[string]snapshot{}
	for _, s := range want {
		byScope[s.scopeID] = s
	}
	live := parity8859LiveSource(t)
	yielded := 0
	for i := 0; ; i++ {
		collected, ok, err := live.Next(context.Background())
		if err != nil {
			t.Fatalf("live Next()[%d] error = %v", i, err)
		}
		if !ok {
			// The false already consumed the batch-exhaust signal (the
			// counter resets on exhaust, so a further call would start a
			// new batch); the count below proves exact exhaustion.
			break
		}
		yielded++
		expected, ok := byScope[collected.Scope.ScopeID]
		if !ok {
			t.Fatalf("live Next()[%d] scope %q not in cassette", i, collected.Scope.ScopeID)
		}
		delete(byScope, collected.Scope.ScopeID)
		// The canonical cassette derives generation_id deterministically
		// from scope_id (replay.Canonicalize DerivedKeys), so it cannot
		// equal the live observedAt-derived ID by design. Both sides pin
		// the same contract instead: every envelope shares its scope's
		// generation ID. Cross-run determinism of the live ID is pinned
		// by TestKubernetesLive8859ParityDuplicateDeliveryIsStable.
		if collected.Generation.GenerationID == "" {
			t.Fatalf("live scope %q has a blank generation ID", expected.scopeID)
		}
		envelopes := drain(t, collected.Facts)
		for _, envelope := range envelopes {
			if envelope.GenerationID != collected.Generation.GenerationID {
				t.Fatalf("live fact generation %q != scope generation %q", envelope.GenerationID, collected.Generation.GenerationID)
			}
		}
		if !maps.Equal(parity8859KindMultiset(envelopes), expected.kinds) {
			t.Fatalf("live kinds = %v, want %v", parity8859KindMultiset(envelopes), expected.kinds)
		}
		if !slices.Equal(parity8859StableKeys(envelopes), expected.keys) {
			t.Fatalf("live stable keys differ from cassette for scope %q", expected.scopeID)
		}
		gotAttrs := map[string]string{}
		for _, envelope := range envelopes {
			gotAttrs[envelope.StableFactKey] = attrKey(envelope)
		}
		if !maps.Equal(gotAttrs, expected.attrs) {
			t.Fatalf("live boundary attributes differ from cassette for scope %q", expected.scopeID)
		}
		for _, envelope := range envelopes {
			if envelope.CollectorKind != parity8859WantCollectorKind(envelope.FactKind) {
				t.Fatalf("CollectorKind = %q, want %q for kind %q", envelope.CollectorKind, parity8859WantCollectorKind(envelope.FactKind), envelope.FactKind)
			}
		}
	}
	if yielded != len(want) {
		t.Fatalf("live yielded %d scopes, want %d", yielded, len(want))
	}
	if len(byScope) != 0 {
		t.Fatalf("cassette scopes never yielded live: %v", slices.Sorted(maps.Keys(byScope)))
	}

	union := map[string]bool{}
	for _, s := range want {
		for kind := range s.kinds {
			union[kind] = true
		}
	}
	for _, kind := range cloud.KubernetesLiveFactKinds() {
		if !union[kind] {
			t.Fatalf("cassette covers no %q fact; parity pack must span all 4 kubernetes_live kinds", kind)
		}
	}
	// The source also emits secrets/IAM posture facts; the pack must pin
	// the service-account, identity-use, IRSA, GCP binding, and coverage
	// evidence it carries. (RBAC role/binding facts are recorder-excluded
	// above and stay pinned by secrets_iam_source_test.go.)
	for _, kind := range []string{
		factsidentity.KubernetesServiceAccountFactKind,
		factsidentity.KubernetesWorkloadIdentityUseFactKind,
		factsidentity.EKSIRSAAnnotationFactKind,
		factsidentity.KubernetesGCPWorkloadIdentityBindingFactKind,
		factsidentity.SecretsIAMCoverageWarningFactKind,
	} {
		if !union[kind] {
			t.Fatalf("cassette covers no %q fact; parity pack must pin the posture evidence", kind)
		}
	}
}

// TestKubernetesLive8859ParityDuplicateDeliveryIsStable pins duplicate safety:
// two identical snapshots emit identical stable-key sets, so a redelivered
// generation commits idempotently at the core boundary.
func TestKubernetesLive8859ParityDuplicateDeliveryIsStable(t *testing.T) {
	t.Parallel()

	first, ok, err := parity8859LiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("first Next() = (%v, %v), want (true, nil)", ok, err)
	}
	second, ok, err := parity8859LiveSource(t).Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("second Next() = (%v, %v), want (true, nil)", ok, err)
	}
	if first.Generation.GenerationID != second.Generation.GenerationID {
		t.Fatalf("duplicate GenerationID = %q then %q; same instant must re-derive identically", first.Generation.GenerationID, second.Generation.GenerationID)
	}
	if !slices.Equal(parity8859StableKeys(drain(t, first.Facts)), parity8859StableKeys(drain(t, second.Facts))) {
		t.Fatal("duplicate snapshots emit different stable keys; redelivery would not be idempotent")
	}
}

// TestKubernetesLive8859ParityInTreeNeedsNoGrant documents the deliberate
// delegation delta: the in-tree first-party path emits with no grant lookup,
// so this asserts emission succeeds grant-free. The extracted producer must
// instead fail closed on a missing, expired, or revoked grant
// (PRODUCER_GRANT_DRAFT); the fencing token and collector kind on every
// envelope are the host-side inputs that enforcement keys on.
func TestKubernetesLive8859ParityInTreeNeedsNoGrant(t *testing.T) {
	t.Parallel()

	collected, ok, err := parity8859LiveSource(t).Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want grant-free in-tree emission")
	}
	for _, envelope := range drain(t, collected.Facts) {
		if envelope.CollectorKind != parity8859WantCollectorKind(envelope.FactKind) {
			t.Fatalf("CollectorKind = %q, want %q for kind %q", envelope.CollectorKind, parity8859WantCollectorKind(envelope.FactKind), envelope.FactKind)
		}
		if envelope.FencingToken != 7 {
			t.Fatalf("FencingToken = %d, want 7", envelope.FencingToken)
		}
	}
}

// TestKubernetesLive8859ParityHostileInputs pins the hostile half of the
// fixture directly: a forbidden list marks the generation partial and emits
// in-band warning evidence, an unresolvable owner emits ambiguity evidence
// instead of an invented identity, and a credential-bearing source URI is
// sanitized before emission.
func TestKubernetesLive8859ParityHostileInputs(t *testing.T) {
	t.Parallel()

	source := parity8859LiveSource(t)
	if _, _, err := source.Next(context.Background()); err != nil {
		t.Fatalf("full Next() error = %v", err)
	}
	hostile, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("hostile Next() error = %v", err)
	}
	if !ok {
		t.Fatal("hostile Next() ok = false, want the parity-hostile scope")
	}
	if hostile.Generation.FreshnessHint != "partial" {
		t.Fatalf("FreshnessHint = %q, want partial", hostile.Generation.FreshnessHint)
	}
	envelopes := drain(t, hostile.Facts)

	reasons := map[string]int{}
	for _, envelope := range envelopesOfKind(envelopes, cloud.KubernetesWarningFactKind) {
		reason, _ := envelope.Payload["reason"].(string)
		reasons[reason]++
	}
	if reasons[WarningForbiddenResource] == 0 {
		t.Fatalf("no %q warning; forbidden list must stay in-band, got %v", WarningForbiddenResource, reasons)
	}
	if reasons[WarningInvalidOwnerReference] == 0 {
		t.Fatalf("no %q warning; unresolvable owner must stay in-band, got %v", WarningInvalidOwnerReference, reasons)
	}

	// sanitizeURL strips userinfo and the token query key but keeps the
	// benign region key: the byte-exact sanitized form pinned below.
	const wantSanitizedURI = "https://api.parity-hostile.example.test/cluster?region=us-east-1"
	for _, envelope := range envelopes {
		if strings.HasPrefix(envelope.FactKind, "kubernetes_live.") {
			// The kubernetes_live envelope seam sanitizes userinfo and
			// secret query keys out of the operator-declared source URI.
			if envelope.SourceRef.SourceURI != wantSanitizedURI {
				t.Fatalf("SourceURI = %q, want sanitized %q", envelope.SourceRef.SourceURI, wantSanitizedURI)
			}
			if message, ok := envelope.Payload["message"].(string); ok {
				if strings.Contains(message, "HOSTILESECRET") || strings.Contains(message, "HOSTILETOKEN") {
					t.Fatalf("credential material reached warning message %q", message)
				}
			}
			continue
		}
		// KNOWN PRE-EXTRACTION GAP (pinned, not approved): the shared
		// posture envelope seam (access/posture newEnvelope) only trims the
		// source URI, so the secrets_iam_coverage_warning fact carries the
		// credential-bearing URI verbatim. The extracted producer must
		// sanitize or drop SourceURI for posture facts; fixing the shared
		// seam belongs to the secrets-IAM lane, not this PREP. See
		// EXTRACTION_PREP.md credential handling.
		if envelope.FactKind == "secrets_iam_coverage_warning" {
			if !strings.Contains(envelope.SourceRef.SourceURI, "HOSTILESECRET") {
				t.Fatalf("posture SourceURI = %q, want the verbatim (unsanitized) pre-extraction bytes", envelope.SourceRef.SourceURI)
			}
		}
	}
}
