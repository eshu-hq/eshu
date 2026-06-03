package vaultlive

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type fakeClientFactory struct {
	client Client
	err    error
	calls  int
}

func (f *fakeClientFactory) Client(context.Context, ClusterTarget) (Client, error) {
	f.calls++
	return f.client, f.err
}

func snapshotFixtureClient() *fakeVaultClient {
	return &fakeVaultClient{
		authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "acc", Method: "kubernetes"}},
	}
}

func fixedClock() func() time.Time {
	t := time.Unix(1700000000, 0).UTC()
	return func() time.Time { return t }
}

func TestSnapshotSourceYieldsGenerationPerTarget(t *testing.T) {
	t.Parallel()

	source := &SnapshotSource{
		Config: Config{
			CollectorInstanceID: "vaultlive-1",
			Targets: []ClusterTarget{
				{VaultClusterID: "vault-a", Namespace: "admin", FencingToken: 1, SourceURI: "https://a:8200"},
				{VaultClusterID: "vault-b", Namespace: "team", FencingToken: 1},
			},
		},
		ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		Clock:         fixedClock(),
	}
	ctx := context.Background()

	var scopeIDs []string
	for i := 0; i < 2; i++ {
		gen, ok, err := source.Next(ctx)
		if err != nil || !ok {
			t.Fatalf("Next[%d] ok=%v err=%v", i, ok, err)
		}
		if gen.Scope.CollectorKind != scope.CollectorVaultLive || gen.Scope.ScopeKind != scope.KindVaultCluster ||
			gen.Scope.SourceSystem != CollectorKind {
			t.Fatalf("Next[%d] scope = %+v", i, gen.Scope)
		}
		if gen.Generation.Status != scope.GenerationStatusPending || gen.Generation.TriggerKind != scope.TriggerKindSnapshot {
			t.Fatalf("Next[%d] generation = %+v", i, gen.Generation)
		}
		if gen.FactCount == 0 {
			t.Fatalf("Next[%d] emitted no facts", i)
		}
		var kinds int
		for env := range gen.Facts {
			if env.FactKind == facts.VaultAuthMountFactKind {
				kinds++
			}
		}
		if kinds != 1 {
			t.Fatalf("Next[%d] expected one vault_auth_mount fact, got %d", i, kinds)
		}
		scopeIDs = append(scopeIDs, gen.Scope.ScopeID)
	}
	if scopeIDs[0] == scopeIDs[1] {
		t.Fatalf("distinct targets shared a scope id: %q", scopeIDs[0])
	}

	// Batch drained: third Next resets and reports ok=false.
	if _, ok, err := source.Next(ctx); ok || err != nil {
		t.Fatalf("drain Next ok=%v err=%v, want false/nil", ok, err)
	}
	// And the next poll starts the batch over.
	if _, ok, err := source.Next(ctx); !ok || err != nil {
		t.Fatalf("post-drain Next ok=%v err=%v, want true/nil", ok, err)
	}
}

func TestSnapshotSourceValidatesConfig(t *testing.T) {
	t.Parallel()

	cases := map[string]*SnapshotSource{
		"missing instance id": {
			Config:        Config{Targets: []ClusterTarget{{VaultClusterID: "v"}}},
			ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		},
		"no targets": {
			Config:        Config{CollectorInstanceID: "ci"},
			ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		},
		"duplicate target": {
			Config: Config{CollectorInstanceID: "ci", Targets: []ClusterTarget{
				{VaultClusterID: "v", Namespace: "n"}, {VaultClusterID: "v", Namespace: "n"},
			}},
			ClientFactory: &fakeClientFactory{client: snapshotFixtureClient()},
		},
		"nil client factory": {
			Config: Config{CollectorInstanceID: "ci", Targets: []ClusterTarget{{VaultClusterID: "v"}}},
		},
	}
	for name, source := range cases {
		name, source := name, source
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := source.Next(context.Background()); err == nil {
				t.Fatalf("%s: Next() error = nil, want non-nil", name)
			}
		})
	}
}

func TestVaultScopeIDIsDeterministicAndNamespaceScoped(t *testing.T) {
	t.Parallel()

	a, err := VaultScopeID("vault-a", "admin")
	if err != nil {
		t.Fatalf("VaultScopeID err = %v", err)
	}
	again, _ := VaultScopeID("vault-a", "admin")
	if a != again {
		t.Fatalf("VaultScopeID not deterministic: %q vs %q", a, again)
	}
	otherNS, _ := VaultScopeID("vault-a", "team")
	if a == otherNS {
		t.Fatalf("namespace not part of scope identity: %q == %q", a, otherNS)
	}
	if _, err := VaultScopeID("  ", "admin"); err == nil {
		t.Fatal("VaultScopeID blank cluster: want error")
	}
}

func TestSnapshotSourceSurfacesClientFactoryError(t *testing.T) {
	t.Parallel()

	source := &SnapshotSource{
		Config:        Config{CollectorInstanceID: "ci", Targets: []ClusterTarget{{VaultClusterID: "v"}}},
		ClientFactory: &fakeClientFactory{err: context.DeadlineExceeded},
		Clock:         fixedClock(),
	}
	if _, _, err := source.Next(context.Background()); err == nil {
		t.Fatal("Next() error = nil, want client factory error")
	}
}
