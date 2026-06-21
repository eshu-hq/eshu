package scopedtoken

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func writeRegistryFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "scoped-tokens.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write registry file: %v", err)
	}
	return path
}

func TestLoadRegistryResolvesScopedTokenGrants(t *testing.T) {
	t.Parallel()

	path := writeRegistryFile(t, `{
      "version": 1,
      "tokens": [
        {
          "token_sha256": "`+tokenHash("team-a-secret")+`",
          "tenant_id": "team-a",
          "workspace_id": "team-a",
          "subject_class": "team_token",
          "subject_id_hash": "sha256:abc12345",
          "policy_revision_hash": "sha256:def67890",
          "allowed_scope_ids": ["git-repository-scope:acme/payments"],
          "allowed_repository_ids": ["repo://acme/payments"]
        }
      ]
    }`)

	registry, err := LoadRegistryFromFile(path)
	if err != nil {
		t.Fatalf("LoadRegistryFromFile: %v", err)
	}
	auth, ok, err := registry.ResolveScopedToken(context.Background(), "team-a-secret")
	if err != nil {
		t.Fatalf("ResolveScopedToken error = %v", err)
	}
	if !ok {
		t.Fatal("ResolveScopedToken ok = false, want true")
	}
	if auth.Mode != query.AuthModeScoped {
		t.Fatalf("Mode = %q, want %q", auth.Mode, query.AuthModeScoped)
	}
	if auth.TenantID != "team-a" || auth.WorkspaceID != "team-a" {
		t.Fatalf("tenant/workspace = %q/%q, want team-a/team-a", auth.TenantID, auth.WorkspaceID)
	}
	if auth.AllScopes {
		t.Fatal("AllScopes = true, want false for a scoped team token")
	}
	if len(auth.AllowedRepositoryIDs) != 1 || auth.AllowedRepositoryIDs[0] != "repo://acme/payments" {
		t.Fatalf("AllowedRepositoryIDs = %#v, want [repo://acme/payments]", auth.AllowedRepositoryIDs)
	}
	if len(auth.AllowedScopeIDs) != 1 || auth.AllowedScopeIDs[0] != "git-repository-scope:acme/payments" {
		t.Fatalf("AllowedScopeIDs = %#v", auth.AllowedScopeIDs)
	}
	if auth.SubjectIDHash != "sha256:abc12345" || auth.PolicyRevisionHash != "sha256:def67890" {
		t.Fatalf("subject/policy hashes = %q/%q", auth.SubjectIDHash, auth.PolicyRevisionHash)
	}
}

func TestResolveScopedTokenUnknownCredentialIsNotFound(t *testing.T) {
	t.Parallel()

	path := writeRegistryFile(t, `{"version":1,"tokens":[{"token_sha256":"`+tokenHash("known")+`","tenant_id":"t","workspace_id":"w"}]}`)
	registry, err := LoadRegistryFromFile(path)
	if err != nil {
		t.Fatalf("LoadRegistryFromFile: %v", err)
	}
	for _, cred := range []string{"unknown", "", "   "} {
		auth, ok, err := registry.ResolveScopedToken(context.Background(), cred)
		if err != nil {
			t.Fatalf("ResolveScopedToken(%q) error = %v", cred, err)
		}
		if ok {
			t.Fatalf("ResolveScopedToken(%q) ok = true, want false", cred)
		}
		if auth.Mode != "" {
			t.Fatalf("ResolveScopedToken(%q) returned auth %#v, want zero", cred, auth)
		}
	}
}

func TestResolveScopedTokenAllScopesGrantsAdminScopedToken(t *testing.T) {
	t.Parallel()

	path := writeRegistryFile(t, `{"version":1,"tokens":[{"token_sha256":"`+tokenHash("admin")+`","tenant_id":"ops","workspace_id":"ops","all_scopes":true}]}`)
	registry, err := LoadRegistryFromFile(path)
	if err != nil {
		t.Fatalf("LoadRegistryFromFile: %v", err)
	}
	auth, ok, _ := registry.ResolveScopedToken(context.Background(), "admin")
	if !ok || !auth.AllScopes {
		t.Fatalf("all-scope token auth = %#v, ok = %v, want AllScopes true", auth, ok)
	}
}

func TestLoadRegistryRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"missing hash":    `{"version":1,"tokens":[{"tenant_id":"t","workspace_id":"w"}]}`,
		"short hash":      `{"version":1,"tokens":[{"token_sha256":"abcd","tenant_id":"t","workspace_id":"w"}]}`,
		"non-hex hash":    `{"version":1,"tokens":[{"token_sha256":"` + nonHex64() + `","tenant_id":"t","workspace_id":"w"}]}`,
		"missing tenant":  `{"version":1,"tokens":[{"token_sha256":"` + tokenHash("x") + `","workspace_id":"w"}]}`,
		"duplicate hash":  `{"version":1,"tokens":[{"token_sha256":"` + tokenHash("x") + `","tenant_id":"t","workspace_id":"w"},{"token_sha256":"` + tokenHash("x") + `","tenant_id":"t2","workspace_id":"w2"}]}`,
		"unknown version": `{"version":99,"tokens":[]}`,
		"malformed json":  `{not json`,
	}
	for name, body := range cases {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeRegistryFile(t, body)
			if _, err := LoadRegistryFromFile(path); err == nil {
				t.Fatalf("LoadRegistryFromFile(%s) error = nil, want validation error", name)
			}
		})
	}
}

func TestLoadRegistryRejectsUnsafeAuditMetadata(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"raw subject hash":    `{"version":1,"tokens":[{"token_sha256":"` + tokenHash("x") + `","tenant_id":"t","workspace_id":"w","subject_id_hash":"operator@example.invalid"}]}`,
		"short subject hash":  `{"version":1,"tokens":[{"token_sha256":"` + tokenHash("x") + `","tenant_id":"t","workspace_id":"w","subject_id_hash":"abc123"}]}`,
		"raw policy revision": `{"version":1,"tokens":[{"token_sha256":"` + tokenHash("x") + `","tenant_id":"t","workspace_id":"w","policy_revision_hash":"policy-v1"}]}`,
	}
	for name, body := range cases {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeRegistryFile(t, body)
			_, err := LoadRegistryFromFile(path)
			if err == nil {
				t.Fatal("LoadRegistryFromFile() error = nil, want unsafe audit metadata rejection")
			}
			for _, forbidden := range []string{"operator@example.invalid", "abc123", "policy-v1"} {
				if containsSubstr(err.Error(), forbidden) {
					t.Fatalf("error leaked unsafe audit metadata %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestLoadRegistryMissingFileErrors(t *testing.T) {
	t.Parallel()

	if _, err := LoadRegistryFromFile(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("LoadRegistryFromFile(absent) error = nil, want error")
	}
}

func TestRegistryErrorsDoNotLeakTokenMaterial(t *testing.T) {
	t.Parallel()

	// A duplicate-hash registry must not echo the offending hash (token
	// material proxy) in its error message.
	dupHash := tokenHash("super-secret-team-token")
	path := writeRegistryFile(t, `{"version":1,"tokens":[{"token_sha256":"`+dupHash+`","tenant_id":"t","workspace_id":"w"},{"token_sha256":"`+dupHash+`","tenant_id":"t2","workspace_id":"w2"}]}`)
	_, err := LoadRegistryFromFile(path)
	if err == nil {
		t.Fatal("expected duplicate-hash error")
	}
	if got := err.Error(); containsSubstr(got, dupHash) {
		t.Fatalf("error leaked token hash material: %q", got)
	}
}

func nonHex64() string {
	return "zz" + "0123456789012345678901234567890123456789012345678901234567890"
}

func containsSubstr(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
