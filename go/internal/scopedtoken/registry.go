// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// registryVersion is the only supported scoped-token registry schema version.
const registryVersion = 1

// sha256HexLen is the hex length of a SHA-256 digest (32 bytes).
const sha256HexLen = 64

// Entry is one operator-managed scoped-token grant in the registry file.
//
// The registry stores only the SHA-256 hash of the bearer token, never the
// token itself, so a leaked registry file cannot be replayed: the API server
// hashes the presented credential and looks it up. Grants name the tenant,
// workspace, and the repository/ingestion-scope ids the token may read.
type Entry struct {
	// TokenSHA256 is the lowercase hex SHA-256 of the bearer token.
	TokenSHA256 string `json:"token_sha256"`
	// TenantID and WorkspaceID identify the isolated tenant the token reads as.
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
	// SubjectClass and SubjectIDHash are low-cardinality, non-identifying
	// labels carried into governance audit. SubjectIDHash must be a sha256:
	// hash when set and must never hold a raw identity.
	SubjectClass  string `json:"subject_class,omitempty"`
	SubjectIDHash string `json:"subject_id_hash,omitempty"`
	// PolicyRevisionHash pins the grant to a sha256: policy revision for audit.
	PolicyRevisionHash string `json:"policy_revision_hash,omitempty"`
	// AllScopes marks an admin-equivalent scoped token that reads every scope.
	// When false the token may read only AllowedScopeIDs/AllowedRepositoryIDs.
	AllScopes bool `json:"all_scopes,omitempty"`
	// AllowedScopeIDs and AllowedRepositoryIDs are the granted ingestion-scope
	// and repository ids. Empty grants (with AllScopes false) authorize no
	// repositories and produce bounded empty reads.
	AllowedScopeIDs      []string `json:"allowed_scope_ids,omitempty"`
	AllowedRepositoryIDs []string `json:"allowed_repository_ids,omitempty"`
}

// registryFile is the on-disk registry document.
type registryFile struct {
	Version int     `json:"version"`
	Tokens  []Entry `json:"tokens"`
}

// Registry resolves a presented bearer credential into an authorization context
// using an operator-managed, hashed-token grant table. It is read-only after
// construction and safe for concurrent use.
type Registry struct {
	byHash map[string]query.AuthContext
}

// LoadRegistryFromFile reads and validates a scoped-token registry file. It
// fails closed: any malformed entry, duplicate hash, or unsupported version
// returns an error so a deployment never silently runs with a partial or
// ambiguous grant table. Errors never include token-hash material.
func LoadRegistryFromFile(path string) (*Registry, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- reads an operator-managed hashed-token grant table at a path supplied by deployment config, not an HTTP/MCP request param
	if err != nil {
		return nil, fmt.Errorf("read scoped token registry: %w", err)
	}
	var doc registryFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode scoped token registry: %w", err)
	}
	if doc.Version != registryVersion {
		return nil, fmt.Errorf("scoped token registry version %d is unsupported; expected %d", doc.Version, registryVersion)
	}
	byHash := make(map[string]query.AuthContext, len(doc.Tokens))
	for i, entry := range doc.Tokens {
		hash, auth, err := entry.normalize()
		if err != nil {
			return nil, fmt.Errorf("scoped token registry entry %d: %w", i, err)
		}
		if _, exists := byHash[hash]; exists {
			return nil, fmt.Errorf("scoped token registry entry %d: duplicate token hash", i)
		}
		byHash[hash] = auth
	}
	return &Registry{byHash: byHash}, nil
}

// normalize validates one entry and returns its lookup hash and auth context.
// It never returns the hash inside an error so token material stays private.
func (e Entry) normalize() (string, query.AuthContext, error) {
	hash := strings.ToLower(strings.TrimSpace(e.TokenSHA256))
	if len(hash) != sha256HexLen {
		return "", query.AuthContext{}, fmt.Errorf("token_sha256 must be %d hex characters", sha256HexLen)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", query.AuthContext{}, fmt.Errorf("token_sha256 must be hex-encoded")
	}
	tenant := strings.TrimSpace(e.TenantID)
	workspace := strings.TrimSpace(e.WorkspaceID)
	if tenant == "" || workspace == "" {
		return "", query.AuthContext{}, fmt.Errorf("tenant_id and workspace_id are required")
	}
	auth := query.AuthContext{
		Mode:                 query.AuthModeScoped,
		TenantID:             tenant,
		WorkspaceID:          workspace,
		SubjectClass:         strings.TrimSpace(e.SubjectClass),
		SubjectIDHash:        strings.TrimSpace(e.SubjectIDHash),
		PolicyRevisionHash:   strings.TrimSpace(e.PolicyRevisionHash),
		AllScopes:            e.AllScopes,
		AllowedScopeIDs:      append([]string(nil), e.AllowedScopeIDs...),
		AllowedRepositoryIDs: append([]string(nil), e.AllowedRepositoryIDs...),
	}
	if !validOptionalAuditHash(auth.SubjectIDHash) {
		return "", query.AuthContext{}, fmt.Errorf("subject_id_hash must be a sha256 hash when set")
	}
	if !validOptionalAuditHash(auth.PolicyRevisionHash) {
		return "", query.AuthContext{}, fmt.Errorf("policy_revision_hash must be a sha256 hash when set")
	}
	return hash, auth, nil
}

func validOptionalAuditHash(value string) bool {
	if value == "" {
		return true
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	hash := strings.TrimPrefix(value, prefix)
	if len(hash) < 8 || len(hash) > sha256HexLen {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// ResolveScopedToken hashes the presented credential and returns the matching
// scoped authorization context. It returns (zero, false, nil) for an empty or
// unrecognized credential so the caller can fall through to shared-token or
// unauthenticated handling. It never logs or returns the credential.
func (r *Registry) ResolveScopedToken(_ context.Context, credential string) (query.AuthContext, bool, error) {
	if r == nil {
		return query.AuthContext{}, false, nil
	}
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return query.AuthContext{}, false, nil
	}
	sum := sha256.Sum256([]byte(credential))
	auth, ok := r.byHash[hex.EncodeToString(sum[:])]
	if !ok {
		return query.AuthContext{}, false, nil
	}
	// Return defensive copies so a handler cannot mutate the shared grant table.
	auth.AllowedScopeIDs = append([]string(nil), auth.AllowedScopeIDs...)
	auth.AllowedRepositoryIDs = append([]string(nil), auth.AllowedRepositoryIDs...)
	return auth, true, nil
}

// Len reports the number of registered scoped tokens. Used for startup logging
// of the grant-table size without exposing any token material.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.byHash)
}
