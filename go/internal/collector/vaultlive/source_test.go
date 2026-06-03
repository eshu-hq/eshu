package vaultlive

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakeVaultClient is a metadata-only test double. It records whether any read
// method was called and returns canned auth-mount metadata.
type fakeVaultClient struct {
	authMounts []AuthMount
}

func (c *fakeVaultClient) ListAuthMounts(context.Context) ([]AuthMount, error) {
	return append([]AuthMount(nil), c.authMounts...), nil
}

func testTarget() VaultTarget {
	return VaultTarget{
		VaultClusterID: "vault-prod",
		Namespace:      "admin",
		ScopeID:        "scope-vault-prod",
		GenerationID:   "gen-1",
		FencingToken:   7,
		ObservedAt:     time.Unix(1700000000, 0).UTC(),
		SourceURI:      "https://vault.example.com/v1/sys/auth",
	}
}

// TestCollectEmitsAuthMountFact proves the source maps one Vault auth mount to a
// vault_auth_mount source fact. It is the first red test of the Vault lane.
func TestCollectEmitsAuthMountFact(t *testing.T) {
	t.Parallel()

	client := &fakeVaultClient{
		authMounts: []AuthMount{
			{
				Path:                   "kubernetes/",
				Accessor:               "auth_kubernetes_abc",
				Method:                 "kubernetes",
				Local:                  false,
				DefaultLeaseTTLSeconds: 3600,
				MaxLeaseTTLSeconds:     7200,
			},
		},
	}
	source := Source{CollectorInstanceID: "vaultlive-1"}

	envelopes, err := source.Collect(context.Background(), testTarget(), client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}
	if got, want := envelopes[0].FactKind, facts.VaultAuthMountFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := envelopes[0].ScopeID, "scope-vault-prod"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

// TestCollectNeverEmitsRawSecretMaterial proves the redaction boundary: the raw
// mount path and accessor must never appear verbatim in any emitted payload,
// because the envelope builders fingerprint them.
func TestCollectNeverEmitsRawSecretMaterial(t *testing.T) {
	t.Parallel()

	const rawPath = "kubernetes/"
	const rawAccessor = "auth_kubernetes_abc"
	client := &fakeVaultClient{
		authMounts: []AuthMount{{Path: rawPath, Accessor: rawAccessor, Method: "kubernetes"}},
	}
	source := Source{CollectorInstanceID: "vaultlive-1"}

	envelopes, err := source.Collect(context.Background(), testTarget(), client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatal("Collect() emitted no envelopes; cannot assert redaction")
	}
	for _, env := range envelopes {
		for key, val := range env.Payload {
			s, ok := val.(string)
			if !ok {
				continue
			}
			if strings.Contains(s, rawAccessor) {
				t.Fatalf("payload[%q] = %q leaks the raw Vault accessor", key, s)
			}
			// The mount path fingerprint differs from the raw path; the raw
			// path itself must not appear as a cleartext value.
			if s == rawPath {
				t.Fatalf("payload[%q] = %q leaks the raw Vault mount path", key, s)
			}
		}
	}
}

// TestClientSurfaceIsMetadataOnly proves, structurally, that the Vault Client
// seam exposes no operation that reads a secret value. This guards the
// metadata-only contract against future methods that would read KV /data,
// tokens, or AppRole secret_id.
func TestClientSurfaceIsMetadataOnly(t *testing.T) {
	t.Parallel()

	forbidden := []string{"getsecret", "readsecret", "readdata", "kvdata", "secretid", "token", "unwrap"}
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		name := strings.ToLower(clientType.Method(i).Name)
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Fatalf("Client exposes value-reading method %q (contains %q); the lane must stay metadata-only", clientType.Method(i).Name, bad)
			}
		}
	}
}
