package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// collectorListReadinessQueryer is the bounded read seam the configured probe
// needs. *sql.DB satisfies it; tests inject a fake to assert query shape and
// short-circuit behavior.
type collectorListReadinessQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// collectorConfiguredQuery reports whether at least one enabled, non-deactivated
// instance of the requested collector kind is registered. It is a single
// indexed existence check over collector_instances; the
// collector_instances_kind_enabled_idx index covers (collector_kind, enabled),
// so the probe stays bounded regardless of fleet size.
const collectorConfiguredQuery = `
SELECT EXISTS (
    SELECT 1
    FROM collector_instances
    WHERE collector_kind = $1
      AND enabled = TRUE
      AND deactivated_at IS NULL
) AS configured`

// PostgresCollectorListReadinessStore answers the configured-collector probe for
// the gated supply-chain list tools from the collector_instances registry. A
// collector counts as configured only when an enabled, non-deactivated instance
// of its kind exists, which is the authoritative configured-vs-empty signal: a
// collector can be enabled yet have collected zero rows, and that case must read
// as ready_zero_results, not not_configured.
type PostgresCollectorListReadinessStore struct {
	DB collectorListReadinessQueryer
}

// NewPostgresCollectorListReadinessStore creates a Postgres-backed configured
// probe over the collector_instances registry.
func NewPostgresCollectorListReadinessStore(
	db collectorListReadinessQueryer,
) PostgresCollectorListReadinessStore {
	return PostgresCollectorListReadinessStore{DB: db}
}

// CollectorConfigured reports whether an enabled, non-deactivated instance of
// kind is registered.
func (s PostgresCollectorListReadinessStore) CollectorConfigured(
	ctx context.Context,
	kind scope.CollectorKind,
) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("collector list readiness database is required")
	}
	rows, err := s.DB.QueryContext(ctx, collectorConfiguredQuery, string(kind))
	if err != nil {
		return false, fmt.Errorf("probe collector configured: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var configured bool
	if rows.Next() {
		if err := rows.Scan(&configured); err != nil {
			return false, fmt.Errorf("scan collector configured: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("read collector configured rows: %w", err)
	}
	return configured, nil
}
