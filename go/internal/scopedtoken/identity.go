// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// CompositeResolver tries scoped-token resolvers in order and returns the first
// match. Unknown credentials fall through; resolver errors fail closed.
type CompositeResolver struct {
	resolvers []query.ScopedTokenResolver
}

// ChainResolvers returns one resolver that tries each non-nil resolver in
// order. It returns nil when no resolver is configured.
func ChainResolvers(resolvers ...query.ScopedTokenResolver) query.ScopedTokenResolver {
	chain := make([]query.ScopedTokenResolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		if resolver != nil {
			chain = append(chain, resolver)
		}
	}
	if len(chain) == 0 {
		return nil
	}
	return &CompositeResolver{resolvers: chain}
}

// ResolveScopedToken implements query.ScopedTokenResolver.
func (r *CompositeResolver) ResolveScopedToken(
	ctx context.Context,
	credential string,
) (query.AuthContext, bool, error) {
	if r == nil {
		return query.AuthContext{}, false, nil
	}
	for _, resolver := range r.resolvers {
		auth, ok, err := resolver.ResolveScopedToken(ctx, credential)
		if err != nil || ok {
			return auth, ok, err
		}
	}
	return query.AuthContext{}, false, nil
}

// PostgresIdentityResolver resolves generated personal and service-principal
// API tokens from identity_token_metadata and active identity role grants.
type PostgresIdentityResolver struct {
	store *pgstatus.ScopedAPITokenStore
	now   func() time.Time
}

// NewPostgresIdentityResolver constructs a resolver for generated
// identity-backed API tokens.
func NewPostgresIdentityResolver(store *pgstatus.ScopedAPITokenStore) *PostgresIdentityResolver {
	return &PostgresIdentityResolver{store: store, now: time.Now}
}

// ResolveScopedToken implements query.ScopedTokenResolver.
func (r *PostgresIdentityResolver) ResolveScopedToken(
	ctx context.Context,
	credential string,
) (query.AuthContext, bool, error) {
	if r == nil || r.store == nil {
		return query.AuthContext{}, false, nil
	}
	credential = trimCredential(credential)
	if credential == "" {
		return query.AuthContext{}, false, nil
	}
	now := r.now()
	if now.IsZero() {
		now = time.Now()
	}
	tokenHash := pgstatus.ScopedAPITokenHash(credential)
	resolution, ok, err := r.store.ResolveIdentityAPITokenHash(ctx, tokenHash, now)
	if err != nil || !ok {
		return query.AuthContext{}, ok, err
	}
	if err := r.store.MarkIdentityAPITokenUsed(ctx, tokenHash, now); err != nil {
		return query.AuthContext{}, false, err
	}
	return query.AuthContext{
		Mode:                         query.AuthModeScoped,
		TenantID:                     resolution.TenantID,
		WorkspaceID:                  resolution.WorkspaceID,
		SubjectClass:                 resolution.SubjectClass,
		SubjectIDHash:                resolution.SubjectIDHash,
		PolicyRevisionHash:           resolution.PolicyRevisionHash,
		RoleIDs:                      append([]string(nil), resolution.RoleIDs...),
		PermissionCatalogEnforced:    true,
		AllowedPermissionFeatures:    append([]string(nil), resolution.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), resolution.AllowedPermissionDataClasses...),
		AllowedScopeIDs:              append([]string(nil), resolution.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), resolution.AllowedRepositoryIDs...),
	}, true, nil
}

func trimCredential(credential string) string {
	return strings.TrimSpace(credential)
}
