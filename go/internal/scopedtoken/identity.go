// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/boundederr"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	store       *pgstatus.ScopedAPITokenStore
	now         func() time.Time
	logger      *slog.Logger
	instruments *telemetry.Instruments
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
		return query.AuthContext{}, ok, r.classifyStoreError(ctx, err)
	}
	if err := r.store.MarkIdentityAPITokenUsed(ctx, tokenHash, now); err != nil {
		return query.AuthContext{}, false, r.classifyStoreError(ctx, err)
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

// WithTelemetry attaches the logger and instruments the resolver reports an
// identity-store outage on (#7586) and returns the resolver for chaining. A nil
// logger falls back to slog.Default, as the Postgres pool does; nil instruments
// skip the counter. Call it during wiring, before the resolver serves requests.
func (r *PostgresIdentityResolver) WithTelemetry(
	logger *slog.Logger,
	instruments *telemetry.Instruments,
) *PostgresIdentityResolver {
	if r == nil {
		return nil
	}
	r.logger = logger
	r.instruments = instruments
	return r
}

// classifyStoreError marks a transient identity-store failure with
// querycontract.ErrIdentityStoreUnavailable so the auth middleware answers a
// retryable 503 instead of a flat 401 (#7586), and reports it once per request
// on the log and counter an operator reads. Every other error, including a
// rejected statement or a caller cancel, is returned unchanged: it is not an
// outage and keeps the middleware's bare 401. The driver cause stays reachable
// through errors.Is and errors.As. It returns nil for nil.
func (r *PostgresIdentityResolver) classifyStoreError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	class, ok := identityStoreOutageClass(err)
	if !ok {
		return err
	}
	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}
	// A topology refusal does not clear on its own, so it logs at error level;
	// unavailable and timeout are blips that usually clear in seconds.
	level := slog.LevelWarn
	if class == identityStoreClassTopology {
		level = slog.LevelError
	}
	logger.Log(ctx, level, "identity store unavailable; credential not evaluated, answering retryable 503",
		telemetry.EventAttr(telemetry.EventAuthIdentityStoreUnavailable),
		slog.String(telemetry.LogKeyFailureClass, class),
	)
	if r.instruments != nil && r.instruments.AuthIdentityStoreUnavailable != nil {
		r.instruments.AuthIdentityStoreUnavailable.Add(ctx, 1,
			metric.WithAttributes(attribute.String(telemetry.MetricDimensionFailureClass, class)))
	}
	return fmt.Errorf("%w: %w", querycontract.ErrIdentityStoreUnavailable, err)
}

// identityStoreClassTopology is the failure_class for a writer refused because
// its role, system, database, or primary history differs from the bootstrapped
// one. The other two classes are boundederr kinds (unavailable, timeout).
const identityStoreClassTopology = "topology"

// identityStoreOutageClass reports whether err means the identity store could
// not be reached and, if so, its closed failure class: unavailable or timeout
// (transient) or topology (permanent until restart). It reads the shared
// topology sentinel, the bounded Postgres error's Kind, a bare deadline, and the
// two database/sql connection sentinels the pool can surface unbounded; it never
// reads error text. A caller cancel and a failed statement are not outages.
func identityStoreOutageClass(err error) (string, bool) {
	// A writer refused for a topology mismatch reaches here as a connect error,
	// which boundederr classes as unavailable. It is permanent until restart, so
	// it gets its own class ahead of the Kind switch: "unavailable" would tell an
	// operator to retry shortly forever.
	if errors.Is(err, db.ErrWrongTopology) {
		return identityStoreClassTopology, true
	}
	var bounded *boundederr.Error
	if errors.As(err, &bounded) {
		if kind := bounded.Kind(); kind == boundederr.KindUnavailable || kind == boundederr.KindTimeout {
			return string(kind), true
		}
		return "", false
	}
	// database/sql returns the context error itself, before the driver and the
	// bounded connector see it, when a pool wait or the request deadline runs out.
	// It is transient, like a reader timeout (#7523). A bare cancel is a client
	// disconnect and stays unclaimed.
	if errors.Is(err, context.DeadlineExceeded) {
		return string(boundederr.KindTimeout), true
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) {
		return string(boundederr.KindUnavailable), true
	}
	return "", false
}
