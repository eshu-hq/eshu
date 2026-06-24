package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakeFactLoader returns a fixed envelope set for any scope/generation.
type fakeFactLoader struct{ envelopes []facts.Envelope }

func (l fakeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return l.envelopes, nil
}

// recordingGraphWriter records every write/retract call.
type recordingGraphWriter struct {
	serviceAccountNodes [][]map[string]any
	vaultAuthRoleNodes  [][]map[string]any
	vaultPolicyNodes    [][]map[string]any
	secretPathNodes     [][]map[string]any
	usesSAEdges         [][]map[string]any
	assumesIAMRoleEdges [][]map[string]any
	authVaultRoleEdges  [][]map[string]any
	usesVaultPolicyEdge [][]map[string]any
	grantsSecretEdges   [][]map[string]any
	retracts            [][]string
}

func (w *recordingGraphWriter) WriteServiceAccountNodes(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.serviceAccountNodes = append(w.serviceAccountNodes, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteVaultAuthRoleNodes(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.vaultAuthRoleNodes = append(w.vaultAuthRoleNodes, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteVaultPolicyNodes(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.vaultPolicyNodes = append(w.vaultPolicyNodes, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteSecretMetadataPathNodes(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.secretPathNodes = append(w.secretPathNodes, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteUsesServiceAccountEdges(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.usesSAEdges = append(w.usesSAEdges, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteAssumesIAMRoleEdges(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.assumesIAMRoleEdges = append(w.assumesIAMRoleEdges, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteAuthenticatesVaultRoleEdges(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.authVaultRoleEdges = append(w.authVaultRoleEdges, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteUsesVaultPolicyEdges(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.usesVaultPolicyEdge = append(w.usesVaultPolicyEdge, r)
	}
	return nil
}

func (w *recordingGraphWriter) WriteGrantsSecretReadEdges(_ context.Context, r []map[string]any) error {
	if len(r) > 0 {
		w.grantsSecretEdges = append(w.grantsSecretEdges, r)
	}
	return nil
}

func (w *recordingGraphWriter) RetractScope(_ context.Context, scopeIDs []string, _ string) error {
	w.retracts = append(w.retracts, scopeIDs)
	return nil
}

func graphProjectionIntent() Intent {
	return Intent{IntentID: "intent-1", ScopeID: "scope-1", GenerationID: "gen-1", Domain: DomainSecretsIAMGraphProjection}
}

func TestGraphProjectionHandleWritesExactRowsAndRetracts(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{
		exactChainFact(fullExactChainPayload()),
		exactPathFact(map[string]any{
			"scope_id": "scope-1", "generation_id": "gen-1", "confidence": "exact",
			"vault_policy_join_key": "sha256:pol1", "vault_mount_join_key": "sha256:mount",
			"kv_path_fingerprint": "sha256:kv", "capabilities": []string{"read"},
		}),
	}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	res, err := h.Handle(context.Background(), graphProjectionIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if res.Status != ResultStatusSucceeded || res.Domain != DomainSecretsIAMGraphProjection {
		t.Fatalf("result = %+v", res)
	}
	if len(writer.retracts) != 1 || len(writer.retracts[0]) != 1 || writer.retracts[0][0] != "scope-1" {
		t.Fatalf("retract not called once for scope-1: %v", writer.retracts)
	}
	if len(writer.serviceAccountNodes) != 1 || len(writer.vaultAuthRoleNodes) != 1 || len(writer.vaultPolicyNodes) != 1 || len(writer.secretPathNodes) != 1 {
		t.Fatalf("node writes: sa=%d vr=%d vp=%d sp=%d", len(writer.serviceAccountNodes), len(writer.vaultAuthRoleNodes), len(writer.vaultPolicyNodes), len(writer.secretPathNodes))
	}
	if len(writer.usesSAEdges) != 1 || len(writer.authVaultRoleEdges) != 1 || len(writer.usesVaultPolicyEdge) != 1 || len(writer.grantsSecretEdges) != 1 {
		t.Fatalf("edge writes: usesSA=%d auth=%d usesPol=%d grants=%d", len(writer.usesSAEdges), len(writer.authVaultRoleEdges), len(writer.usesVaultPolicyEdge), len(writer.grantsSecretEdges))
	}
	if res.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0")
	}
}

func TestGraphProjectionHandleWritesAssumesIAMRoleEdgeWhenResolvable(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	p["iam_role_cloud_resource_uid"] = "cr-uid-iam-role"
	p["iam_role_assume_mode"] = "web_identity"
	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(p)}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.assumesIAMRoleEdges) != 1 || len(writer.assumesIAMRoleEdges[0]) != 1 {
		t.Fatalf("ASSUMES_IAM_ROLE edge not written: %+v", writer.assumesIAMRoleEdges)
	}
	edge := writer.assumesIAMRoleEdges[0][0]
	if edge["service_account_uid"] != "sha256:sa" || edge["cloud_resource_uid"] != "cr-uid-iam-role" {
		t.Fatalf("edge endpoints = %+v", edge)
	}
}

func TestGraphProjectionHandleSkipsAssumesIAMRoleEdgeWithoutJoinableUID(t *testing.T) {
	t.Parallel()

	// The default exact chain carries only iam_role_fingerprint, so the IAM-role
	// endpoint is unresolved: no edge is written and the skip is counted.
	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.assumesIAMRoleEdges) != 0 {
		t.Fatalf("ASSUMES_IAM_ROLE edge written without joinable uid: %+v", writer.assumesIAMRoleEdges)
	}
}

func TestGraphProjectionHandleNonExactWritesNothingButRetracts(t *testing.T) {
	t.Parallel()

	p := fullExactChainPayload()
	p["state"] = "partial"
	loader := fakeFactLoader{envelopes: []facts.Envelope{{FactKind: secretsIAMIdentityTrustChainFactKind, Payload: p}}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}

	res, err := h.Handle(context.Background(), graphProjectionIntent())
	if err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.serviceAccountNodes) != 0 || len(writer.usesSAEdges) != 0 {
		t.Fatal("non-exact rows produced graph writes")
	}
	// Retract still runs to clear any prior generation's edges.
	if len(writer.retracts) != 1 {
		t.Fatalf("retract calls = %d, want 1", len(writer.retracts))
	}
	if res.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0", res.CanonicalWrites)
	}
}

func TestGraphProjectionHandleValidation(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{}
	writer := &recordingGraphWriter{}
	cases := map[string]struct {
		h      SecretsIAMGraphProjectionHandler
		intent Intent
	}{
		"wrong domain": {SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer}, Intent{Domain: DomainWorkloadIdentity}},
		"nil loader":   {SecretsIAMGraphProjectionHandler{Writer: writer}, graphProjectionIntent()},
		"nil writer":   {SecretsIAMGraphProjectionHandler{FactLoader: loader}, graphProjectionIntent()},
	}
	for name, tc := range cases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.h.Handle(context.Background(), tc.intent); err == nil {
				t.Fatalf("%s: error = nil, want non-nil", name)
			}
		})
	}
}

func TestGraphProjectionHandleSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{
		FactLoader: loader, Writer: writer,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v", err)
	}
	if len(writer.retracts) != 0 {
		t.Fatalf("retract ran on first generation: %v", writer.retracts)
	}
	if len(writer.serviceAccountNodes) != 1 {
		t.Fatal("nodes not written on first generation")
	}
}

func TestSecretsIAMGraphProjectionDomainDefinition(t *testing.T) {
	t.Parallel()

	def := secretsIAMGraphProjectionDomainDefinition()
	if def.Domain != DomainSecretsIAMGraphProjection {
		t.Fatalf("domain = %q", def.Domain)
	}
	if !def.Ownership.CanonicalWrite || !def.Ownership.CrossSource || !def.Ownership.CrossScope {
		t.Fatalf("ownership = %+v", def.Ownership)
	}
}
